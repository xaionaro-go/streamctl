package streamd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/fsnotify/fsnotify"
	"github.com/goccy/go-yaml"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamd/api"
	translator_grpc "github.com/xaionaro-go/streamctl/pkg/translator/grpc/go/translator_grpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// translationSeparator marks the boundary between original message text and
// the translated text appended to it. Mirrors cmd/chatinjector/engine.go so
// downstream consumers see the same wire format whichever binary translates.
const translationSeparator = " -文A-> "

// All translator timeout / delay constants live in translator_timeouts.go.

// translationJob is one unit of work pushed onto d.translationJobs. The
// snapshot of platID + msg is what the worker rebuilds into a translated
// ChatMessage and feeds back through processChatMessageUpdate.
//
// id is the dedupKey computed for the InjectChatMessage event that produced
// this job; it is the identity surface for queue-list, flush, and
// dump-replay. enqueuedAt records when enqueueTranslation observed the job.
type translationJob struct {
	id         dedupKey
	platID     streamcontrol.PlatformName
	msg        api.ChatMessage
	enqueuedAt time.Time
}

// setupTranslationWorker creates the bounded job channel and spawns the
// translation worker goroutine. Called only from reconcileTranslator after
// it has confirmed translation is enabled; the disabled-translation gate
// lives there, not here, so the three lifecycle helpers
// (setupTranslationWorker / StartTranslatorSubprocess / initTranslatorClient)
// have one entry point and one place that decides "is translation on?".
//
// Idempotent across concurrent callers: the create-and-store is protected by
// translatorLocker so two simultaneous reconciles (e.g. operator-driven
// enable racing the watchdog's re-init) cannot both see translationJobs ==
// nil and spawn duplicate workers — only the first wins.
func (d *StreamD) setupTranslationWorker(
	ctx context.Context,
) {
	logger.Tracef(ctx, "setupTranslationWorker")
	defer logger.Tracef(ctx, "/setupTranslationWorker")

	d.translatorLocker.Lock()
	if d.translationJobs != nil {
		// Idempotent: the channel + worker pair is already in place from a
		// prior call (boot reconcile or an earlier mutation). Spawning a
		// second worker would race the original on every job.
		d.translatorLocker.Unlock()
		return
	}
	queueSize := d.Config.Translation.EffectiveQueueSize()
	d.translationJobs = make(chan *acceptedJob, queueSize)
	if d.translationDispositions == nil {
		d.translationDispositions = &translationDispositionLedger{}
	}
	d.translatorLocker.Unlock()

	observability.Go(ctx, d.runTranslationWorker)

	logger.Debugf(ctx, "translation worker ready: queue=%d", queueSize)
}

