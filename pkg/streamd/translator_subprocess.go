package streamd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/streamctl/pkg/translator"
	translator_grpc "github.com/xaionaro-go/streamctl/pkg/translator/grpc/go/translator_grpc"
	"google.golang.org/grpc"
)

// externalTranslator tracks one spawned translator subprocess. The streamd
// supervisor owns lifecycle (start/stop/restart) and observes liveness via
// lastActivityTime; the in-flight TranslatorChain lives entirely inside the
// subprocess. Mirror of externalChatHandler with translator-specific bits.
type externalTranslator struct {
	cmd        *exec.Cmd
	cancelFunc context.CancelFunc
	socketPath string

	// conn is the gRPC connection to the subprocess's UNIX socket. Reused
	// across Translate / Reconfigure / Stats calls so streamd does not pay
	// the dial cost on the hot path.
	conn *grpc.ClientConn

	// client is the cached generated stub bound to conn. Built once by
	// initTranslatorClient so every Translate call avoids the per-RPC
	// reflection cost of NewTranslatorClient.
	client translator_grpc.TranslatorClient

	// lastActivityTime carries the most recent ReportTranslatorActivity
	// timestamp (UnixNano). Read by the supervisor watchdog.
	lastActivityTime atomic.Int64

	// manualReplace flips to true when an operator-initiated path
	// (TranslatorRestart / TranslatorDisable / stopTranslatorSubprocess)
	// has decided to take over lifecycle. It tells the watchdog's
	// restartTranslatorSubprocess to bow out so we don't double-spawn.
	manualReplace atomic.Bool
}

// recordTranslatorActivity updates the last-activity timestamp for the
// currently registered translator subprocess. Safe to call when no
// subprocess is registered (no-op).
func (d *StreamD) recordTranslatorActivity(
	ctx context.Context,
) {
	logger.Tracef(ctx, "recordTranslatorActivity")
	defer logger.Tracef(ctx, "/recordTranslatorActivity")

	d.translatorLocker.Lock()
	defer d.translatorLocker.Unlock()

	if d.translatorSubprocess == nil {
		return
	}
	d.translatorSubprocess.lastActivityTime.Store(time.Now().UnixNano())
}

// registerTranslator stores t as the active subprocess, cancelling and
// cleaning up any previous one. Safe to call from supervisor goroutines on
// restart paths. Mirrors registerExternalChatHandler.
//
// Lock-hold discipline (shared with stopTranslatorSubprocess):
// cancelFunc and conn.Close can both block — the former can race with
// goroutines holding heartbeat work, the latter waits for gRPC drain.
// Holding translatorLocker across either would starve concurrent
// recordTranslatorActivity callers. So both helpers snapshot under the
// lock, swap the field, RELEASE the lock, then perform the cleanup outside
// the critical section.
func (d *StreamD) registerTranslator(
	t *externalTranslator,
) {
	d.translatorLocker.Lock()
	old := d.translatorSubprocess
	d.translatorSubprocess = t
	d.translatorLocker.Unlock()

	if old == nil {
		return
	}
	if old.cancelFunc != nil {
		old.cancelFunc()
	}
	// Best-effort socket cleanup: if the old subprocess crashed without
	// removing its socket, the new one would fail to bind. Mirrors the
	// pattern used for externalChatHandler restart.
	if old.socketPath != "" {
		_ = os.Remove(old.socketPath)
	}
	if old.conn != nil {
		_ = old.conn.Close()
	}
}

// isCurrentTranslator returns true if t is still the active subprocess.
// Used by supervisor restart logic as a staleness guard.
func (d *StreamD) isCurrentTranslator(
	t *externalTranslator,
) bool {
	d.translatorLocker.Lock()
	defer d.translatorLocker.Unlock()

	return d.translatorSubprocess == t
}

