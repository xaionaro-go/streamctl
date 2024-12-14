package xaionarogortmp

import (
	"context"
	"fmt"
	"runtime/debug"
	"sync/atomic"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/hashicorp/go-multierror"
	"github.com/xaionaro-go/go-rtmp"
	rtmpmsg "github.com/xaionaro-go/go-rtmp/message"
	recoder "github.com/xaionaro-go/streamctl/pkg/encoder"
	yutoppgortmp "github.com/xaionaro-go/streamctl/pkg/streamserver/implementations/xaionaro-go-rtmp"
	"github.com/xaionaro-go/streamctl/pkg/xsync"
	flvtag "github.com/yutopp/go-flv/tag"
)

const (
	chunkSize = 128
)

type Encoder struct {
	Locker     xsync.Mutex
	Stream     *rtmp.Stream
	CancelFunc context.CancelFunc
	ReadCount  atomic.Uint64
	WriteCount atomic.Uint64
	Sub        *yutoppgortmp.Sub
	eventChan  chan *flvtag.FlvTag
}

var _ recoder.Encoder = (*Encoder)(nil)
var _ recoder.NewInputFromPublisherer = (*Encoder)(nil)

func (EncoderFactory) New(
	ctx context.Context,
	cfg recoder.Config,
) (recoder.Encoder, error) {
	return &Encoder{
		eventChan: make(chan *flvtag.FlvTag),
	}, nil
}

func (r *Encoder) StartRecoding(
	ctx context.Context,
	inputIface recoder.Input,
	outputIface recoder.Output,
) (_err error) {
	defer func() {
		if r := recover(); r != nil {
			_err = fmt.Errorf("got panic: %v", r)
		}
		if _err == nil {
			return
		}
		logger.FromCtx(ctx).
			WithField("error_event_exception_stack_trace", string(debug.Stack())).Errorf("%v", _err)
		r.Close()
	}()
	input, ok := inputIface.(*Input)
	if !ok {
		return fmt.Errorf("expected Input of type %T, but received %T", input, inputIface)
	}

	output, ok := outputIface.(*Output)
	if !ok {
		return fmt.Errorf("expected Input of type %T, but received %T", output, outputIface)
	}

	err := xsync.DoR1(ctx, &r.Locker, func() error {
		if r.CancelFunc != nil {
			return fmt.Errorf("recoding is already running")
		}

		stream, err := output.Client.CreateStream(
			ctx,
			&rtmpmsg.NetConnectionCreateStream{},
			chunkSize,
		)
		if err != nil {
			return fmt.Errorf("unable to create a stream on the remote side: %w", err)
		}
		r.Stream = stream

		logger.Debugf(ctx, "calling Publish")
		if err := r.Stream.Publish(ctx, &rtmpmsg.NetStreamPublish{
			PublishingName: output.StreamKey,
			PublishingType: "live",
		}); err != nil {
			return fmt.Errorf("unable to send the Publish to the remote endpoint: %w", err)
		}

		logger.Debugf(ctx, "starting publishing")
		switch {
		case input.Pubsub != nil:
			r.Sub = input.Pubsub.Sub(output.Client, r.subCallback(r.Stream))
		default:
			return fmt.Errorf("this case is not implemented, yet")
		}

		logger.Debugf(ctx, "started publishing")
		return nil
	})
	if err != nil {
		return nil
	}

	logger.Debugf(ctx, "the source stopped, so stopped also publishing")
	return nil
}

func (r *Encoder) WaitForRecordingEnd(
	ctx context.Context,
) error {
	var closeChan <-chan struct{}
	err := xsync.DoR1(ctx, &r.Locker, func() error {
		if r.CancelFunc != nil {
			return fmt.Errorf("recoding is not started (or was already closed)")
		}

		switch {
		case r.Sub != nil:
			closeChan = r.Sub.ClosedChan()
		default:
			return fmt.Errorf("this case is not implemented, yet")
		}
		return nil
	})
	if err != nil {
		return err
	}
	<-closeChan
	return nil
}

func (r *Encoder) GetStats(context.Context) (*recoder.Stats, error) {
	return &recoder.Stats{
		BytesCountRead:  r.ReadCount.Load(),
		BytesCountWrote: r.WriteCount.Load(),
	}, nil
}

func (r *Encoder) Close() (_err error) {
	ctx := context.TODO()
	logger.Debug(ctx, "closing the Encoder")
	defer func() { logger.Debugf(ctx, "closed the Encoder: %v", _err) }()
	return xsync.DoR1(ctx, &r.Locker, func() error {
		var result *multierror.Error

		if r.CancelFunc == nil {
			return fmt.Errorf("the stream was not started yet")
		}
		r.CancelFunc()
		r.CancelFunc = nil
		if r.Stream != nil {
			result = multierror.Append(result, r.Stream.Close())
			r.Stream = nil
		}
		if r.Sub != nil {
			result = multierror.Append(result, r.Sub.Close())
			r.Sub = nil
		}
		return result.ErrorOrNil()
	})
}
