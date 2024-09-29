package streamforward

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"runtime/debug"
	"strings"
	"sync/atomic"

	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/hashicorp/go-multierror"
	"github.com/xaionaro-go/lockmap"
	"github.com/xaionaro-go/streamctl/pkg/observability"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/types"
	commontypes "github.com/xaionaro-go/streamctl/pkg/streamtypes"
	"github.com/xaionaro-go/streamctl/pkg/xlogger"
	"github.com/xaionaro-go/streamctl/pkg/xsync"
)

const (
	chunkSize = 128
)

type StreamForwards struct {
	DestinationStreamingLocker *lockmap.LockMap
}

func NewStreamForwards() *StreamForwards {
	return &StreamForwards{
		DestinationStreamingLocker: lockmap.NewLockMap(),
	}
}

type Unlocker interface {
	Unlock()
}

type ActiveStreamForwarding struct {
	*StreamForwards
	Recoding      commontypes.VideoConvertConfig
	Locker        xsync.Mutex
	StreamServer  StreamServer
	StreamID      types.StreamID
	DestinationID types.DestinationID
	URL           *url.URL
	Sub           Sub
	CancelFunc    context.CancelFunc
	ReadCount     atomic.Uint64
	WriteCount    atomic.Uint64
	PauseFunc     func(ctx context.Context, fwd *ActiveStreamForwarding)
}