// StartTranslatorSubprocess re-spawns the streamd executable in
// internal-translator mode and registers it as the active translator. The
// subprocess hosts the in-flight TranslatorChain; streamd retains no chain
// state. Mirrors StartExternalChatHandler in shape and locking discipline.
//
// ctx is the request/log ctx (used for tracing, log fields). lifeCtx is
// the daemon-lifetime ctx: it controls subprocess + monitor lifetime and
// MUST outlive any single request — pass d.runCtx from RPC handlers, the
// caller's ctx from boot/watchdog paths where it already is the
// daemon-lifetime ctx. Passing a request-scoped ctx as lifeCtx would (a)
// SIGKILL the freshly-spawned subprocess via exec.CommandContext when the
// handler returns and (b) terminate the monitor goroutine before it could
// observe the death and trigger a respawn — the bug 0022d11 fixed.
//
// Translation-enabled gating happens in reconcileTranslator; this function
// trusts its caller. The GRPCListenAddr precondition is enforced here
// regardless: it is a different concern (the subprocess heartbeats back via
// the streamd gRPC server) and the failure mode would be a wedged
// supervisor watchdog, not a silent disable.
func (d *StreamD) StartTranslatorSubprocess(
	ctx context.Context,
	lifeCtx context.Context,
) (_err error) {
	logger.Debugf(ctx, "StartTranslatorSubprocess")
	defer func() { logger.Debugf(ctx, "/StartTranslatorSubprocess: %v", _err) }()

	if lifeCtx == nil {
		return fmt.Errorf("StartTranslatorSubprocess: lifeCtx is nil — caller must pass a daemon-lifetime ctx (typically d.runCtx)")
	}
	if d.GRPCListenAddr == "" {
		// The subprocess heartbeats back via the streamd gRPC server. Without
		// an address it has no way to keep the supervisor's watchdog warm.
		return fmt.Errorf("cannot start translator: GRPCListenAddr is not set")
	}

	// Place the socket inside a per-streamd temp dir so concurrent streamd
	// instances (rare, but possible during tests) do not collide.
	socketDir, err := os.MkdirTemp("", "streamd-translator-")
	if err != nil {
		return fmt.Errorf("create translator socket dir: %w", err)
	}
	socketPath := filepath.Join(socketDir, "t.sock")

	subCtx, cancel := context.WithCancel(lifeCtx)

	args := translator.BuildTranslatorArgs(
		socketPath, d.GRPCListenAddr,
		observability.LogLevelFilter.GetLevel(),
		d.Options.LogstashAddr,
	)

	cmd, err := d.Options.SubprocessIO.spawn(subCtx, args)
	if err != nil {
		cancel()
		_ = os.RemoveAll(socketDir)
		return fmt.Errorf("start translator subprocess: %w", err)
	}

	t := &externalTranslator{
		cmd:        cmd,
		cancelFunc: cancel,
		socketPath: socketPath,
	}
	t.lastActivityTime.Store(time.Now().UnixNano())

	d.registerTranslator(t)

	// Monitor on lifeCtx so it survives the caller and can fire restart
	// after a subprocess death. Mirrors the chat-handler monitor's
	// long-lifetime requirement.
	observability.Go(lifeCtx, func(ctx context.Context) {
		d.monitorTranslatorSubprocess(ctx, t)
	})

	logger.Debugf(ctx, "started translator subprocess (pid=%d socket=%s)", cmd.Process.Pid, socketPath)
	return nil
}

// monitorTranslatorSubprocess watches one translator subprocess. If it dies
// or stops sending heartbeats, the supervisor cancels it and re-spawns.
// Mirrors monitorExternalChatHandler.
func (d *StreamD) monitorTranslatorSubprocess(
	ctx context.Context,
	t *externalTranslator,
) {
	logger.Debugf(ctx, "monitorTranslatorSubprocess")
	defer logger.Debugf(ctx, "/monitorTranslatorSubprocess")

	processDone := make(chan error, 1)
	observability.Go(ctx, func(_ context.Context) {
		processDone <- t.cmd.Wait()
	})

	ticker := time.NewTicker(subprocessHealthTimeout / 2)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case err := <-processDone:
			logger.Errorf(ctx, "translator subprocess died (pid=%d): %v — will restart", t.cmd.Process.Pid, err)
			d.restartTranslatorSubprocess(ctx, t)
			return

		case <-ticker.C:
			lastMsg := time.Unix(0, t.lastActivityTime.Load())
			if time.Since(lastMsg) <= subprocessHealthTimeout {
				continue
			}
			logger.Errorf(ctx,
				"translator subprocess unresponsive for %s — restarting",
				time.Since(lastMsg).Round(time.Second))
			t.cancelFunc()
			d.restartTranslatorSubprocess(ctx, t)
			return
		}
	}
}

