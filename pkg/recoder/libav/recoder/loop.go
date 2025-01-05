package recoder

import (
	"context"
	"errors"
	"fmt"
	"io"
	"runtime"
	"sync"
	"sync/atomic"

	"github.com/asticode/go-astiav"
	"github.com/davecgh/go-spew/spew"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/hashicorp/go-multierror"
	"github.com/xaionaro-go/streamctl/pkg/observability"
	"github.com/xaionaro-go/streamctl/pkg/recoder/libav/recoder/types"
)

const (
	maxInputs      = 256
	maxOutputs     = 256
	outputQueueLen = 1024
)

type LoopConfig struct{}
type Packet = types.Packet

type LoopStats struct {
	BytesCountRead  atomic.Uint64
	BytesCountWrote atomic.Uint64
}

type loopOutput struct {
	Streams      map[*astiav.Stream]*astiav.Stream
	Output       *Output
	OutputChan   chan *EncoderOutput
	FinishedChan chan struct{}

	OutputChanCloseOnce sync.Once
}

type Loop struct {
	WaiterChan chan struct{}
	Result     error
	LoopStats
	cfg           LoopConfig
	locker        sync.Mutex
	newInputChan  chan *Input
	newOutputChan chan *Output
	outputs       map[*loopOutput]struct{}
}

func NewLoop() *Loop {
	l := &Loop{
		WaiterChan:    make(chan struct{}),
		cfg:           LoopConfig{},
		newInputChan:  make(chan *Input, maxInputs),
		newOutputChan: make(chan *Output, maxOutputs),
		outputs:       make(map[*loopOutput]struct{}),
	}
	close(
		l.WaiterChan,
	) // to prevent Wait() from blocking when the process is not started, yet.
	return l
}

func (l *Loop) SetConfig(cfg LoopConfig) error {
	l.locker.Lock()
	defer l.locker.Unlock()
	l.cfg = cfg
	return nil
}

func (l *Loop) AddInput(
	ctx context.Context,
	input *Input,
) error {
	select {
	case l.newInputChan <- input:
	default:
		return fmt.Errorf("too many new inputs already queued")
	}
	return nil
}

func (l *Loop) AddOutput(
	ctx context.Context,
	output *Output,
) error {
	select {
	case l.newOutputChan <- output:
	default:
		return fmt.Errorf("too many new outputs already queued")
	}
	return nil
}

func readIntoPacket(
	_ context.Context,
	input *Input,
	packet *astiav.Packet,
) error {
	err := input.FormatContext.ReadFrame(packet)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, astiav.ErrEof):
		return io.EOF
	default:
		return fmt.Errorf("unable to read a frame: %w", err)
	}
}

func (l *Loop) writerLoopForOutput(
	ctx context.Context,
	output *loopOutput,
) (_err error) {
	logger.Debugf(ctx, "writerLoopForOutput")
	defer func() { logger.Debugf(ctx, "/writerLoopForOutput: %v", _err) }()

	defer func() {
		close(output.FinishedChan)
		l.removeOutput(ctx, output)
		observability.Go(ctx, func() {
			for outputItem := range output.OutputChan {
				PacketPool.Put(outputItem.Packet)
				outputItem.Packet = nil
			}
		})
	}()

	for outputItem := range output.OutputChan {
		err := l.writePacket(ctx, output, outputItem)
		PacketPool.Put(outputItem.Packet)
		outputItem.Packet = nil
		if err != nil {
			return err
		}
	}
	return nil
}