// initTranslatorClient dials the translator subprocess UNIX socket and ships
// the current translation config. Called from reconcileTranslator AFTER
// StartTranslatorSubprocess so the subprocess is live and the socket file
// exists, AND on the restart path from monitorTranslatorSubprocess after a
// new subprocess has been spawned.
//
// The translation worker is NOT (re)created here — that lives in
// setupTranslationWorker. This separation keeps the in-flight worker alive
// across subprocess restarts; it picks up the new conn the next time it
// enters translateOneJob (which snapshots the conn under translatorLocker).
//
// Translation-enabled gating happens in reconcileTranslator; this function
// trusts its caller. When the subprocess pointer is nil — meaning Start
// failed earlier — it logs and returns nil so a degraded translator does
// not abort the rest of streamd's startup.
func (d *StreamD) initTranslatorClient(
	ctx context.Context,
) (_err error) {
	logger.Tracef(ctx, "initTranslatorClient")
	defer func() { logger.Tracef(ctx, "/initTranslatorClient: %v", _err) }()

	tc := d.Config.Translation

	d.translatorLocker.Lock()
	sp := d.translatorSubprocess
	d.translatorLocker.Unlock()
	if sp == nil {
		logger.Warnf(ctx, "translator subprocess is not registered; translation disabled")
		return nil
	}

	if err := waitForTranslatorSocket(ctx, sp.socketPath); err != nil {
		return fmt.Errorf("wait for translator socket %q: %w", sp.socketPath, err)
	}

	conn, err := grpc.NewClient(
		"unix://"+sp.socketPath,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return fmt.Errorf("dial translator socket %q: %w", sp.socketPath, err)
	}

	// Park the conn (and the gRPC client built atop it) on the registered
	// subprocess so the worker can re-use it. translateOneJob snapshots both
	// at the start of the call so a Reconfigure-driven swap mid-flight is
	// safe.
	client := translator_grpc.NewTranslatorClient(conn)
	d.translatorLocker.Lock()
	if d.translatorSubprocess == sp {
		d.translatorSubprocess.conn = conn
		d.translatorSubprocess.client = client
	}
	d.translatorLocker.Unlock()

	configYaml, err := yaml.Marshal(tc)
	if err != nil {
		return fmt.Errorf("marshal translation config: %w", err)
	}

	reconfigureCtx, reconfigureCancel := context.WithTimeout(ctx, translatorReconfigureTimeout)
	reply, err := client.Reconfigure(reconfigureCtx, &translator_grpc.ReconfigureRequest{ConfigYaml: configYaml})
	reconfigureCancel()
	switch {
	case err != nil:
		return fmt.Errorf("initial Reconfigure: %w", err)
	case !reply.GetApplied():
		return fmt.Errorf("subprocess rejected initial config: %s", reply.GetError())
	}

	logger.Debugf(ctx, "translator client initialized: target=%q", tc.TargetLanguage)
	return nil
}

// runTranslationWorker is the goroutine loop pulling jobs and calling
// translateOneJob. Spawned by setupTranslationWorker inside
// observability.Go so panics surface through the same instrumentation as
// the rest of streamd.
//
// Shutdown drain: when ctx is cancelled (daemon-lifetime ctx fires),
// remaining jobs on the channel are drained and resolved to
// DispositionAbandonedOnShutdown so the partition stays balanced. A bare
// return without draining would leak every still-queued acceptedJob's
// disposition and trip the leaked finalizer at GC.
//
// The translationJobs channel is never closed (drainTranslationJobsOnShutdown
// uses select-default to drain, not close()), so the receive cannot observe
// an ok=false signal. Closing would require making enqueueTranslation's
// send panic-safe; keeping the channel open for the daemon's lifetime is
// simpler and correct.
func (d *StreamD) runTranslationWorker(
	ctx context.Context,
) {
	logger.Tracef(ctx, "runTranslationWorker")
	defer logger.Tracef(ctx, "/runTranslationWorker")

	for {
		select {
		case <-ctx.Done():
			d.drainTranslationJobsOnShutdown(ctx)
			return
		case job := <-d.translationJobs:
			d.removeTranslationQueueIndexHead(ctx, job)
			d.translateOneJob(ctx, job)
		}
	}
}

// drainTranslationJobsOnShutdown empties d.translationJobs, resolving each
// remaining acceptedJob to DispositionAbandonedOnShutdown. Runs inside
// runTranslationWorker after ctx.Done fires; held under
// translationQueueLocker so a concurrent TranslatorQueueFlush sees a
// coherent state (either fully drained, or its own drain finishes first
// — both leave zero unresolved jobs).
func (d *StreamD) drainTranslationJobsOnShutdown(
	ctx context.Context,
) {
	d.translationQueueLocker.Lock()
	defer d.translationQueueLocker.Unlock()
	for {
		select {
		case job := <-d.translationJobs:
			if job != nil {
				job.Resolve(ctx, DispositionAbandonedOnShutdown)
			}
		default:
			d.translationQueueIndex = nil
			return
		}
	}
}

