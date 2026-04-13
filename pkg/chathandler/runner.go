package chathandler

import (
	"context"
	"fmt"
	"time"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	scgoconv "github.com/xaionaro-go/streamctl/pkg/streamcontrol/protobuf/goconv"
	"github.com/xaionaro-go/streamctl/pkg/streamd/grpc/go/streamd_grpc"
	"google.golang.org/grpc"
)

const (
	defaultMaxRetries    = 3
	defaultBaseBackoff   = time.Second
	defaultMaxBackoff    = 30 * time.Second
	defaultKeepaliveTime = 10 * time.Second

	// minHealthyRuntime is the minimum duration a listener must run before
	// its session is considered healthy. If the listener runs at least this
	// long before failing, consecutiveFailures resets so that backoff
	// starts from base again instead of continuing from a stale high value.
	minHealthyRuntime = 30 * time.Second
)

// RunnerConfig configures the single-listener runner.
type RunnerConfig struct {
	MaxRetries    int
	BaseBackoff   time.Duration
	MaxBackoff    time.Duration
	KeepaliveTime time.Duration
}

func (c RunnerConfig) maxRetries() int {
	if c.MaxRetries > 0 {
		return c.MaxRetries
	}
	return defaultMaxRetries
}

func (c RunnerConfig) baseBackoff() time.Duration {
	if c.BaseBackoff > 0 {
		return c.BaseBackoff
	}
	return defaultBaseBackoff
}

func (c RunnerConfig) maxBackoff() time.Duration {
	if c.MaxBackoff > 0 {
		return c.MaxBackoff
	}
	return defaultMaxBackoff
}

func (c RunnerConfig) keepaliveTime() time.Duration {
	if c.KeepaliveTime > 0 {
		return c.KeepaliveTime
	}
	return defaultKeepaliveTime
}

// Runner implements chat listening with retry and keepalive injection.
// Each Runner handles exactly one listener type (PACE methodology).
type Runner struct {
	Platform      streamcontrol.PlatformName
	StreamdClient streamd_grpc.StreamDClient
	Config        RunnerConfig
	Listener      ChatListener
	ListenerType  streamcontrol.ChatListenerType
}

// NewSingleListenerRunner creates a runner for a single listener type.
// Used by per-PACE-type processes where each process handles exactly one listener.
func NewSingleListenerRunner(
	platform streamcontrol.PlatformName,
	listenerType streamcontrol.ChatListenerType,
	streamdClient streamd_grpc.StreamDClient,
	listener ChatListener,
	cfg RunnerConfig,
) *Runner {
	return &Runner{
		Platform:      platform,
		StreamdClient: streamdClient,
		Config:        cfg,
		Listener:      listener,
		ListenerType:  listenerType,
	}
}

// Run starts the main event loop. It blocks until ctx is cancelled.
// Retries the listener with exponential backoff on failure.
// Injects keepalive events periodically for health monitoring.
func (r *Runner) Run(ctx context.Context) (_err error) {
	logger.Tracef(ctx, "Run")
	defer func() { logger.Tracef(ctx, "/Run: %v", _err) }()

	return r.runSingleListenerLoop(ctx)
}

// runSingleListenerLoop retries the single listener with exponential backoff.
func (r *Runner) runSingleListenerLoop(ctx context.Context) error {
	consecutiveFailures := 0

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		logger.Debugf(ctx, "starting listener %q (%s) for %s",
			r.Listener.Name(), r.ListenerType, r.Platform)

		start := time.Now()
		err := r.runListener(ctx, r.Listener)
		if closeErr := r.Listener.Close(ctx); closeErr != nil {
			logger.Warnf(ctx, "listener %q close error: %v", r.Listener.Name(), closeErr)
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}

		// Reset backoff when the listener ran long enough to be considered
		// healthy — a failure after sustained uptime is not a rapid crash loop.
		if time.Since(start) >= minHealthyRuntime {
			consecutiveFailures = 0
		}
		consecutiveFailures++
		logger.Warnf(ctx, "listener %q (%s) for %s stopped (failure %d): %v",
			r.Listener.Name(), r.ListenerType, r.Platform, consecutiveFailures, err)

		backoff := r.calculateBackoff(consecutiveFailures)
		logger.Debugf(ctx, "retrying in %s", backoff)
		if !sleep(ctx, backoff) {
			return ctx.Err()
		}
	}
}