func (l *Loop) writePacket(
	ctx context.Context,
	output *loopOutput,
	outputItem *EncoderOutput,
) (_err error) {
	logger.Tracef(ctx,
		"writePacket (pos:%d, pts:%d, dts:%d, dur:%d)",
		outputItem.Packet.Pos(), outputItem.Packet.Pts(), outputItem.Packet.Dts(), outputItem.Packet.Duration(),
	)
	defer func() { logger.Tracef(ctx, "/writePacket: %v", _err) }()

	packet := outputItem.Packet
	if packet == nil {
		return fmt.Errorf("packet == nil")
	}

	sampleStream := outputItem.Stream
	if sampleStream == nil {
		return fmt.Errorf("sampleStream == nil")
	}

	outputStream := output.Streams[sampleStream]
	if outputStream == nil {
		logger.Debugf(
			ctx,
			"new output stream: %s: %s: %s: %s: %s",
			sampleStream.CodecParameters().MediaType(),
			sampleStream.CodecParameters().CodecID(),
			sampleStream.TimeBase(),
			spew.Sdump(sampleStream),
			spew.Sdump(sampleStream.CodecParameters()),
		)
		sampleStream.CodecParameters()
		outputStream = output.Output.FormatContext.NewStream(nil)
		if outputStream == nil {
			return fmt.Errorf("unable to initialize an output stream")
		}
		if err := sampleStream.CodecParameters().Copy(outputStream.CodecParameters()); err != nil {
			return fmt.Errorf("unable to copy the codec parameters of stream #%d: %w", packet.StreamIndex(), err)
		}
		outputStream.SetDiscard(sampleStream.Discard())
		outputStream.SetAvgFrameRate(sampleStream.AvgFrameRate())
		outputStream.SetRFrameRate(sampleStream.RFrameRate())
		outputStream.SetSampleAspectRatio(sampleStream.SampleAspectRatio())
		outputStream.SetTimeBase(sampleStream.TimeBase())
		outputStream.SetStartTime(sampleStream.StartTime())
		outputStream.SetEventFlags(sampleStream.EventFlags())
		outputStream.SetPTSWrapBits(sampleStream.PTSWrapBits())

		logger.Tracef(
			ctx,
			"resulting output stream: %s: %s: %s: %s: %s",
			outputStream.CodecParameters().MediaType(),
			outputStream.CodecParameters().CodecID(),
			outputStream.TimeBase(),
			spew.Sdump(outputStream),
			spew.Sdump(outputStream.CodecParameters()),
		)
		output.Streams[sampleStream] = outputStream
		if len(output.Streams) < 2 {
			return nil
		}
		if err := output.Output.FormatContext.WriteHeader(nil); err != nil {
			return fmt.Errorf("unable to write the header: %w", err)
		}
	}
	if len(output.Streams) < 2 {
		return nil
	}
	assert(ctx, outputStream != nil)
	if logger.FromCtx(ctx).Level() >= logger.LevelTrace {
		logger.Tracef(
			ctx,
			"unmodified packet with pos:%v (pts:%v, dts:%v, dur: %v) for %s stream %d (->%d) with flags 0x%016X",
			packet.Pos(), packet.Pts(), packet.Dts(), packet.Duration(),
			outputStream.CodecParameters().MediaType(),
			packet.StreamIndex(),
			outputStream.Index(),
			packet.Flags(),
		)
	}
	packet.SetStreamIndex(outputStream.Index())
	if sampleStream.TimeBase() != outputStream.TimeBase() {
		logger.Debugf(ctx, "timebase does not match between the sample and the output: %v != %v; correcting the output stream", sampleStream.TimeBase(), outputStream.TimeBase())
		outputStream.SetTimeBase(sampleStream.TimeBase())
	}
	if logger.FromCtx(ctx).Level() >= logger.LevelTrace {
		logger.Tracef(
			ctx,
			"writing packet with pos:%v (pts:%v, dts:%v, dur:%v) for %s stream %d (sample_rate: %v, time_base: %v) with flags 0x%016X and data 0x %X",
			packet.Pos(), packet.Pts(), packet.Pts(), packet.Duration(),
			outputStream.CodecParameters().MediaType(),
			packet.StreamIndex(), outputStream.CodecParameters().SampleRate(), outputStream.TimeBase(),
			packet.Flags(),
			packet.Data(),
		)
	}
	err := output.Output.FormatContext.WriteInterleavedFrame(packet)
	if err != nil {
		return fmt.Errorf("unable to write the frame: %w", err)
	}
	l.BytesCountRead.Add(uint64(packet.Size()))
	if logger.FromCtx(ctx).Level() >= logger.LevelTrace {
		logger.Tracef(
			ctx,
			"wrote a frame: %s: %s: %#+v",
			outputStream.CodecParameters().MediaType(),
			outputStream.CodecParameters().CodecID(),
			packet,
		)
	}
	return nil
}

func (l *Loop) removeOutput(
	ctx context.Context,
	output *loopOutput,
) {
	logger.Debugf(ctx, "removeOutput: %p", output)
	l.locker.Lock()
	defer l.locker.Unlock()
	delete(l.outputs, output)
}