// removeTranslationQueueIndexHead pops the head of translationQueueIndex
// after a successful channel receive. The job we just received is expected
// to be at the head (FIFO); a TranslatorQueueFlush that ran concurrently
// may have already cleared the index, in which case the slice is empty and
// we have nothing to remove. If the head id does NOT match the received
// job, an invariant has drifted — leaving the index alone (instead of
// blindly popping a wrong entry) keeps subsequent QueueList answers
// consistent with the actual queue contents and surfaces the drift loudly
// for diagnosis. Removing on mismatch would silently shift every future
// QueueList answer by one, breaking dump/replay id-based targeting.
func (d *StreamD) removeTranslationQueueIndexHead(
	ctx context.Context,
	job *acceptedJob,
) {
	d.translationQueueLocker.Lock()
	defer d.translationQueueLocker.Unlock()

	if len(d.translationQueueIndex) == 0 {
		return
	}
	if d.translationQueueIndex[0].id != job.id {
		logger.Errorf(ctx,
			"translation queue index FIFO drifted: head=%q dequeued=%q index_len=%d — leaving index untouched",
			d.translationQueueIndex[0].id, job.id, len(d.translationQueueIndex))
		return
	}
	// Copy onto a fresh slice so the previous backing array can be
	// garbage-collected; otherwise long-lived slices keep retired jobs
	// (and the api.ChatMessage they reference) alive.
	rest := make([]*acceptedJob, len(d.translationQueueIndex)-1)
	copy(rest, d.translationQueueIndex[1:])
	d.translationQueueIndex = rest
}

// translateOneJob runs a single Translate RPC to the subprocess and, on a
// successful translation that differs from the original, re-publishes the
// message via processChatMessageUpdate. Errors are logged and swallowed
// (translation is best-effort; the raw publish has already happened).
//
// Single-disposition accounting: every return path resolves the acceptedJob
// to exactly one Disposition. The mapping is:
//   - empty / already-translated content (no RPC issued) → Translated
//     (a degenerate "successful" path — the worker observed the job and
//     decided nothing needed translating).
//   - nil client (subprocess not registered or client not yet built) →
//     branch on disableInProgress: AbandonedOnDisable vs AbandonedOn-
//     SubprocessDeath.
//   - transport / RPC error → same disable-vs-death branch, with a
//     ctx.Err() short-circuit to AbandonedOnShutdown when ctx is gone.
//   - in-band reply.Error → map reply.Outcome to a Disposition; UNSPECIFIED
//     falls back to AllProvidersFailed (the chain attempted, every
//     provider failed).
//   - successful Translate → map reply.Outcome to the matching 6-bucket
//     chain disposition.
func (d *StreamD) translateOneJob(
	ctx context.Context,
	job *acceptedJob,
) {
	logger.Tracef(ctx, "translateOneJob(id=%q plat=%q)", job.msg.ID, job.platID)
	defer logger.Tracef(ctx, "/translateOneJob(id=%q plat=%q)", job.msg.ID, job.platID)

	ev := job.msg.Event
	if ev.Message == nil || ev.Message.Content == "" {
		// Degenerate case: nothing to translate. Resolve as Translated so
		// the partition stays balanced; an empty-message event was still
		// counted against the offered counter at enqueue time.
		job.Resolve(ctx, DispositionTranslated)
		return
	}
	original := ev.Message.Content

	// Skip if upstream already appended a translation (e.g. when chatinjector
	// is also running). Translating "original -文A-> translated" again would
	// feed the LLM a garbled mixed string.
	if strings.Contains(original, translationSeparator) {
		job.Resolve(ctx, DispositionAlreadyTarget)
		return
	}

	client := d.snapshotTranslatorClient()
	if client == nil {
		job.Resolve(ctx, d.classifyAbandon(ctx))
		return
	}

	user := ev.User.Name
	if user == "" {
		user = string(ev.User.ID)
	}

	translateCtx, cancel := context.WithTimeout(ctx, translatorPerCallTimeout)
	reply, err := client.Translate(translateCtx, &translator_grpc.TranslateRequest{
		User:    user,
		Message: original,
	})
	cancel()
	switch {
	case err != nil:
		logger.Warnf(ctx, "translate RPC failed for %s: %v", ev.ID, err)
		job.Resolve(ctx, d.classifyAbandon(ctx))
		return
	case reply.GetError() != "":
		// In-band, application-level failure (no chain loaded yet, chain
		// translate failed, etc.) per the Translator service rule. Log
		// like a transport error so operators see the same shape.
		logger.Warnf(ctx, "translate RPC returned in-band error for %s: %s",
			ev.ID, reply.GetError())
		job.Resolve(ctx, dispositionFromOutcome(reply.GetOutcome(), DispositionAllProvidersFailed))
		return
	}
	// reply.Error == "" and err == nil: chain produced an outcome. Map it to
	// the streamd-side disposition; UNSPECIFIED is unexpected here so we
	// log + fall back to AllProvidersFailed (the chain accepted but did not
	// produce a usable result).
	disposition := dispositionFromOutcome(reply.GetOutcome(), DispositionAllProvidersFailed)
	switch {
	case reply.GetResult() == "":
		// Already in target language or detection skipped — nothing to publish.
		logger.Tracef(ctx, "translator returned empty result for %s; skipping update", ev.ID)
		job.Resolve(ctx, disposition)
		return
	case reply.GetResult() == original:
		// No change — would just duplicate the raw publish.
		logger.Tracef(ctx, "translator result equals original for %s; skipping update", ev.ID)
		job.Resolve(ctx, disposition)
		return
	}

	updated := job.msg
	updatedEv := updated.Event
	updatedMsg := *updatedEv.Message
	updatedMsg.Content = original + translationSeparator + reply.GetResult()
	updatedEv.Message = &updatedMsg
	updated.Event = updatedEv
	// Updates carry IsLive=false: the live notification fired on the raw publish
	// and the storage path also forces IsLive=false; keeping it false on the
	// event-bus side prevents the streampanel from re-firing notifications.
	updated.IsLive = false

	if err := d.processChatMessageUpdate(ctx, updated); err != nil {
		logger.Errorf(ctx, "processChatMessageUpdate failed for %s: %v", ev.ID, err)
	}
	job.Resolve(ctx, disposition)
}

