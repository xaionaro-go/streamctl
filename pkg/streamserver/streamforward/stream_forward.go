package streamforward

import (
	"context"
	"fmt"
	"net/url"
	"runtime/debug"
	"sync/atomic"
	"time"

	"github.com/facebookincubator/go-belt/tool/experimental/errmon"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/hashicorp/go-multierror"
	"github.com/xaionaro-go/observability"
	recoder "github.com/xaionaro-go/recoder"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/types"
	"github.com/xaionaro-go/xsync"
)

type StreamForward struct {
	StreamID         types.StreamID
	DestinationID    types.DestinationID
	Enabled          bool
	Quirks           types.ForwardingQuirks
	ActiveForwarding *ActiveStreamForwarding
	NumBytesWrote    uint64
	NumBytesRead     uint64
}

type ActiveStreamForwarding struct {
	*StreamForwards
	StreamID             types.StreamID
	DestinationURL       *url.URL
	DestinationStreamKey string
	ReadCount            atomic.Uint64
	WriteCount           atomic.Uint64
	RecoderFactory       recoder.Factory
	PauseFunc            func(ctx context.Context, fwd *ActiveStreamForwarding)

	cancelFunc context.CancelFunc

	locker             xsync.Mutex
	recoder            recoder.Recoder
	recodingCancelFunc context.CancelFunc
}

func (fwds *StreamForwards) NewActiveStreamForward(
	ctx context.Context,
	streamID types.StreamID,
	urlString string,
	streamKey string,
	pauseFunc func(ctx context.Context, fwd *ActiveStreamForwarding),
	opts ...Option,
) (_ret *ActiveStreamForwarding, _err error) {
	logger.Debugf(
		ctx,
		"NewActiveStreamForward(ctx, '%s', '%s', relayService, pauseFunc)",
		streamID,
		urlString,
	)
	defer func() {
		logger.Debugf(
			ctx,
			"/NewActiveStreamForward(ctx, '%s', '%s', relayService, pauseFunc): %#+v %v",
			streamID,
			urlString,
			_ret,
			_err,
		)
	}()

	urlParsed, err := url.Parse(urlString)
	if err != nil {
		return nil, fmt.Errorf("unable to parse URL '%s': %w", urlString, err)
	}
	fwd := &ActiveStreamForwarding{
		RecoderFactory:       fwds.RecoderFactory,
		StreamForwards:       fwds,
		StreamID:             streamID,
		DestinationURL:       urlParsed,
		DestinationStreamKey: streamKey,
		PauseFunc:            pauseFunc,
	}
	for _, opt := range opts {
		opt.apply(fwd)
	}
	if err := fwd.Start(ctx); err != nil {
		return nil, fmt.Errorf("unable to start the forwarder: %w", err)
	}
	return fwd, nil
}

func (fwd *ActiveStreamForwarding) Start(ctx context.Context) (_err error) {
	logger.Debugf(ctx, "Start")
	defer func() { logger.Debugf(ctx, "/Start: %v", _err) }()

	return xsync.DoA1R1(ctx, &fwd.locker, fwd.start, ctx)
}

func (fwd *ActiveStreamForwarding) start(ctx context.Context) (_err error) {
	if fwd.cancelFunc != nil {
		return fmt.Errorf("the stream forwarder is already running")
	}
	ctx, cancelFn := context.WithCancel(ctx)
	fwd.cancelFunc = cancelFn
	observability.Go(ctx, func() {
		for {
			err := fwd.waitForPublisherAndStart(
				ctx,
			)
			select {
			case <-ctx.Done():
				fwd.Close()
				return
			default:
			}
			if err != nil {
				logger.Errorf(ctx, "%s", err)
			}
		}
	})
	return nil
}

func (fwd *ActiveStreamForwarding) Stop() error {
	return fwd.Close()
}

