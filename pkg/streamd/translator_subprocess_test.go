package streamd

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	tassert "github.com/stretchr/testify/assert"
	trequire "github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// TestRecordTranslatorActivity_NilSubprocess locks in that calling the
// activity recorder when no subprocess is registered is a safe no-op. A
// regression here would crash streamd the first time a chat-handler
// subprocess (or anything else) speculatively pinged the translator path
// before the supervisor came up.
func TestRecordTranslatorActivity_NilSubprocess(t *testing.T) {
	d := &StreamD{}
	// Must not panic.
	tassert.NotPanics(t, func() {
		d.recordTranslatorActivity(context.Background())
	})
}

// TestIsCurrentTranslator covers the supervisor's staleness guard. Register
// translator A, then replace it with B; A must no longer be considered
// current. The chat-handler equivalent in chat.go already relies on the
// same pattern; this test guarantees the translator side behaves
// identically.
func TestIsCurrentTranslator(t *testing.T) {
	d := &StreamD{}

	a := &externalTranslator{}
	b := &externalTranslator{}

	d.registerTranslator(a)
	tassert.True(t, d.isCurrentTranslator(a), "freshly registered translator must be current")

	d.registerTranslator(b)
	tassert.False(t, d.isCurrentTranslator(a), "replaced translator must NOT be current")
	tassert.True(t, d.isCurrentTranslator(b), "newly registered translator must be current")
}

// TestRegisterTranslator_CleansUpReplaced asserts that when the supervisor
// registers a new translator over an existing one, the old translator's
// cleanup side-effects fire: cancelFunc is called, the socket file is
// removed, and the gRPC ClientConn is closed. Without these the supervisor
// would leak goroutines (the in-flight heartbeat loop), socket inodes, and
// gRPC connections every time the subprocess restarts.
func TestRegisterTranslator_CleansUpReplaced(t *testing.T) {
	d := &StreamD{}

	// Stub cancelFunc: a context.CancelFunc that records when it's invoked.
	var cancelCalls atomic.Int32
	cancel := func() { cancelCalls.Add(1) }

	// Real socket file we expect registerTranslator to remove.
	dir := t.TempDir()
	sock := filepath.Join(dir, "old.sock")
	listener, err := net.Listen("unix", sock)
	trequire.NoError(t, err, "create stub socket")
	defer listener.Close()
	trequire.FileExists(t, sock, "stub socket must exist before registerTranslator runs")

	// Real ClientConn we expect to be Close()d. We dial through a passthrough
	// dialer that needs no actual server — grpc.NewClient with WithNoProxy
	// does not eagerly connect, so Close() is non-blocking.
	conn, err := grpc.NewClient("passthrough:///stub",
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	trequire.NoError(t, err, "create stub gRPC client")

	old := &externalTranslator{
		cancelFunc: cancel,
		socketPath: sock,
		conn:       conn,
	}
	d.registerTranslator(old)

	// Now register a replacement — the old translator's cleanup side-effects
	// must all fire.
	replacement := &externalTranslator{}
	d.registerTranslator(replacement)

	tassert.Equal(t, int32(1), cancelCalls.Load(),
		"replaced translator's cancelFunc must run exactly once")
	_, statErr := os.Stat(sock)
	tassert.True(t, os.IsNotExist(statErr),
		"replaced translator's socket file must be removed (stat err: %v)", statErr)
	// Connection must transition to a closed state. grpc.ClientConn does
	// not expose a public "is closed" predicate, but Close on an
	// already-closed conn returns ErrClientConnClosing on every grpc-go
	// version; we assert that calling Close() now returns that error so
	// we know registerTranslator already closed it once.
	closeErr := conn.Close()
	tassert.Error(t, closeErr,
		"replaced translator's gRPC conn must already be closed (calling Close again must error)")
}

// TestShouldWatchdogRespawn_manualReplace covers the double-spawn guard.
// When an operator-initiated TranslatorRestart / TranslatorDisable flips
// manualReplace=true on the dying subprocess, the watchdog must bow out:
// the operator path is responsible for spawning the next one. A regression
// here re-introduces the duplicate-spawn race the manualReplace flag was
// added to close.
//
// Falsification: drop the manualReplace check from shouldWatchdogRespawn
// and this test fails because the function returns true instead of false.
func TestShouldWatchdogRespawn_manualReplace(t *testing.T) {
	d := &StreamD{}
	a := &externalTranslator{}
	d.registerTranslator(a)
	trequire.True(t, d.isCurrentTranslator(a),
		"freshly registered translator must be current")

	// Baseline: with manualReplace clear AND a still the current
	// subprocess, the watchdog must re-spawn.
	tassert.True(t, d.shouldWatchdogRespawn(a),
		"current subprocess with manualReplace=false → watchdog respawns")

	// Operator path takes over.
	a.manualReplace.Store(true)
	tassert.False(t, d.shouldWatchdogRespawn(a),
		"manualReplace=true must make the watchdog bow out so it does NOT double-spawn")
}

// TestShouldWatchdogRespawn_replacedTranslator complements the
// manualReplace path: when t is no longer the current subprocess (someone
// already swapped in a replacement) the watchdog must also skip the
// re-spawn. The two-cases-OR'd-together shape is the production decision
// branch — both must short-circuit to false individually.
func TestShouldWatchdogRespawn_replacedTranslator(t *testing.T) {
	d := &StreamD{}
	a := &externalTranslator{}
	b := &externalTranslator{}

	d.registerTranslator(a)
	d.registerTranslator(b) // a is no longer current.

	tassert.False(t, d.shouldWatchdogRespawn(a),
		"replaced subprocess must NOT trigger a watchdog respawn")
	tassert.True(t, d.shouldWatchdogRespawn(b),
		"replacement subprocess (current, manual=false) → watchdog respawns")
}