// classifyAbandon maps a translateOneJob abandonment reason to the right
// Disposition based on the daemon's current shutdown / disable state. The
// three branches are mutually exclusive at observation time:
//   - ctx.Err() != nil → AbandonedOnShutdown (the worker ctx is gone, so
//     daemon-lifetime cancellation has fired).
//   - disableInProgress.Load() → AbandonedOnDisable (operator-driven).
//   - else → AbandonedOnSubprocessDeath (subprocess is unreachable for
//     reasons other than disable/shutdown — crash, restart in flight).
func (d *StreamD) classifyAbandon(
	ctx context.Context,
) Disposition {
	if ctx.Err() != nil {
		return DispositionAbandonedOnShutdown
	}
	if d.disableInProgress.Load() {
		return DispositionAbandonedOnDisable
	}
	return DispositionAbandonedOnSubprocessDeath
}

// dispositionFromOutcome maps a translator_grpc.Outcome to its streamd-side
// Disposition. UNSPECIFIED falls back to fallback (used by the in-band
// error path which expects the chain's outcome to be populated, but tolerates
// the case where it isn't).
func dispositionFromOutcome(
	o translator_grpc.Outcome,
	fallback Disposition,
) Disposition {
	switch o {
	case translator_grpc.Outcome_OUTCOME_TRANSLATED:
		return DispositionTranslated
	case translator_grpc.Outcome_OUTCOME_ALREADY_TARGET:
		return DispositionAlreadyTarget
	case translator_grpc.Outcome_OUTCOME_DETECT_FAILED:
		return DispositionDetectFailed
	case translator_grpc.Outcome_OUTCOME_SPELLING_ONLY:
		return DispositionSpellingOnly
	case translator_grpc.Outcome_OUTCOME_ALL_PROVIDERS_FAILED:
		return DispositionAllProvidersFailed
	case translator_grpc.Outcome_OUTCOME_SKIPPED_QUEUE_FULL:
		return DispositionChainSkippedQueueFull
	default:
		return fallback
	}
}

