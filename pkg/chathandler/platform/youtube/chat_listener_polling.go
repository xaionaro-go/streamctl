package youtube

import (
	"context"
	"errors"
	"fmt"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	ytpkg "github.com/xaionaro-go/streamctl/pkg/streamcontrol/youtube"
	"google.golang.org/api/option"
	youtubesvc "google.golang.org/api/youtube/v3"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// PollingListener implements chathandler.ChatListener using YouTube Data API v3 polling.
// When YTProxyAddr is set, it auto-discovers broadcasts via youtubeapiproxy.
// Otherwise, a pre-configured LiveChatID is required.
type PollingListener struct {
	APIKey       string
	YTProxyAddr  string
	ChannelID    string
	DetectMethod DetectMethod

	// LiveChatID and VideoID are optional pre-configured values.
	// When empty, the listener auto-discovers via youtubeapiproxy.
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

	svc, err := youtubesvc.NewService(ctx, option.WithAPIKey(l.APIKey))
	if err != nil {
		return nil, fmt.Errorf("create YouTube service: %w", err)
	}

	listenCtx, cancel := context.WithCancel(ctx)
	l.cancel = cancel

	// Pre-configured liveChatID: use directly (no discovery loop).
	if l.LiveChatID != "" {
		return l.listenDirect(listenCtx, svc)
	}

	// Auto-discovery mode: discover, listen, re-discover loop.
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
		l.discoverAndPollLoop(listenCtx, conn, svc, ch)
	})

	return ch, nil
}

// listenDirect creates a single polling listener with pre-configured IDs.
func (l *PollingListener) listenDirect(
	ctx context.Context,
	svc *youtubesvc.Service,
) (<-chan streamcontrol.Event, error) {
	client := &APIKeyChatClient{Service: svc}

	listener, err := ytpkg.NewChatListener(ctx, client, l.VideoID, l.LiveChatID)
	if err != nil {
		return nil, fmt.Errorf("create chat listener: %w", err)
	}
	l.listener = listener

	return listener.MessagesChan(), nil
}

// discoverAndPollLoop discovers broadcasts and polls chat messages.
// When chat ends, re-discovers the next broadcast.
func (l *PollingListener) discoverAndPollLoop(
	ctx context.Context,
	conn *grpc.ClientConn,
	svc *youtubesvc.Service,
	ch chan<- streamcontrol.Event,
) {
	logger.Tracef(ctx, "discoverAndPollLoop")
	defer func() { logger.Tracef(ctx, "/discoverAndPollLoop") }()

	detectMethod := l.DetectMethod
	if detectMethod == "" {
		detectMethod = DetectMethodBroadcasts
	}

	client := &APIKeyChatClient{Service: svc}

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