// runListener starts the given listener and forwards events to streamd.
// Returns when the event channel closes or ctx is cancelled.
func (r *Runner) runListener(
	ctx context.Context,
	listener ChatListener,
) error {
	listenerCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	ch, err := listener.Listen(listenerCtx)
	if err != nil {
		return fmt.Errorf("listener %q failed to start: %w", listener.Name(), err)
	}

	keepaliveTicker := time.NewTicker(r.Config.keepaliveTime())
	defer keepaliveTicker.Stop()

	eventsReceived := false
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ev, ok := <-ch:
			if !ok {
				if eventsReceived {
					return fmt.Errorf("listener %q event channel closed", listener.Name())
				}
				return fmt.Errorf("listener %q event channel closed immediately", listener.Name())
			}
			eventsReceived = true
			r.injectEvent(ctx, ev)
		case <-keepaliveTicker.C:
			r.injectKeepalive(ctx)
		}
	}
}

func (r *Runner) injectEvent(
	ctx context.Context,
	ev streamcontrol.Event,
) {
	_, err := r.StreamdClient.InjectPlatformEvent(ctx, &streamd_grpc.InjectPlatformEventRequest{
		PlatID:  string(r.Platform),
		Message: scgoconv.EventGo2GRPC(ev),
	})
	if err != nil {
		logger.Errorf(ctx, "InjectPlatformEvent failed for %s event %s: %v",
			r.Platform, ev.ID, err)
	}
}

func (r *Runner) injectKeepalive(ctx context.Context) {
	keepaliveEv := streamcontrol.Event{
		ID:        streamcontrol.EventID(fmt.Sprintf("keepalive-%s-%s-%d", r.ListenerType, r.Platform, time.Now().UnixNano())),
		CreatedAt: time.Now(),
		Type:      streamcontrol.EventTypeOther,
		User: streamcontrol.User{
			ID:   "system",
			Name: "system",
		},
		Message: &streamcontrol.Message{
			Content: fmt.Sprintf("[keepalive] chat-handler-%s/%s alive", r.Platform, r.ListenerType),
			Format:  streamcontrol.TextFormatTypePlain,
		},
	}
	_, err := r.StreamdClient.InjectPlatformEvent(ctx, &streamd_grpc.InjectPlatformEventRequest{
		PlatID:  string(r.Platform),
		Message: scgoconv.EventGo2GRPC(keepaliveEv),
	})
	if err != nil {
		logger.Warnf(ctx, "keepalive inject failed for %s: %v", r.Platform, err)
	}
}

// ConnectToStreamd dials the streamd gRPC server and returns the connection and client.
func ConnectToStreamd(
	ctx context.Context,
	addr string,
	creds grpc.DialOption,
) (*grpc.ClientConn, streamd_grpc.StreamDClient, error) {
	conn, err := grpc.NewClient(addr, creds)
	if err != nil {
		return nil, nil, fmt.Errorf("connect to streamd at %s: %w", addr, err)
	}
	return conn, streamd_grpc.NewStreamDClient(conn), nil
}

func (r *Runner) calculateBackoff(consecutiveFailures int) time.Duration {
	base := r.Config.baseBackoff()
	maxB := r.Config.maxBackoff()
	backoff := base
	for i := 1; i < consecutiveFailures; i++ {
		backoff *= 2
		if backoff > maxB {
			return maxB
		}
	}
	return backoff
}

func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}