// snapshotTranslatorClient returns the cached translator gRPC client that
// initTranslatorClient parked on the active subprocess, or nil when no
// subprocess is registered or no client has been built yet. Snapshotting
// under translatorLocker keeps a Reconfigure-driven swap mid-flight safe.
func (d *StreamD) snapshotTranslatorClient() translator_grpc.TranslatorClient {
	d.translatorLocker.Lock()
	defer d.translatorLocker.Unlock()

	if d.translatorSubprocess == nil {
		return nil
	}
	return d.translatorSubprocess.client
}

// waitForTranslatorSocket blocks until the translator subprocess has bound
// its UNIX socket at socketPath, or translatorSocketWaitTimeout elapses, or
// ctx is cancelled.
//
// Mechanism: subscribe to filesystem events on the parent directory via
// fsnotify (inotify on Linux, kqueue on BSD/macOS, FEN on illumos,
// ReadDirectoryChangesW on Windows) and return as soon as we observe a
// Create event for the socket basename. Watching the parent directory is
// the recommended fsnotify pattern — adding a watch on a not-yet-existing
// file would itself fail, and watching the file post-bind defeats the
// purpose. The per-streamd socket directory only ever holds this one
// socket, so directory-level events are unambiguous.
//
// The bind IS the event we care about; per go-coding-style "Timeouts are a
// design smell when the event is observable", the kernel's filesystem
// notification is exactly that observable event. translatorSocketWaitTimeout
// is retained as the upper bound on top of the event (for genuine wedge:
// exec succeeded but the subprocess never reaches net.Listen), not as the
// detection mechanism.
//
// Race handling: the subprocess can win the spawn-to-bind race before we
// install the watcher. We do exactly one os.Stat after Add() to catch the
// already-bound case; this is a single check, not a poll loop.
func waitForTranslatorSocket(
	ctx context.Context,
	socketPath string,
) (_err error) {
	logger.Tracef(ctx, "waitForTranslatorSocket(%q)", socketPath)
	defer func() { logger.Tracef(ctx, "/waitForTranslatorSocket(%q): %v", socketPath, _err) }()

	socketDir := filepath.Dir(socketPath)
	socketName := filepath.Base(socketPath)

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("create fsnotify watcher: %w", err)
	}
	defer func() { _ = watcher.Close() }()

	if err := watcher.Add(socketDir); err != nil {
		return fmt.Errorf("watch %q: %w", socketDir, err)
	}

	// Single post-Add stat catches the spawn → bind → Add race; if the
	// subprocess already bound before our watcher was installed, the
	// kernel will not deliver a retroactive Create event.
	if _, err := os.Stat(socketPath); err == nil {
		return nil
	}

	timeoutCh := time.After(translatorSocketWaitTimeout)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timeoutCh:
			return fmt.Errorf("socket did not appear within %s", translatorSocketWaitTimeout)
		case ev, ok := <-watcher.Events:
			if !ok {
				return fmt.Errorf("fsnotify events channel closed before socket appeared")
			}
			if filepath.Base(ev.Name) != socketName {
				continue
			}
			if !ev.Has(fsnotify.Create) {
				continue
			}
			return nil
		case err, ok := <-watcher.Errors:
			if !ok {
				return fmt.Errorf("fsnotify errors channel closed before socket appeared")
			}
			return fmt.Errorf("fsnotify watcher error: %w", err)
		}
	}
}
