package youtube

import (
	"context"
	"errors"
	"fmt"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	ytpkg "github.com/xaionaro-go/streamctl/pkg/streamcontrol/youtube"
	youtubesvc "google.golang.org/api/youtube/v3"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// PollingListener implements chathandler.ChatListener using YouTube Data API v3 polling.
//
// Broadcast discovery supports two modes (selected based on which fields are set):
//   - gRPC proxy: when YTProxyAddr is set, discovers via youtubeapiproxy RPCs.
//   - OAuth2 direct: when Service is set (no proxy), discovers via
//     liveBroadcasts.list mine=true using the authenticated YouTube service.
//
// A pre-configured LiveChatID bypasses discovery entirely.
type PollingListener struct {
	// Service is the YouTube Data API v3 service used for chat polling
	// and (when YTProxyAddr is empty) for broadcast discovery.
	Service *youtubesvc.Service

	YTProxyAddr  string
	ChannelID    string
	DetectMethod DetectMethod

	// LiveChatID and VideoID are optional pre-configured values.
	// When empty, the listener auto-discovers broadcasts.
	LiveChatID string
	VideoID    string

	listener *ytpkg.ChatListener
	conn     *grpc.ClientConn
	cancel   context.CancelFunc
}

func (l *PollingListener) Name() string { return "YouTube-Polling" }

func (l *PollingListener) Listen(
	ctx context.Context,
) (_ <-chan streamcontrol.Event, _err error) {
	logger.Tracef(ctx, "PollingListener.Listen")
	defer func() { logger.Tracef(ctx, "/PollingListener.Listen: %v", _err) }()

	listenCtx, cancel := context.WithCancel(ctx)
	l.cancel = cancel

	// Pre-configured liveChatID: use directly (no discovery loop).
	if l.LiveChatID != "" {
		return l.listenDirect(listenCtx)
	}

	// Auto-discovery via gRPC proxy when available.
	if l.YTProxyAddr != "" {
		return l.listenViaProxy(listenCtx, cancel)
	}

	// Auto-discovery via OAuth2 (liveBroadcasts.list mine=true).
	ch := make(chan streamcontrol.Event, 64)
	observability.Go(ctx, func(ctx context.Context) {
		defer close(ch)
		l.discoverAndPollLoopOAuth2(listenCtx, ch)
	})
	return ch, nil
}

func (l *PollingListener) listenViaProxy(
	ctx context.Context,
	cancel context.CancelFunc,
) (<-chan streamcontrol.Event, error) {
	conn, err := grpc.NewClient(l.YTProxyAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("connect to youtubeapiproxy at %s: %w", l.YTProxyAddr, err)
	}
	l.conn = conn

	ch := make(chan streamcontrol.Event, 64)

	observability.Go(ctx, func(ctx context.Context) {
		defer close(ch)
		l.discoverAndPollLoop(ctx, conn, ch)
	})

	return ch, nil
}

// listenDirect creates a single polling listener with pre-configured IDs.
func (l *PollingListener) listenDirect(
	ctx context.Context,
) (<-chan streamcontrol.Event, error) {
	client := &APIKeyChatClient{Service: l.Service}

	listener, err := ytpkg.NewChatListener(ctx, client, l.VideoID, l.LiveChatID)
	if err != nil {
		return nil, fmt.Errorf("create chat listener: %w", err)
	}
	l.listener = listener

	return listener.MessagesChan(), nil
}

// discoverAndPollLoop discovers broadcasts via gRPC proxy and polls chat messages.
// When chat ends, re-discovers the next broadcast.
func (l *PollingListener) discoverAndPollLoop(
	ctx context.Context,
	conn *grpc.ClientConn,
	ch chan<- streamcontrol.Event,
) {
	logger.Tracef(ctx, "discoverAndPollLoop")
	defer func() { logger.Tracef(ctx, "/discoverAndPollLoop") }()

	detectMethod := l.DetectMethod
	if detectMethod == "" {
		detectMethod = DetectMethodBroadcasts
	}

	client := &APIKeyChatClient{Service: l.Service}

	for {
		if ctx.Err() != nil {
			return
		}

		logger.Debugf(ctx, "discovering active broadcast for polling...")
		result, err := DiscoverBroadcast(ctx, conn, l.ChannelID, detectMethod)
		if err != nil {
			logger.Debugf(ctx, "broadcast discovery ended: %v", err)
			return
		}

		logger.Debugf(ctx, "polling chat for liveChatID=%s (video=%s)", result.LiveChatID, result.VideoID)

		listener, err := ytpkg.NewChatListener(ctx, client, result.VideoID, result.LiveChatID)
		if err != nil {
			logger.Warnf(ctx, "create chat listener: %v", err)
			if !sleepCtx(ctx, grpcReconnectDelay) {
				return
			}
			continue
		}

		forwardEvents(ctx, listener.MessagesChan(), ch)
		listener.Close(ctx)

		logger.Debugf(ctx, "polling chat ended, will re-discover")
	}
}

// discoverAndPollLoopOAuth2 discovers broadcasts using the YouTube Data API
// directly (liveBroadcasts.list mine=true) and polls chat messages.
// Used when no gRPC proxy is available.
func (l *PollingListener) discoverAndPollLoopOAuth2(
	ctx context.Context,
	ch chan<- streamcontrol.Event,
) {
	logger.Tracef(ctx, "discoverAndPollLoopOAuth2")
	defer func() { logger.Tracef(ctx, "/discoverAndPollLoopOAuth2") }()

	client := &APIKeyChatClient{Service: l.Service}

	for {
		if ctx.Err() != nil {
			return
		}

		logger.Debugf(ctx, "discovering active broadcast via OAuth2...")
		result, err := DiscoverBroadcastViaOAuth2(ctx, l.Service)
		if err != nil {
			logger.Debugf(ctx, "OAuth2 broadcast discovery ended: %v", err)
			return
		}

		logger.Debugf(ctx, "polling chat for liveChatID=%s (video=%s)", result.LiveChatID, result.VideoID)

		listener, err := ytpkg.NewChatListener(ctx, client, result.VideoID, result.LiveChatID)
		if err != nil {
			logger.Warnf(ctx, "create chat listener: %v", err)
			if !sleepCtx(ctx, broadcastPollInterval) {
				return
			}
			continue
		}

		forwardEvents(ctx, listener.MessagesChan(), ch)
		listener.Close(ctx)

		logger.Debugf(ctx, "OAuth2 polling chat ended, will re-discover")
	}
}

// forwardEvents forwards events from src to dst until src is closed or ctx is cancelled.
func forwardEvents(
	ctx context.Context,
	src <-chan streamcontrol.Event,
	dst chan<- streamcontrol.Event,
) {
	for {
		select {
		case ev, ok := <-src:
			if !ok {
				return
			}
			select {
			case dst <- ev:
			case <-ctx.Done():
				return
			}
		case <-ctx.Done():
			return
		}
	}
}

func (l *PollingListener) Close(ctx context.Context) error {
	if l.cancel != nil {
		l.cancel()
		l.cancel = nil
	}

	var errs []error

	if l.listener != nil {
		errs = append(errs, l.listener.Close(ctx))
		l.listener = nil
	}

	if l.conn != nil {
		errs = append(errs, l.conn.Close())
		l.conn = nil
	}

	return errors.Join(errs...)
}
