package recoder

import (
	"context"
	"fmt"
	"math"
	"sync"

	"github.com/asticode/go-astiav"
	"github.com/davecgh/go-spew/spew"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/observability"
)

type loopStreamOutput struct {
	*astiav.Stream
	LastDTS int64
}

type loopOutput struct {
	Loop         *Loop
	Streams      map[*astiav.Stream]*loopStreamOutput
	Output       *Output
	OutputChan   chan *EncoderOutput
	FinishedChan chan struct{}

	OutputChanCloseOnce sync.Once
}

func newLoopOutput(
	loop *Loop,
	output *Output,
) *loopOutput {
	return &loopOutput{
		Loop:         loop,
		Output:       output,
		OutputChan:   make(chan *EncoderOutput, outputQueueLen),
		FinishedChan: make(chan struct{}),
		Streams:      make(map[*astiav.Stream]*loopStreamOutput),
	}
}
func (out *loopOutput) writerLoopForOutput(
	ctx context.Context,
) (_err error) {
	logger.Debugf(ctx, "writerLoopForOutput")
	defer func() { logger.Debugf(ctx, "/writerLoopForOutput: %v", _err) }()

	defer func() {
		close(out.FinishedChan)
		out.Loop.removeOutput(ctx, out)
		observability.Go(ctx, func() {
			for outputItem := range out.OutputChan {
				PacketPool.Put(outputItem.Packet)
				outputItem.Packet = nil
			}
		})
	}()

	for outputItem := range out.OutputChan {
		err := out.writePacket(ctx, outputItem)
		PacketPool.Put(outputItem.Packet)
		outputItem.Packet = nil
		if err != nil {
			return err
		}
	}
	return nil
}

var firstTime = false

func (out *loopOutput) writePacket(
	ctx context.Context,
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

	outputStream := out.Streams[sampleStream]
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
		outputStream = &loopStreamOutput{
			Stream:  out.Output.FormatContext.NewStream(nil),
			LastDTS: math.MinInt64,
		}
		if outputStream.Stream == nil {
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
		out.Streams[sampleStream] = outputStream
		if len(out.Streams) < 2 {
			return nil
		}
		if err := out.Output.FormatContext.WriteHeader(nil); err != nil {
			return fmt.Errorf("unable to write the header: %w", err)
		}
	}
	if len(out.Streams) < 2 {
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

	if packet.Dts() < outputStream.LastDTS {
		logger.Errorf(ctx, "received a DTS from the past, ignoring the packet: %d < %d", packet.Dts(), outputStream.LastDTS)
		return nil
	}

	packet.SetStreamIndex(outputStream.Index())
	if sampleStream.TimeBase() != outputStream.TimeBase() {
		logger.Debugf(ctx, "timebase does not match between the sample and the output: %v != %v; correcting the output stream", outputStream.TimeBase(), sampleStream.TimeBase())
		outputStream.SetTimeBase(sampleStream.TimeBase())
	}
	dts := packet.Dts()
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

	err := out.Output.FormatContext.WriteInterleavedFrame(packet)
	if err != nil {
		return fmt.Errorf("unable to write the frame: %w", err)
	}
	outputStream.LastDTS = dts
	out.Loop.BytesCountRead.Add(uint64(packet.Size()))
	if logger.FromCtx(ctx).Level() >= logger.LevelTrace {
		logger.Tracef(
			ctx,
			"wrote a packet (dts: %d): %s: %s",
			dts,
			outputStream.CodecParameters().MediaType(),
			outputStream.CodecParameters().CodecID(),
		)
	}
	return nil
}

func (l *Loop) findOutputByID(
	ctx context.Context,
	outputID OutputID,
) (_ret *loopOutput, _err error) {
	logger.Debugf(ctx, "findOutputByID: %d", outputID)
	defer func() { logger.Debugf(ctx, "/findOutputByID: %d: %#+v %v", outputID, _ret, _err) }()

	l.locker.Lock()
	defer l.locker.Unlock()
	for output := range l.outputs {
		if output.Output.ID == outputID {
			return output, nil
		}
	}
	return nil, fmt.Errorf("not found")
}

func (l *Loop) removeOutputByID(
	ctx context.Context,
	outputID OutputID,
) {
	logger.Debugf(ctx, "removeOutputByID: %d", outputID)
	output, err := l.findOutputByID(ctx, outputID)
	if err != nil {
		logger.Errorf(ctx, "unable to find output with ID %d", outputID)
		return
	}
	l.removeOutput(ctx, output)
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
