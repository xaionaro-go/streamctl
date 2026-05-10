// Package runner implements the long-running entrypoint of the translator
// subprocess. Run blocks until the supplied context is cancelled, serving
// the Translator gRPC service on a UNIX domain socket.
package runner

import (
	"context"
	"fmt"
	"net"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/streamctl/pkg/chathandler"
	"github.com/xaionaro-go/streamctl/pkg/streamd/grpc/go/streamd_grpc"
	"github.com/xaionaro-go/streamctl/pkg/translator"
	translator_grpc "github.com/xaionaro-go/streamctl/pkg/translator/grpc/go/translator_grpc"
	"github.com/xaionaro-go/streamctl/pkg/translator/server"
	"google.golang.org/grpc"
)

// heartbeatInterval is the cadence at which the translator subprocess sends
// ReportTranslatorActivity to streamd. The streamd-side supervisor watches
// for missed heartbeats to detect a wedged subprocess. Decoupled from the
// Translate handler so a stuck translation cannot starve the watchdog.
const heartbeatInterval = 5 * time.Second

// Option configures a single Run invocation. The Options pattern lets tests
// inject a readiness callback without burdening production callers (init in
// pkg/translator/process) with extra arguments.
type Option interface{ apply(*runConfig) }

// Options is a list of Option values applied left-to-right by Run.
type Options []Option

func (opts Options) config() runConfig {
	var cfg runConfig
	for _, o := range opts {
		o.apply(&cfg)
	}
	return cfg
}

// runConfig is the materialised view of all Options applied to one Run call.
type runConfig struct {
	onReady func()
}

type optionOnReady func()

func (o optionOnReady) apply(c *runConfig) { c.onReady = (func())(o) }

// WithOnReady returns an Option that invokes fn synchronously after the gRPC
// listener has been created and registered, immediately before
// grpcServer.Serve blocks. Used by tests to replace wall-clock polling with
// a deterministic readiness signal. fn must not block; if it needs to
// signal a goroutine, use a non-blocking channel send (e.g. close(ch)).
func WithOnReady(fn func()) Option { return optionOnReady(fn) }

// Run spawns the Translator gRPC server on the UNIX socket carried in args
// and blocks until ctx is cancelled. Returns nil on graceful shutdown,
// non-nil on listen/serve errors before shutdown.
//
// args follows the format produced by translator.BuildTranslatorArgs.
func Run(
	ctx context.Context,
	args []string,
	opts ...Option,
) (_err error) {
	logger.Tracef(ctx, "runner.Run")
	defer func() { logger.Tracef(ctx, "/runner.Run: %v", _err) }()

	cfg := Options(opts).config()

	socketPath, streamdAddr, err := parseArgs(args)
	if err != nil {
		return fmt.Errorf("parse args: %w", err)
	}

	listener, err := newListener(ctx, socketPath)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}

	srv := server.New()
	grpcServer := grpc.NewServer()
	translator_grpc.RegisterTranslatorServer(grpcServer, srv)

	// Heartbeat goroutine reports liveness to streamd. Decoupled from the
	// Translate handler so a slow translation cannot stall the watchdog.
	// streamdAddr=="" disables heartbeating entirely (used by unit tests
	// that don't need a streamd-side server).
	if streamdAddr != "" {
		observability.Go(ctx, func(ctx context.Context) {
			runHeartbeatLoop(ctx, streamdAddr)
		})
	}

	// Stop the gRPC server when ctx is cancelled. GracefulStop drains
	// in-flight calls; the listener.Close happens automatically when
	// GracefulStop returns.
	observability.Go(ctx, func(ctx context.Context) {
		<-ctx.Done()
		grpcServer.GracefulStop()
	})

	// Debug, not Info: chathandler precedent (the comparable subprocess) uses
	// Debug for startup; per style Info is reserved for rare events.
	logger.Debugf(ctx, "translator subprocess listening on %s", socketPath)

	// Fire the readiness callback after listen is up but before Serve
	// blocks. Tests rely on this to avoid polling the socket file.
	if cfg.onReady != nil {
		cfg.onReady()
	}

	if err := grpcServer.Serve(listener); err != nil {
		// gRPC's Serve returns a non-nil error when stopped via Stop on
		// some Go versions; GracefulStop is documented to return nil.
		// Still, treat any error as "served & stopped" once ctx is done.
		if ctx.Err() != nil {
			return nil
		}
		return fmt.Errorf("grpc Serve: %w", err)
	}
	return nil
}