func (l *Loop) readerLoopForInput(
	ctx context.Context,
	input *Input,
	packetOutput chan<- EncoderInput,
) (_err error) {
	logger.Debugf(ctx, "readerLoopForInput")
	defer func() { logger.Debugf(ctx, "/readerLoopForInput: %v", _err) }()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		packet := PacketPool.Get()
		err := readIntoPacket(ctx, input, packet)
		switch err {
		case nil:
			logger.Tracef(
				ctx,
				"received a frame (pos:%d, pts:%d, dts:%d, dur:%d), data: 0x %X",
				packet.Pos(), packet.Pts(), packet.Dts(), packet.Duration(),
				packet.Data(),
			)
			l.BytesCountRead.Add(uint64(packet.Size()))
			packetOutput <- EncoderInput{
				Input:  input,
				Packet: packet,
			}
		case io.EOF:
			packet.Free()
			return nil
		default:
			packet.Free()
			return fmt.Errorf("unable to process one iteration: %w", err)
		}

	}
}

func (l *Loop) Start(
	ctx context.Context,
	encoder Encoder,
) (_err error) {
	ctx, cancelFn := context.WithCancel(ctx)
	defer func() {
		if _err != nil {
			cancelFn()
		}
	}()

	l.WaiterChan = make(chan struct{})
	var setResultOnce sync.Once
	setResultingError := func(err error) {
		setResultOnce.Do(func() {
			logger.Debugf(ctx, "setResultingError(%v)", err)
			l.Result = err
			cancelFn()
			close(l.WaiterChan)
		})
	}

	const packetsPoolSize = 100
	packetsChan := make(chan EncoderInput, packetsPoolSize)

	inputsEnded := make(chan struct{})
	outputsEnded := make(chan struct{})

	observability.Go(ctx, func() {
		defer func() {
			logger.Debugf(ctx, "flushing packet buffers")
			defer logger.Tracef(ctx, "/flushing packet buffers")
			for {
				select {
				case packet := <-packetsChan:
					PacketPool.Put(packet.Packet)
					packet.Packet = nil
				default:
					return
				}
			}
		}()

		var readersCount atomic.Int64
		var readersCountLocker sync.Mutex

		startReaderLoopForInput := func(
			input *Input,
		) {
			readersCount.Add(1)
			logger.Tracef(ctx, "amount of inputs: +1: %d", readersCount.Load())
			observability.Go(ctx, func() {
				defer func() {
					readersCountLocker.Lock()
					defer readersCountLocker.Unlock()
					if readersCount.Add(-1) == 0 {
						logger.Infof(ctx, "no more inputs left")
						close(inputsEnded)
					}
					logger.Tracef(ctx, "amount of inputs: -1: %d", readersCount.Load())
				}()
				err := l.readerLoopForInput(ctx, input, packetsChan)
				if err != nil {
					setResultingError(fmt.Errorf("unable to process one iteration: %w", err))
					return
				}
			})
		}

		func() {
			readersCountLocker.Lock()
			defer readersCountLocker.Unlock()
			for {
				select {
				case input := <-l.newInputChan:
					startReaderLoopForInput(input)
				default:
					return
				}
			}
		}()

		runtime.Gosched()

		var writersCount atomic.Int64
		var writersCountLocker sync.Mutex

		startWriterLoopForOutput := func(
			output *Output,
		) {
			out := &loopOutput{
				Output:       output,
				OutputChan:   make(chan *EncoderOutput, outputQueueLen),
				FinishedChan: make(chan struct{}),
				Streams:      make(map[*astiav.Stream]*astiav.Stream),
			}
			l.outputs[out] = struct{}{}
			writersCount.Add(1)
			logger.Tracef(ctx, "amount of outputs: +1: %d", writersCount.Load())
			observability.Go(ctx, func() {
				defer func() {
					writersCountLocker.Lock()
					defer writersCountLocker.Unlock()
					if writersCount.Add(-1) == 0 {
						logger.Infof(ctx, "no more outputs left")
						close(outputsEnded)
					}
					logger.Tracef(ctx, "amount of outputs: -1: %d", writersCount.Load())
				}()
				err := l.writerLoopForOutput(ctx, out)
				if err != nil {
					setResultingError(fmt.Errorf("unable to process one iteration: %w", err))
					return
				}
			})
		}

		func() {
			writersCountLocker.Lock()
			defer writersCountLocker.Unlock()
			for {
				select {
				case <-inputsEnded:
					return
				case output := <-l.newOutputChan:
					startWriterLoopForOutput(output)
				default:
					return
				}
			}
		}()

		for {
			select {
			case <-inputsEnded:
				return
			case <-outputsEnded:
				return
			case <-ctx.Done():
				return
			case input := <-l.newInputChan:
				startReaderLoopForInput(input)
			case output := <-l.newOutputChan:
				startWriterLoopForOutput(output)
			}
		}
	})

	observability.Go(ctx, func() {
		defer func() {
			logger.Debugf(ctx, "flushing packet buffers")
			defer logger.Tracef(ctx, "/flushing packet buffers")
			for {
				select {
				case packet := <-packetsChan:
					PacketPool.Put(packet.Packet)
					packet.Packet = nil
				default:
					return
				}
			}
		}()

	iterationsLoop:
		for {
			select {
			case <-ctx.Done():
				setResultingError(ctx.Err())
				return
			case <-inputsEnded:
				break iterationsLoop
			case <-outputsEnded:
				break iterationsLoop
			case packet := <-packetsChan:
				err := l.processInputPacket(ctx, encoder, packet)
				PacketPool.Put(packet.Packet)
				packet.Packet = nil
				switch err {
				case nil:
				default:
					setResultingError(fmt.Errorf("unable to process an input packet: %w", err))
					return
				}
			}
		}

		if err := l.finalize(ctx); err != nil {
			setResultingError(fmt.Errorf("unable to finalize: %w", err))
			return
		}

		logger.Debugf(ctx, "finished re-encoding")
		setResultingError(nil)
	})

	return nil
}

