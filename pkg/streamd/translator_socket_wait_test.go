// These tests exercise waitForTranslatorSocket end-to-end through a real
// fsnotify watcher and a real UNIX socket bind on a t.TempDir() path — no
// mocks, no fakes. Today `go test ./pkg/streamd/` does not build because of
// pre-existing upstream API drift in the local avpipeline checkout
// (frame.SetHardwareFramesContext and hwFramesCtx.SoftwarePixelFormat are
// undefined against the current go-astiav). Once avpipeline is repaired
// these tests run unchanged; until then the helper is verified via the
// standalone reproducer documented in the original commit message. Moving
// the helper to a sub-package was rejected as out-of-scope churn for one
// helper that is intimate to translator subprocess management.
package streamd

import (
	"context"
	"net"
	"path/filepath"
	"testing"
	"time"

	tassert "github.com/stretchr/testify/assert"
	trequire "github.com/stretchr/testify/require"
)

// TestWaitForTranslatorSocket_FiresOnEvent_NotTimeout asserts the wait is
// event-driven, not poll-based. A delayed socket-create must surface within
// the same millisecond ballpark as the create — well before the 10s
// translatorSocketWaitTimeout. Falsification: revert waitForTranslatorSocket
// to a poll loop with a coarse period (e.g. 5s) and elapsed-delay grows past
// the assertion margin.
func TestWaitForTranslatorSocket_FiresOnEvent_NotTimeout(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "t.sock")

	const delay = 300 * time.Millisecond
	go func() {
		time.Sleep(delay)
		l, err := net.Listen("unix", sock)
		if err != nil {
			t.Errorf("create stub socket: %v", err)
			return
		}
		// Hold the listener until test end via t.Cleanup; closing here would
		// race the watcher's event delivery on slow CI.
		t.Cleanup(func() { _ = l.Close() })
	}()

	start := time.Now()
	err := waitForTranslatorSocket(context.Background(), sock)
	elapsed := time.Since(start)
	trequire.NoError(t, err, "wait must succeed when socket appears")

	// Event-driven: elapsed - delay must be a few ms, not seconds. We give
	// 1s of slack to absorb scheduler jitter on loaded CI.
	tassert.Less(t, elapsed, delay+1*time.Second,
		"wait returned at elapsed=%v (delay=%v) — too slow for an event-driven path",
		elapsed, delay)

	// Hard ceiling: must NOT have hit the 10s timeout.
	tassert.Less(t, elapsed, translatorSocketWaitTimeout,
		"wait hit the %s timeout instead of firing on event: elapsed=%v",
		translatorSocketWaitTimeout, elapsed)
}

// TestWaitForTranslatorSocket_AlreadyExists covers the spawn → bind race:
// the subprocess may bind before waitForTranslatorSocket installs its
// watcher, in which case the kernel will not deliver a retroactive Create
// event. The single post-Add os.Stat must catch it. Falsification: drop
// that os.Stat from the implementation and this test fails (timeout).
func TestWaitForTranslatorSocket_AlreadyExists(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "t.sock")

	l, err := net.Listen("unix", sock)
	trequire.NoError(t, err, "pre-create stub socket")
	t.Cleanup(func() { _ = l.Close() })

	start := time.Now()
	err = waitForTranslatorSocket(context.Background(), sock)
	elapsed := time.Since(start)
	trequire.NoError(t, err, "wait must succeed when socket already exists")
	tassert.Less(t, elapsed, 100*time.Millisecond,
		"wait should return immediately when socket exists, got %v", elapsed)
}

// TestWaitForTranslatorSocket_CtxCancel covers the ctx-cancel short-circuit.
// Without a working ctx wire-up, a wedged subprocess plus a daemon shutdown
// would hold the wait for the full 10s timeout.
func TestWaitForTranslatorSocket_CtxCancel(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "never.sock")

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	err := waitForTranslatorSocket(ctx, sock)
	elapsed := time.Since(start)
	tassert.ErrorIs(t, err, context.Canceled,
		"ctx cancel must surface as context.Canceled, got %v", err)
	tassert.Less(t, elapsed, 1*time.Second,
		"ctx cancel did not short-circuit promptly: elapsed=%v", elapsed)
}
