package youtube

import (
	"context"
	"errors"
	"fmt"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	ytpkg "github.com/xaionaro-go/streamctl/pkg/streamcontrol/youtube"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// ObsoleteJSONListener implements chathandler.ChatListener using the obsolete
// YouTube JSON parser (ChatListenerOBSOLETE). This does not require API keys
// but scrapes the YouTube live chat page directly.
//
// When YTProxyAddr is set, it auto-discovers broadcasts via youtubeapiproxy.
// Otherwise, a pre-configured VideoID is required.
type ObsoleteJSONListener struct {
	YTProxyAddr  string
	ChannelID    string
	DetectMethod DetectMethod

	// VideoID is an optional pre-configured value.
	// When empty, the listener auto-discovers via youtubeapiproxy.
	VideoID string

	listener *ytpkg.ChatListenerOBSOLETE
	conn     *grpc.ClientConn
	cancel   context.CancelFunc
}

func (l *ObsoleteJSONListener) Name() string { return "YouTube-JSON-Obsolete" }

func (l *ObsoleteJSONListener) Listen(
	ctx context.Context,
) (_ <-chan streamcontrol.Event, _err error) {
	logger.Tracef(ctx, "ObsoleteJSONListener.Listen")
	defer func() { logger.Tracef(ctx, "/ObsoleteJSONListener.Listen: %v", _err) }()

	listenCtx, cancel := context.WithCancel(ctx)
	l.cancel = cancel

	// Pre-configured videoID: use directly (no discovery loop).
	if l.VideoID != "" {
		return l.listenDirect(listenCtx)
	}

	// Auto-discovery mode.
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
		l.discoverAndScrapeLoop(listenCtx, conn, ch)
	})

	return ch, nil
}

func (l *ObsoleteJSONListener) listenDirect(
	ctx context.Context,
) (<-chan streamcontrol.Event, error) {
	listener, err := ytpkg.NewChatListenerOBSOLETE(ctx, l.VideoID, nil)
	if err != nil {
		return nil, fmt.Errorf("create obsolete YouTube listener: %w", err)
	}
	l.listener = listener

	return listener.MessagesChan(), nil
}

func (l *ObsoleteJSONListener) discoverAndScrapeLoop(
	ctx context.Context,
	conn *grpc.ClientConn,
	ch chan<- streamcontrol.Event,
) {
	logger.Tracef(ctx, "discoverAndScrapeLoop")
	defer func() { logger.Tracef(ctx, "/discoverAndScrapeLoop") }()

	detectMethod := l.DetectMethod
	if detectMethod == "" {
		detectMethod = DetectMethodBroadcasts
	}

	for {
		if ctx.Err() != nil {
			return
		}

		logger.Debugf(ctx, "discovering active broadcast for obsolete scraper...")
		result, err := DiscoverBroadcast(ctx, conn, l.ChannelID, detectMethod)
		if err != nil {
			logger.Debugf(ctx, "broadcast discovery ended: %v", err)
			return
		}

		logger.Debugf(ctx, "scraping chat for videoID=%s", result.VideoID)

		listener, err := ytpkg.NewChatListenerOBSOLETE(ctx, result.VideoID, nil)
		if err != nil {
			logger.Warnf(ctx, "create obsolete listener: %v", err)
			if !sleepCtx(ctx, grpcReconnectDelay) {
				return
			}
			continue
		}

		forwardEvents(ctx, listener.MessagesChan(), ch)
		listener.Close(ctx)

		logger.Debugf(ctx, "obsolete chat ended, will re-discover")
	}
}

func (l *ObsoleteJSONListener) Close(ctx context.Context) error {
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