func (l *Loop) processInputPacket(
	ctx context.Context,
	encoder Encoder,
	inputPacket EncoderInput,
) (_err error) {
	logger.Tracef(
		ctx,
		"encoding packet (pos:%d, pts:%d, dts:%d, dur:%d), data: 0x %X",
		inputPacket.Packet.Pos(), inputPacket.Packet.Pts(), inputPacket.Packet.Dts(), inputPacket.Packet.Duration(),
		inputPacket.Packet.Data(),
	)
	defer func() { logger.Tracef(ctx, "encoding result: %v", _err) }()

	outputPacket, err := encoder.Encode(ctx, inputPacket)
	if err != nil {
		return fmt.Errorf("unable to encode: %w", err)
	}
	if outputPacket == nil {
		return nil
	}
	defer func(outputPacket *EncoderOutput) {
		PacketPool.Put(outputPacket.Packet)
		outputPacket.Packet = nil
	}(outputPacket)

	for output := range l.outputs {
		select {
		case <-output.FinishedChan:
			output.OutputChanCloseOnce.Do(func() {
				close(output.OutputChan)
			})
			continue
		default:
		}
		clonedOutput := &EncoderOutput{
			Packet: ClonePacketAsWritable(outputPacket.Packet),
			Stream: outputPacket.Stream,
		}
		logger.Tracef(
			ctx,
			"queueing packet (pos:%d, pts:%d, dts:%d, dur:%d), data: 0x %X",
			inputPacket.Packet.Pos(), inputPacket.Packet.Pts(), inputPacket.Packet.Dts(), inputPacket.Packet.Duration(),
			inputPacket.Packet.Data(),
		)
		select {
		case output.OutputChan <- clonedOutput:
		default:
			logger.Errorf(ctx, "the queue is full, cannot send a packet to the Output")
		}
	}

	return nil
}

func (l *Loop) finalize(
	_ context.Context,
) error {
	var mErr *multierror.Error
	for output := range l.outputs {
		// waiting until nobody else writing to the output:
		for outputItem := range output.OutputChan {
			PacketPool.Put(outputItem.Packet)
			outputItem.Packet = nil
		}

		// writing the trailer
		if err := output.Output.FormatContext.WriteTrailer(); err != nil {
			mErr = multierror.Append(mErr, fmt.Errorf("unable to write the trailer to output %p: %w", output, err))
		}
	}
	return nil
}

func (l *Loop) Wait(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-l.WaiterChan:
	}
	return l.Result
}

func (l *Loop) StartAndWait(
	ctx context.Context,
	encoder Encoder,
) error {
	err := l.Start(ctx, encoder)
	if err != nil {
		return fmt.Errorf("got an error while starting the recording: %w", err)
	}

	if err != l.Wait(ctx) {
		return fmt.Errorf("got an error while waiting for a completion: %w", err)
	}

	return nil
}
