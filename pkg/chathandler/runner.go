package chathandler

import (
	"context"
	"fmt"
	"sync/atomic"
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
)

// RunnerConfig configures the fallback chain runner.
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

// Runner implements a two-method fallback chain for chat listening.
// It reads events from the primary listener, falling back to the
// secondary listener after MaxRetries consecutive failures with
// exponential backoff. All transitions are logged at ERROR level
// and a diagnostic event is injected into streamd.
type Runner struct {
	platform      streamcontrol.PlatformName
	streamdClient streamd_grpc.StreamDClient
	primary       ChatListener
	fallback      ChatListener
	config        RunnerConfig

	// fallbackActive tracks whether we are currently using the fallback listener.
	fallbackActive atomic.Bool
}

// NewRunner creates a new fallback chain runner.
func NewRunner(
	platform streamcontrol.PlatformName,
	streamdClient streamd_grpc.StreamDClient,
	primary ChatListener,
	fallback ChatListener,
	cfg RunnerConfig,
) *Runner {
	return &Runner{
		platform:      platform,
		streamdClient: streamdClient,
		primary:       primary,
		fallback:      fallback,
		config:        cfg,
	}
}

// Run starts the main event loop. It blocks until ctx is cancelled.
//
// The loop:
// 1. Starts the current listener (primary or fallback).
// 2. Reads events and injects them into streamd via InjectChatMessage.
// 3. When the event channel closes, increments the retry counter.
// 4. After MaxRetries consecutive failures, switches to the other listener.
// 5. Periodically sends keepalive events so streamd can detect liveness.
func (r *Runner) Run(ctx context.Context) (_err error) {
	logger.Tracef(ctx, "Run")
	defer func() { logger.Tracef(ctx, "/Run: %v", _err) }()

	consecutiveFailures := 0

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		current := r.currentListener()
		logger.Debugf(ctx, "starting listener %q for %s", current.Name(), r.platform)

		err := r.runListener(ctx, current)
		// Ensure resources are released after the listener stops.
		if closeErr := current.Close(ctx); closeErr != nil {
			logger.Warnf(ctx, "listener %q close error: %v", current.Name(), closeErr)
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}

		consecutiveFailures++
		logger.Warnf(ctx, "listener %q for %s stopped (failure %d/%d): %v",
			current.Name(), r.platform, consecutiveFailures, r.config.maxRetries(), err)

		if consecutiveFailures >= r.config.maxRetries() {
			r.switchListener(ctx, current.Name(), err)
			consecutiveFailures = 0
		}

		backoff := r.calculateBackoff(consecutiveFailures)
		logger.Debugf(ctx, "retrying in %s", backoff)
		if !sleep(ctx, backoff) {
			return ctx.Err()
		}
	}
}

// IsFallbackActive returns whether the runner is currently using the fallback listener.
func (r *Runner) IsFallbackActive() bool {
	return r.fallbackActive.Load()
}

func (r *Runner) currentListener() ChatListener {
	if r.fallbackActive.Load() {
		return r.fallback
	}
	return r.primary
}

// switchListener transitions between primary and fallback.
// This is ALWAYS loud: ERROR log + diagnostic event injection.
func (r *Runner) switchListener(
	ctx context.Context,
	failedName string,
	failErr error,
) {
	wasFallback := r.fallbackActive.Load()
	r.fallbackActive.Store(!wasFallback)

	target := r.currentListener()

	switch wasFallback {
	case false:
		// Primary → Fallback
		logger.Errorf(ctx,
			"FALLBACK ACTIVATED: %s %s failed, switching to %s: %v",
			r.platform, failedName, target.Name(), failErr)
		r.injectDiagnosticEvent(ctx, fmt.Sprintf(
			"FALLBACK ACTIVATED: %s %s failed, switching to %s",
			r.platform, failedName, target.Name()))
	case true:
		// Fallback → Primary (recovery)
		logger.Infof(ctx,
			"PRIMARY RESTORED: %s %s reconnected, deactivating fallback %s",
			r.platform, target.Name(), failedName)
		r.injectDiagnosticEvent(ctx, fmt.Sprintf(
			"PRIMARY RESTORED: %s %s reconnected",
			r.platform, target.Name()))
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

	keepaliveTicker := time.NewTicker(r.config.keepaliveTime())
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
	_, err := r.streamdClient.InjectChatMessage(ctx, &streamd_grpc.InjectChatMessageRequest{
		PlatID: string(r.platform),
		Event:  scgoconv.EventGo2GRPC(ev),
	})
	if err != nil {
		logger.Errorf(ctx, "InjectChatMessage failed for %s event %s: %v",
			r.platform, ev.ID, err)
	}
}

func (r *Runner) injectKeepalive(ctx context.Context) {
	keepaliveEv := streamcontrol.Event{
		ID:        streamcontrol.EventID(fmt.Sprintf("keepalive-%s-%d", r.platform, time.Now().UnixNano())),
		CreatedAt: time.Now(),
		Type:      streamcontrol.EventTypeOther,
		User: streamcontrol.User{
			ID:   "system",
			Name: "system",
		},
		Message: &streamcontrol.Message{
			Content: fmt.Sprintf("[keepalive] chat-handler-%s alive", r.platform),
			Format:  streamcontrol.TextFormatTypePlain,
		},
	}
	_, err := r.streamdClient.InjectChatMessage(ctx, &streamd_grpc.InjectChatMessageRequest{
		PlatID: string(r.platform),
		Event:  scgoconv.EventGo2GRPC(keepaliveEv),
	})
	if err != nil {
		logger.Warnf(ctx, "keepalive inject failed for %s: %v", r.platform, err)
	}
}

func (r *Runner) injectDiagnosticEvent(
	ctx context.Context,
	message string,
) {
	diagEv := streamcontrol.Event{
		ID:        streamcontrol.EventID(fmt.Sprintf("diag-%s-%d", r.platform, time.Now().UnixNano())),
		CreatedAt: time.Now(),
		Type:      streamcontrol.EventTypeOther,
		User: streamcontrol.User{
			ID:   "system",
			Name: "system",
		},
		Message: &streamcontrol.Message{
			Content: fmt.Sprintf("[DIAGNOSTIC] %s", message),
			Format:  streamcontrol.TextFormatTypePlain,
		},
	}
	_, err := r.streamdClient.InjectChatMessage(ctx, &streamd_grpc.InjectChatMessageRequest{
		PlatID: string(r.platform),
		Event:  scgoconv.EventGo2GRPC(diagEv),
	})
	if err != nil {
		logger.Errorf(ctx, "failed to inject diagnostic event for %s: %v", r.platform, err)
	}
}

func (r *Runner) calculateBackoff(consecutiveFailures int) time.Duration {
	base := r.config.baseBackoff()
	maxB := r.config.maxBackoff()
	backoff := base
	for i := 1; i < consecutiveFailures; i++ {
		backoff *= 2
		if backoff > maxB {
			return maxB
		}
	}
	return backoff
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