func (fwds *StreamForwards) NewActiveStreamForward(
	ctx context.Context,
	s StreamServer,
	streamID types.StreamID,
	dstID types.DestinationID,
	urlString string,
	pauseFunc func(ctx context.Context, fwd *ActiveStreamForwarding),
	opts ...Option,
) (_ret *ActiveStreamForwarding, _err error) {
	logger.Debugf(ctx, "NewActiveStreamForward(ctx, '%s', '%s', '%s', relayService, pauseFunc)", streamID, dstID, urlString)
	defer func() {
		logger.Debugf(ctx, "/NewActiveStreamForward(ctx, '%s', '%s', '%s', relayService, pauseFunc): %#+v %v", streamID, dstID, urlString, _ret, _err)
	}()

	urlParsed, err := url.Parse(urlString)
	if err != nil {
		return nil, fmt.Errorf("unable to parse URL '%s': %w", urlString, err)
	}
	fwd := &ActiveStreamForwarding{
		StreamForwards: fwds,
		StreamServer:   s,
		StreamID:       streamID,
		DestinationID:  dstID,
		URL:            urlParsed,
		PauseFunc:      pauseFunc,
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

	return xsync.DoA1R1(ctx, &fwd.Locker, fwd.start, ctx)
}

func (fwd *ActiveStreamForwarding) start(ctx context.Context) (_err error) {
	if fwd.CancelFunc != nil {
		return fmt.Errorf("the stream forwarder is already running")
	}
	ctx, cancelFn := context.WithCancel(ctx)
	fwd.CancelFunc = cancelFn
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

func (fwd *ActiveStreamForwarding) getAppNameAndKey() (string, string, string) {
	remoteAppName := "live"
	pathParts := strings.SplitN(fwd.URL.Path, "/", -2)
	apiKey := pathParts[len(pathParts)-1]
	if len(pathParts) >= 2 {
		remoteAppName = strings.Trim(strings.Join(pathParts[:len(pathParts)-1], "/"), "/")
	}
	streamID := fwd.StreamID
	streamIDParts := strings.Split(string(streamID), "/")
	localAppName := string(streamID)
	if len(streamIDParts) == 2 {
		localAppName = streamIDParts[1]
	}

	return localAppName, remoteAppName, apiKey
}

func (fwd *ActiveStreamForwarding) WaitForPublisher(
	ctx context.Context,
) (Pubsub, error) {
	localAppName, _, apiKey := fwd.getAppNameAndKey()

	ctx = belt.WithField(belt.WithField(ctx, "appNameLocal", localAppName), "appNameRemote", apiKey)

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	logger.Debugf(ctx, "wait for stream '%s'", fwd.StreamID)
	pubSub := fwd.StreamServer.WaitPubsub(ctx, localAppName)
	logger.Debugf(ctx, "wait for stream '%s' result: %#+v", fwd.StreamID, pubSub)
	if pubSub == nil {
		return nil, fmt.Errorf(
			"unable to find stream ID '%s', available stream IDs: %s",
			fwd.StreamID,
			strings.Join(fwd.StreamServer.PubsubNames(), ", "),
		)
	}

	logger.Debugf(ctx, "checking if we need to pause")
	fwd.PauseFunc(ctx, fwd)
	logger.Debugf(ctx, "no pauses or pauses ended")

	return pubSub, nil
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

	pubSub, err := fwd.WaitForPublisher(ctx)
	if err != nil {
		return fmt.Errorf("unable to get publisher: %w", err)
	}

	logger.Debugf(ctx, "DestinationStreamingLocker.Lock(ctx, '%s')", fwd.DestinationID)
	destinationUnlocker := fwd.StreamForwards.DestinationStreamingLocker.Lock(ctx, fwd.DestinationID)
	defer func() {
		if destinationUnlocker != nil { // if ctx was cancelled before we locked then the unlocker is nil
			destinationUnlocker.Unlock()
		}
		logger.Debugf(ctx, "DestinationStreamingLocker.Unlock(ctx, '%s')", fwd.DestinationID)
	}()
	logger.Debugf(ctx, "/DestinationStreamingLocker.Lock(ctx, '%s')", fwd.DestinationID)

	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	_, remoteAppName, apiKey := fwd.getAppNameAndKey()

	urlParsed := ptr(*fwd.URL)
	logger.Debugf(ctx, "connecting to '%s'", fwd.URL.String())
	if fwd.URL.Port() == "" {
		switch urlParsed.Scheme {
		case "rtmp":
			urlParsed.Host += ":1935"
		case "rtmps":
			urlParsed.Host += ":443"
		default:
			return fmt.Errorf("unexpected scheme '%s' in URL '%s'", urlParsed.Scheme, urlParsed.String())
		}
	}
	var dialFunc func(protocol, addr string, config *rtmp.ConnConfig) (*rtmp.ClientConn, error)
	switch urlParsed.Scheme {
	case "rtmp":
		dialFunc = rtmp.Dial
	case "rtmps":
		dialFunc = func(protocol, addr string, config *rtmp.ConnConfig) (*rtmp.ClientConn, error) {
			return rtmp.TLSDial(protocol, addr, config, http.DefaultTransport.(*http.Transport).TLSClientConfig)
		}
	default:
		return fmt.Errorf("unexpected scheme '%s' in URL '%s'", urlParsed.Scheme, urlParsed.String())
	}
	client, err := dialFunc(urlParsed.Scheme, urlParsed.Host, &rtmp.ConnConfig{
		Logger: xlogger.LogrusFieldLoggerFromCtx(ctx),
	})
	if err != nil {
		return fmt.Errorf("unable to connect to '%s': %w", urlParsed.String(), err)
	}

	logger.Debugf(ctx, "connected to '%s'", urlParsed.String())

	fwd.Locker.Do(ctx, func() {
		fwd.Client = client
	})

	defer func() {
		fwd.Locker.Do(ctx, func() {
			if fwd.Client == nil {
				return
			}
			err := fwd.Client.Close()
			if err != nil {
				logger.Warnf(ctx, "unable to close fwd.Client: %v", err)
			}
			fwd.Client = nil
		})
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	tcURL := *urlParsed
	tcURL.Path = "/" + remoteAppName
	if tcURL.Port() == "1935" {
		tcURL.Host = tcURL.Hostname()
	}

	err = xsync.DoR1(ctx, &fwd.Locker, func() error {
		STARTPUBLISHING

		logger.Debugf(ctx, "started publishing to '%s'", urlParsed.String())
		return nil
	})
	if err != nil {
		return nil
	}

	<-fwd.Sub.ClosedChan()
	logger.Debugf(ctx, "the source stopped, so stopped also publishing to '%s'", urlParsed.String())
	return nil
}

func (fwd *ActiveStreamForwarding) Close() error {
	ctx := context.TODO()
	return xsync.DoR1(ctx, &fwd.Locker, func() error {
		if fwd.CancelFunc == nil {
			return fmt.Errorf("the stream was not started yet")
		}

		var result *multierror.Error
		if fwd.CancelFunc != nil {
			fwd.CancelFunc()
			fwd.CancelFunc = nil
		}
		if fwd.Sub != nil {
			result = multierror.Append(result, fwd.Sub.Close())
			fwd.Sub = nil
		}
		CLOSECLIENT
		return result.ErrorOrNil()
	})
}

func (fwd *ActiveStreamForwarding) String() string {
	return fmt.Sprintf("%s->%s", fwd.StreamID, fwd.DestinationID)
}