// parseArgs extracts the socket path and streamd address from the argv
// produced by translator.BuildTranslatorArgs. Returns descriptive errors so
// init() can surface misconfiguration via the parent's stderr.
func parseArgs(args []string) (string, string, error) {
	socketPath := flagValue(args, translator.FlagTranslatorSocketPath)
	if socketPath == "" {
		return "", "", fmt.Errorf("--%s is required", translator.FlagTranslatorSocketPath)
	}
	streamdAddr := flagValue(args, translator.FlagTranslatorStreamdAddr)
	return socketPath, streamdAddr, nil
}

// flagValue scans args for "--name value" or "--name=value" and returns the
// value (or "" when not present).
func flagValue(args []string, name string) string {
	target := "--" + name
	for i, arg := range args {
		switch {
		case strings.HasPrefix(arg, target+"="):
			return arg[len(target)+1:]
		case arg == target && i+1 < len(args):
			return args[i+1]
		}
	}
	return ""
}

// newListener creates a UNIX domain socket listener after first removing any
// stale socket file at the path. On android we will fall back to a TCP
// loopback socket plus a portfile (FUTURE — not implemented in this sub-
// iteration); for now we error out so the misconfiguration is loud.
func newListener(
	ctx context.Context,
	socketPath string,
) (net.Listener, error) {
	if runtime.GOOS == "android" {
		// TODO(android): bind to 127.0.0.1:0 and write the picked port to
		// "<socketPath>.port" so the streamd-side supervisor can read it.
		// AF_UNIX is unreliable on the platform's app sandbox.
		return nil, fmt.Errorf("translator subprocess on android requires TCP fallback (not implemented)")
	}

	// Remove any leftover socket from a previous (crashed) subprocess.
	// Without this, net.Listen would fail with EADDRINUSE.
	if err := os.Remove(socketPath); err != nil && !os.IsNotExist(err) {
		logger.Warnf(ctx, "failed to remove stale socket %q: %v", socketPath, err)
	}

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("listen unix %q: %w", socketPath, err)
	}
	return listener, nil
}

// runHeartbeatLoop periodically calls streamd.ReportTranslatorActivity. It
// reuses chathandler.DetectTransportCredentials for TLS auto-negotiation so
// the subprocess works against both TLS and plaintext streamd endpoints.
//
// Caller short-circuits when streamdAddr is empty; this function assumes a
// non-empty address.
func runHeartbeatLoop(
	ctx context.Context,
	streamdAddr string,
) {
	logger.Tracef(ctx, "runHeartbeatLoop")
	defer logger.Tracef(ctx, "/runHeartbeatLoop")

	creds := chathandler.DetectTransportCredentials(ctx, streamdAddr)
	conn, err := grpc.NewClient(streamdAddr, creds)
	if err != nil {
		logger.Warnf(ctx, "heartbeat dial failed: %v", err)
		return
	}
	defer conn.Close()

	client := streamd_grpc.NewStreamDClient(conn)
	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			callCtx, cancel := context.WithTimeout(ctx, heartbeatInterval)
			_, err := client.ReportTranslatorActivity(callCtx, &streamd_grpc.ReportTranslatorActivityRequest{})
			cancel()
			if err != nil {
				logger.Warnf(ctx, "heartbeat: %v", err)
			}
		}
	}
}