func (fwd *ActiveStreamForwarding) WaitForPublisher(
	ctx context.Context,
) (types.Publisher, error) {
	var publisher types.Publisher
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		logger.Debugf(ctx, "wait for stream '%s'", fwd.StreamID)
		publisherChan, err := fwd.StreamServer.WaitPublisherChan(ctx, fwd.StreamID, false)
		if err != nil {
			return nil, fmt.Errorf("unable to get a channel to wait for a publisher: %w", err)
		}
		publisher = <-publisherChan
		if publisher != nil {
			break
		}
		logger.Debugf(ctx, "received publisher is nil, retrying")
	}
	logger.Debugf(ctx, "received a non-nil publisher: %#+v", publisher)

	logger.Debugf(ctx, "checking if we need to pause")
	fwd.PauseFunc(ctx, fwd)
	logger.Debugf(ctx, "no pauses or pauses ended")
	return publisher, nil
}

func (fwd *ActiveStreamForwarding) waitForPublisherAndStart(
	ctx context.Context,
) (_ret error) {
	defer func() {
		if r := recover(); r != nil {
			_ret = fmt.Errorf("got panic: %v", r)
		}
		if _ret == nil {
			return
		}
		logger.FromCtx(ctx).
			WithField("error_event_exception_stack_trace", string(debug.Stack())).Errorf("%v", _ret)
	}()

	publisher, err := fwd.WaitForPublisher(ctx)
	if err != nil {
		return fmt.Errorf("unable to get publisher: %w", err)
	}

	ctx, cancelFn := context.WithCancel(ctx)
	defer cancelFn()
	observability.Go(ctx, func() {
		defer cancelFn()
		select {
		case <-ctx.Done():
			return
		case <-publisher.ClosedChan():
			return
		}
	})

	logger.Debugf(ctx, "DestinationStreamingLocker.Lock(ctx, '%s')", fwd.DestinationURL)
	destinationUnlocker := fwd.StreamForwards.DestinationStreamingLocker.Lock(
		ctx,
		fwd.DestinationURL,
	)
	defer func() {
		if destinationUnlocker != nil { // if ctx was cancelled before we locked then the unlocker is nil
			destinationUnlocker.Unlock()
		}
		logger.Debugf(ctx, "DestinationStreamingLocker.Unlock(ctx, '%s')", fwd.DestinationURL)
	}()
	logger.Debugf(ctx, "/DestinationStreamingLocker.Lock(ctx, '%s')", fwd.DestinationURL)

	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	defer func() {
		fwd.locker.Do(ctx, func() {
			err := fwd.killRecodingProcess()
			if err != nil {
				logger.Warn(ctx, err)
			}
		})
	}()

	recoderInstance, err := fwd.RecoderFactory.New(ctx)
	if err != nil {
		return fmt.Errorf("unable to initialize a recoder: %w", err)
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	encoder, err := fwd.newEncoderFor(ctx, recoderInstance)
	if err != nil {
		errmon.ObserveErrorCtx(ctx, recoderInstance.Close())
		return fmt.Errorf("unable to open the input: %w", err)
	}
	defer func() {
		err := encoder.Close()
		if err != nil {
			logger.Errorf(ctx, "unable to close the encoder: %v", err)
		}
	}()

	input, err := fwd.openInputFor(ctx, recoderInstance, publisher)
	if err != nil {
		errmon.ObserveErrorCtx(ctx, recoderInstance.Close())
		return fmt.Errorf("unable to open the input: %w", err)
	}
	defer func() {
		err := input.Close()
		if err != nil {
			logger.Errorf(ctx, "unable to close the input: %v", err)
		}
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	output, err := fwd.openOutputFor(ctx, recoderInstance)
	if err != nil {
		errmon.ObserveErrorCtx(ctx, recoderInstance.Close())
		return fmt.Errorf("unable to open the output: %w", err)
	}
	defer func() {
		err := output.Close()
		if err != nil {
			logger.Errorf(ctx, "unable to close the output: %v", err)
		}
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	recodingFinished := make(chan struct{})
	defer func() {
		close(recodingFinished)
	}()
	err = xsync.DoR1(ctx, &fwd.locker, func() error {
		if fwd.recoder != nil {
			return fmt.Errorf("recoder process is already initialized")
		}
		fwd.recoder = recoderInstance

		fwd.recodingCancelFunc = func() {
			cancelFn()
			<-recodingFinished
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		err = recoderInstance.StartRecoding(ctx, encoder, input, output)
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			default:
			}
			return fmt.Errorf("unable to Recode: %w", err)
		}
		return nil
	})
	if err != nil {
		return err
	}

	observability.Go(ctx, func() {
		t := time.NewTicker(time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}

			stats, err := recoderInstance.GetStats(ctx)
			if err != nil {
				logger.Errorf(ctx, "unable to get stats: %v", err)
				return
			}

			fwd.ReadCount.Store(stats.BytesCountRead)
			fwd.WriteCount.Store(stats.BytesCountWrote)
		}
	})

	return recoderInstance.WaitForRecodingEnd(ctx)
}

func (fwd *ActiveStreamForwarding) newEncoderFor(
	ctx context.Context,
	recoderInstance recoder.Recoder,
) (recoder.Encoder, error) {
	return recoderInstance.NewEncoder(ctx, recoder.EncoderConfig{})
}

func (fwd *ActiveStreamForwarding) openInputFor(
	ctx context.Context,
	recoderInstance recoder.Recoder,
	publisher types.Publisher,
) (recoder.Input, error) {
	inputURL, err := fwd.GetLocalhostEndpoint(ctx)
	if err != nil {
		return nil, fmt.Errorf("unable to get a localhost endpoint: %w", err)
	}

	inputURL.Path = "/" + string(fwd.StreamID)

	var input recoder.Input
	inputCfg := recoder.InputConfig{}
	if newInputFromStreamIDer, ok := recoderInstance.(recoder.NewInputFromPublisherer); ok {
		input, err = newInputFromStreamIDer.NewInputFromPublisher(ctx, publisher, inputCfg)
	} else {
		input, err = recoderInstance.NewInputFromURL(ctx, inputURL.String(), "", inputCfg)
	}
	if err != nil {
		return nil, fmt.Errorf("unable to open '%s' as the input: %w", inputURL, err)
	}
	logger.Debugf(ctx, "opened '%s' as the input", inputURL)
	return input, nil
}

func (fwd *ActiveStreamForwarding) openOutputFor(
	ctx context.Context,
	recoderInstance recoder.Recoder,
) (recoder.Output, error) {
	output, err := recoderInstance.NewOutputFromURL(
		ctx,
		fwd.DestinationURL.String(),
		fwd.DestinationStreamKey,
		recoder.OutputConfig{},
	)
	if err != nil {
		return nil, fmt.Errorf("unable to open '%s' as the output: %w", fwd.DestinationURL, err)
	}
	logger.Debugf(ctx, "opened '%s' as the output", fwd.DestinationURL)

	return output, nil
}

func (fwd *ActiveStreamForwarding) killRecodingProcess() error {
	var result *multierror.Error

	if fwd.recodingCancelFunc != nil {
		fwd.recodingCancelFunc()
		fwd.recodingCancelFunc = nil
	}

	if fwd.recoder != nil {
		err := fwd.recoder.Close()
		if err != nil {
			result = multierror.Append(result, fmt.Errorf("unable to close fwd.Client: %v", err))
		}
		fwd.recoder = nil
	}

	return result.ErrorOrNil()
}

func (fwd *ActiveStreamForwarding) Close() error {
	ctx := context.TODO()
	return xsync.DoR1(ctx, &fwd.locker, func() error {
		if fwd.cancelFunc == nil {
			return fmt.Errorf("the stream was not started yet")
		}

		var result *multierror.Error
		if fwd.cancelFunc != nil {
			fwd.cancelFunc()
			fwd.cancelFunc = nil
		}

		if err := fwd.killRecodingProcess(); err != nil {
			result = multierror.Append(result, fmt.Errorf("unable to stop recoding: %w", err))
		}
		return result.ErrorOrNil()
	})
}

func (fwd *ActiveStreamForwarding) String() string {
	if fwd == nil {
		return "null"
	}
	return fmt.Sprintf("%s->%s", fwd.StreamID, fwd.DestinationURL)
}
