package runner_test

import (
	"context"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/streamctl/pkg/translator"
	translator_grpc "github.com/xaionaro-go/streamctl/pkg/translator/grpc/go/translator_grpc"
	"github.com/xaionaro-go/streamctl/pkg/translator/runner"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// readinessTimeout bounds how long a test waits on the runner's onReady
// callback. The runner fires this after listen+register but before Serve
// blocks; a healthy run reaches it in microseconds. Set high enough to be
// useful on a loaded CI host but low enough to fail fast on a wedged Run.
const readinessTimeout = 5 * time.Second

func socketPath(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	// AF_UNIX path length is limited (~108 bytes on Linux); the temp dir
	// path can be too long, so use a short relative socket name.
	return filepath.Join(dir, "t.sock")
}

// startRunner is the shared bring-up helper: pass empty streamdAddr so the
// runner skips the heartbeat goroutine entirely (a non-empty addr would dial
// nothing on "127.0.0.1:0"), build args, and block on the deterministic
// ready callback rather than polling the socket file.
func startRunner(
	t *testing.T,
	ctx context.Context,
	sock string,
) <-chan error {
	t.Helper()
	// streamdAddr="" disables the heartbeat loop. Tests that cover the
	// heartbeat path must stand up a stub gRPC server and pass its addr.
	args := translator.BuildTranslatorArgs(sock, "", parentLogLevel(), "")
	ready := make(chan struct{})
	done := make(chan error, 1)
	observability.Go(ctx, func(ctx context.Context) {
		done <- runner.Run(ctx, args, runner.WithOnReady(func() { close(ready) }))
	})
	select {
	case <-ready:
	case <-time.After(readinessTimeout):
		t.Fatalf("runner did not signal ready within %s", readinessTimeout)
	}
	return done
}

func TestRun_ListensWithin1s(t *testing.T) {
	if runtime.GOOS == "android" {
		t.Skip("UNIX socket runner not supported on android yet")
	}

	sock := socketPath(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := startRunner(t, ctx, sock)

	// Connect and Ping to confirm the server is actually serving.
	conn, err := grpc.NewClient("unix://"+sock, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	defer conn.Close()

	client := translator_grpc.NewTranslatorClient(conn)
	pingCtx, pingCancel := context.WithTimeout(context.Background(), readinessTimeout)
	defer pingCancel()
	_, err = client.Ping(pingCtx, &translator_grpc.PingRequest{})
	require.NoError(t, err, "Ping must succeed once Run is listening")

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after ctx cancel")
	}
}

func TestRun_GracefulStopOnContextCancel(t *testing.T) {
	if runtime.GOOS == "android" {
		t.Skip("UNIX socket runner not supported on android yet")
	}

	sock := socketPath(t)
	ctx, cancel := context.WithCancel(context.Background())

	done := startRunner(t, ctx, sock)

	cancel()

	select {
	case err := <-done:
		// Run must return promptly (within 2s) on ctx cancel.
		assert.NoError(t, err, "graceful shutdown must not return an error")
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return within 2s of ctx cancel")
	}
}

// parentLogLevel returns a fixed log level for use in argv.
func parentLogLevel() logger.Level { return logger.LevelInfo }
