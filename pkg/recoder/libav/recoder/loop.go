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
	Streams      map[int]*astiav.Stream
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
				outputItem.refCounter.Done()
			}
		})
	}()

	for outputItem := range output.OutputChan {
		err := l.writePacket(ctx, output, outputItem)
		outputItem.refCounter.Done()
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
) error {
	streamIdx := outputItem.Packet.StreamIndex()

	packet := outputItem.Packet
	if packet == nil {
		return fmt.Errorf("packet == nil")
	}

	sampleStream := outputItem.Stream
	if sampleStream == nil {
		return fmt.Errorf("sampleStream == nil")
	}

	outputStream := output.Streams[streamIdx]
	if outputStream == nil {
		logger.Debugf(
			ctx,
			"new output stream: %s: %s: %s",
			sampleStream.CodecParameters().MediaType(),
			sampleStream.CodecParameters().CodecID(),
			spew.Sdump(sampleStream.CodecParameters()),
		)
		outputStream = output.Output.FormatContext.NewStream(nil)
		if outputStream == nil {
			return fmt.Errorf("unable to initialize an output stream")
		}
		if err := sampleStream.CodecParameters().Copy(outputStream.CodecParameters()); err != nil {
			return fmt.Errorf("unable to copy the codec parameters of stream #%d: %w", packet.StreamIndex(), err)
		}
		if err := output.Output.FormatContext.WriteHeader(nil); err != nil {
			return fmt.Errorf("unable to write the header: %w", err)
		}
		output.Streams[streamIdx] = outputStream
	}
	assert(ctx, outputStream != nil)
	packet.SetStreamIndex(outputStream.Index())
	err := output.Output.FormatContext.WriteInterleavedFrame(packet)
	if err != nil {
		return fmt.Errorf("unable to write the frame: %w", err)
	}
	l.BytesCountRead.Add(uint64(packet.Size()))
	logger.Tracef(ctx, "wrote a frame: %#+v", packet)
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
	packetsPool <-chan *astiav.Packet,
) (_err error) {
	logger.Debugf(ctx, "readerLoopForInput")
	defer func() { logger.Debugf(ctx, "/readerLoopForInput: %v", _err) }()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case packet := <-packetsPool:
			err := readIntoPacket(ctx, input, packet)
			switch err {
			case nil:
				logger.Tracef(ctx, "received a frame, data: %X", packet.Data())
				l.BytesCountRead.Add(uint64(packet.Size()))
				packetOutput <- EncoderInput{
					Input:  input,
					Packet: packet,
				}
			case io.EOF:
				return nil
			default:
				return fmt.Errorf("unable to process one iteration: %w", err)
			}
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
	packetsPool := make(chan *astiav.Packet, packetsPoolSize)
	for range packetsPoolSize {
		packet := astiav.AllocPacket()
		packetsPool <- packet
	}
	packetsChan := make(chan EncoderInput, packetsPoolSize)

	inputsEnded := make(chan struct{})
	outputsEnded := make(chan struct{})

	observability.Go(ctx, func() {
		defer func() {
			for {
				select {
				case packet := <-packetsChan:
					packet.Packet.Unref()
					packet.Packet.Free()
				case packet := <-packetsPool:
					packet.Free()
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
				err := l.readerLoopForInput(ctx, input, packetsChan, packetsPool)
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
				Streams:      make(map[int]*astiav.Stream),
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
			for {
				select {
				case packet := <-packetsChan:
					packet.Packet.Unref()
					packet.Packet.Free()
				case packet := <-packetsPool:
					packet.Free()
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
				packet.Packet.Unref()
				packetsPool <- packet.Packet
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
) error {
	outputPacket, err := encoder.Encode(ctx, inputPacket)
	if err != nil {
		return fmt.Errorf("unable to encode: %w", err)
	}
	if outputPacket == nil {
		return nil
	}

	for output := range l.outputs {
		outputPacket.refCounter.Add(1)
		select {
		case <-output.FinishedChan:
			output.OutputChanCloseOnce.Do(func() {
				close(output.OutputChan)
			})
			outputPacket.refCounter.Done()
			continue
		default:
		}
		select {
		case output.OutputChan <- outputPacket:
		default:
			outputPacket.refCounter.Done()
			logger.Errorf(ctx, "the queue is full, cannot send a packet to the Output")
		}
	}

	observability.Go(ctx, func() {
		outputPacket.refCounter.Wait()
		outputPacket.UnrefAndFree()
	})

	return nil
}

func (l *Loop) finalize(
	_ context.Context,
) error {
	var mErr *multierror.Error
	for output := range l.outputs {
		// waiting until nobody else writing to the output:
		for outputItem := range output.OutputChan {
			outputItem.refCounter.Done()
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