// shouldWatchdogRespawn is the unit-testable spawn-decision helper for
// restartTranslatorSubprocess. Returns true iff the watchdog should
// re-spawn t, i.e.: t is still the registered subprocess AND no
// operator-initiated path has claimed lifecycle (manualReplace==false).
//
// Pulled out of restartTranslatorSubprocess so a unit test can falsify
// the decision branch without paying the subprocessRestartDelay cool-down
// or wiring a real subprocess. The doc-comment on the watchdog explains
// why double-spawning is the bug we're guarding against.
func (d *StreamD) shouldWatchdogRespawn(t *externalTranslator) bool {
	if !d.isCurrentTranslator(t) {
		return false
	}
	if t.manualReplace.Load() {
		return false
	}
	return true
}

// restartTranslatorSubprocess waits the cool-down, confirms t is still the
// current subprocess, then re-spawns and re-establishes the gRPC client.
// Without re-init, the in-flight worker would keep pointing at the dead conn.
func (d *StreamD) restartTranslatorSubprocess(
	ctx context.Context,
	t *externalTranslator,
) {
	if !sleep(ctx, subprocessRestartDelay) {
		return
	}
	// Belt-and-braces against double-spawn: an operator-initiated
	// TranslatorRestart / TranslatorDisable flips manualReplace to true on
	// the dying subprocess and is responsible for spawning the next one.
	// Skip both: a) when we're no longer the current subprocess (someone
	// already replaced us) and b) when manualReplace is set (operator-driven
	// replace is in flight).
	if !d.shouldWatchdogRespawn(t) {
		logger.Debugf(ctx, "translator subprocess already replaced (manual=%v), skipping restart", t.manualReplace.Load())
		return
	}
	// The watchdog goroutine itself runs on d.runCtx (installed in
	// StartTranslatorSubprocess), so its ctx is already the
	// daemon-lifetime ctx — pass it as both the log ctx and the lifeCtx.
	if err := d.StartTranslatorSubprocess(ctx, ctx); err != nil {
		logger.Errorf(ctx, "failed to restart translator subprocess: %v", err)
		return
	}
	if err := d.initTranslatorClient(ctx); err != nil {
		logger.Errorf(ctx, "failed to re-init translator client after restart: %v", err)
	}
}

// stopTranslatorSubprocess tears down the active translator subprocess (if
// any) and clears d.translatorSubprocess so the next reconcile sees a fresh
// disabled state. Best-effort cleanup: the subprocess monitor will reclaim
// its own resources after cancelFunc fires. Returns nil — there's nothing
// the caller can do about a failed Close beyond logging.
//
// Lock-hold discipline mirrors registerTranslator: snapshot under the
// lock, swap the field to nil, release the lock, THEN call cancelFunc /
// Close on the snapshot outside the critical section. cancelFunc and
// conn.Close can both block; holding translatorLocker across either would
// starve concurrent recordTranslatorActivity callers.
func (d *StreamD) stopTranslatorSubprocess(
	ctx context.Context,
) error {
	logger.Tracef(ctx, "stopTranslatorSubprocess")
	defer logger.Tracef(ctx, "/stopTranslatorSubprocess")

	d.translatorLocker.Lock()
	sp := d.translatorSubprocess
	d.translatorSubprocess = nil
	d.translatorLocker.Unlock()

	if sp == nil {
		return nil
	}
	sp.manualReplace.Store(true)
	if sp.cancelFunc != nil {
		sp.cancelFunc()
	}
	if sp.conn != nil {
		_ = sp.conn.Close()
	}
	return nil
}
