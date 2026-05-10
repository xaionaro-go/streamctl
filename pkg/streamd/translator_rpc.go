package streamd

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/goccy/go-yaml"
	"github.com/xaionaro-go/streamctl/pkg/streamd/api"
	"github.com/xaionaro-go/streamctl/pkg/streamd/config"
	translator_grpc "github.com/xaionaro-go/streamctl/pkg/translator/grpc/go/translator_grpc"
)

// All translator timeout / delay constants live in translator_timeouts.go.

// TranslatorStats returns a snapshot of streamd's view of the translator
// (queue depth, drops, subprocess registration) augmented with the
// subprocess's own counter snapshot when reachable.
//
// Behaviour:
//   - No subprocess registered (translation disabled or startup failed) →
//     Running:false, queue fields zero, no error.
//   - Subprocess registered but no client yet (initTranslatorClient hasn't
//     run / failed) → Running:true (subprocess is spawned), queue fields
//     populated, SubprocessStats nil, no error.
//   - Subprocess registered + client built → call Stats with
//     translatorStatsTimeout. On RPC error, return the partial snapshot
//     with SubprocessStats nil AND a wrapped error so the caller can choose
//     to surface both.
//
// Dual-return-on-failure: the third bullet intentionally returns
// (non-nil snapshot, non-nil err). Callers MUST inspect both — the snapshot
// still carries streamd-side queue counters that are valid even when the
// subprocess Stats RPC failed, and the error explains why SubprocessStats is
// nil. Treating err != nil as "no usable data" would discard ingestion
// telemetry the operator needs to diagnose a stuck queue.
func (d *StreamD) TranslatorStats(
	ctx context.Context,
) (_ret *api.TranslatorStatsSnapshot, _err error) {
	logger.Tracef(ctx, "TranslatorStats")
	defer func() { logger.Tracef(ctx, "/TranslatorStats: %v", _err) }()

	snapshot := &api.TranslatorStatsSnapshot{
		QueueDrops: d.translationWorkerQueueDrops.Load(),
	}
	// Snapshot of the channel's current depth and capacity. len/cap on a
	// nil channel both return 0, so the disabled case is naturally covered.
	snapshot.QueueLen = uint32(len(d.translationJobs))
	snapshot.QueueCap = uint32(cap(d.translationJobs))
	if l := d.translationDispositions; l != nil {
		snapshot.TotalOffered = l.totalOffered.Load()
		snapshot.TotalEnqueued = l.totalEnqueued.Load()
		snapshot.TotalResolved = l.totalResolved.Load()
		snapshot.QueueFullAtEnqueue = l.queueFullAtEnqueue.Load()
		snapshot.OfferedWithTranslationDisabled = l.offeredWithTranslationDisabled.Load()
		snapshot.Dispositions = l.Snapshot()
	}

	d.translatorLocker.Lock()
	sp := d.translatorSubprocess
	d.translatorLocker.Unlock()
	if sp == nil {
		return snapshot, nil
	}

	snapshot.Running = true
	snapshot.LastActivityUnixNano = sp.lastActivityTime.Load()
	if sp.cmd != nil && sp.cmd.Process != nil {
		snapshot.PID = int32(sp.cmd.Process.Pid)
	}
	snapshot.SocketPath = sp.socketPath

	client := sp.client
	if client == nil {
		// Subprocess spawned but client hasn't been built yet (e.g. mid
		// initTranslatorClient or a restart in flight). Surface the queue
		// counters so the operator still sees ingestion state.
		return snapshot, nil
	}

	statsCtx, cancel := context.WithTimeout(ctx, translatorStatsTimeout)
	defer cancel()
	reply, err := client.Stats(statsCtx, &translator_grpc.StatsRequest{})
	if err != nil {
		return snapshot, fmt.Errorf("translator subprocess Stats RPC failed: %w", err)
	}
	snapshot.SubprocessStats = reply
	return snapshot, nil
}

// TranslatorReload re-marshals the current d.Config.Translation to YAML and
// forwards it to the subprocess via Reconfigure. The boolean return mirrors
// the subprocess's `applied` flag: true when the new chain swapped in,
// false when the subprocess kept the old chain (in which case the in-band
// error is wrapped and returned alongside applied=false).
//
// Errors:
//   - No subprocess registered → returns false + a clear error so the CLI
//     does not silently report success when translation is disabled.
//   - Reconfigure RPC error → returns false + wrapped error.
//   - Reconfigure replied applied=false → returns false + wrapped in-band
//     error (the subprocess kept the old chain).
func (d *StreamD) TranslatorReload(
	ctx context.Context,
) (_applied bool, _err error) {
	logger.Tracef(ctx, "TranslatorReload")
	defer func() { logger.Tracef(ctx, "/TranslatorReload: applied=%v err=%v", _applied, _err) }()

	d.translatorLocker.Lock()
	sp := d.translatorSubprocess
	d.translatorLocker.Unlock()
	if sp == nil {
		return false, errors.New("translator subprocess not registered")
	}
	client := sp.client
	if client == nil {
		return false, errors.New("translator subprocess client not initialised")
	}

	configYaml, err := yaml.Marshal(d.Config.Translation)
	if err != nil {
		return false, fmt.Errorf("marshal translation config: %w", err)
	}

	reconfigureCtx, cancel := context.WithTimeout(ctx, translatorReconfigureTimeout)
	defer cancel()
	reply, err := client.Reconfigure(reconfigureCtx, &translator_grpc.ReconfigureRequest{ConfigYaml: configYaml})
	switch {
	case err != nil:
		return false, fmt.Errorf("translator subprocess Reconfigure RPC failed: %w", err)
	case !reply.GetApplied():
		return false, fmt.Errorf("translator subprocess rejected new config: %s", reply.GetError())
	}

	return true, nil
}

// reconcileTranslator brings runtime translation state in line with
// d.Config.Translation. The single gate for translation-enabled checks:
// every other helper (setupTranslationWorker, StartTranslatorSubprocess,
// initTranslatorClient) trusts its caller and skips the
// "is TargetLanguage empty?" check, so the answer lives here only.
// Idempotent: calling twice with the same target stays in the same state
// (no spawn/kill churn).
//
// Called at boot from Run() and at runtime from TranslatorEnable /
// TranslatorDisable after a config mutation.
//
// The four cases are exhaustive on (desired, running):
//   - desired=="" && !running: nothing to do.
//   - desired=="" &&  running: stop the subprocess.
//   - desired!="" && !running: spawn fresh subprocess + dial it.
//   - desired!="" &&  running: live edit — push the new config via Reload.
// ctx is the request/log ctx; lifeCtx is the daemon-lifetime ctx that
// governs any subprocess this reconcile spawns. See StartTranslatorSubprocess
// for the lifeCtx contract.
func (d *StreamD) reconcileTranslator(
	ctx context.Context,
	lifeCtx context.Context,
) (_err error) {
	logger.Tracef(ctx, "reconcileTranslator")
	defer func() { logger.Tracef(ctx, "/reconcileTranslator: %v", _err) }()

	desired := d.Config.Translation.TargetLanguage
	d.translatorLocker.Lock()
	running := d.translatorSubprocess != nil
	d.translatorLocker.Unlock()

	switch {
	case desired == "" && !running:
		return nil
	case desired == "" && running:
		return d.stopTranslatorSubprocess(ctx)
	case desired != "" && !running:
		d.setupTranslationWorker(ctx)
		if err := d.StartTranslatorSubprocess(ctx, lifeCtx); err != nil {
			return fmt.Errorf("start: %w", err)
		}
		return d.initTranslatorClient(ctx)
	default:
		// desired != "" && running — live edit; push via Reload.
		applied, err := d.TranslatorReload(ctx)
		if err != nil {
			return err
		}
		if !applied {
			return errors.New("subprocess rejected reload")
		}
		return nil
	}
}

// mutateTranslationConfig is the orchestration helper for any RPC that
// touches d.Config.Translation. Wraps Get → mutate → Set → Save in a
// SINGLE ConfigLock.Do critical section so two concurrent mutators cannot
// interleave and lose one of their writes (operator A reads, B reads, A
// sets, B sets — A's mutation lost). The inner GetConfig / SetConfig /
// SaveConfig each take ConfigLock internally; gorex.Mutex is reentrant
// (same goroutine may re-lock, depth-tracked) so calling them under an
// already-held ConfigLock is safe and keeps the helper a thin wrapper
// around the public methods.
func (d *StreamD) mutateTranslationConfig(
	ctx context.Context,
	fn func(*config.TranslationConfig),
) error {
	var resultErr error
	d.ConfigLock.Do(ctx, func() {
		cfg, err := d.GetConfig(ctx)
		if err != nil {
			resultErr = fmt.Errorf("GetConfig: %w", err)
			return
		}
		fn(&cfg.Translation)
		if err := d.SetConfig(ctx, cfg); err != nil {
			resultErr = fmt.Errorf("SetConfig: %w", err)
			return
		}
		resultErr = d.SaveConfig(ctx)
	})
	return resultErr
}

// ErrTranslatorEnablePersistedRuntimeFailed is returned by TranslatorEnable
// when the configuration was persisted to disk successfully but the
// runtime reconcile (subprocess spawn, client init) failed. The operator
// must know the difference between "won't enable" (caller can retry the
// same RPC) and "persisted but runtime failed" (a streamd restart will
// retry the runtime side; the config is committed). Callers can match
// via errors.Is.
//
// Wraps the underlying reconcile error so the cause stays inspectable.
type ErrTranslatorEnablePersistedRuntimeFailed struct {
	Cause error
}

func (e *ErrTranslatorEnablePersistedRuntimeFailed) Error() string {
	return fmt.Sprintf("config persisted but runtime reconcile failed (will retry on streamd restart): %v", e.Cause)
}

func (e *ErrTranslatorEnablePersistedRuntimeFailed) Unwrap() error {
	return e.Cause
}

// TranslatorEnable persists targetLanguage in config (so a streamd restart
// resumes the enabled state) THEN reconciles runtime. The two phases have
// distinct disposition:
//
//   - persist failure: nothing happened, plain error.
//   - persist OK, reconcile failure: state is committed to disk but
//     runtime did not start. Returns ErrTranslatorEnablePersistedRuntimeFailed
//     so operators see the partial-success disposition explicitly. The next
//     streamd restart picks up the persisted config and retries the
//     runtime side; nothing further is required from the operator.
//
// Without this distinction the CLI just printed the reconcile error red
// and operators could not tell which retry strategy applied.
func (d *StreamD) TranslatorEnable(
	ctx context.Context,
	targetLanguage string,
) (_err error) {
	logger.Tracef(ctx, "TranslatorEnable(%q)", targetLanguage)
	defer func() { logger.Tracef(ctx, "/TranslatorEnable(%q): %v", targetLanguage, _err) }()

	if targetLanguage == "" {
		return errors.New("targetLanguage must not be empty (use TranslatorDisable to turn translation off)")
	}
	if err := d.mutateTranslationConfig(ctx, func(tc *config.TranslationConfig) {
		tc.TargetLanguage = targetLanguage
	}); err != nil {
		return fmt.Errorf("persist enable: %w", err)
	}
	if err := d.reconcileTranslator(ctx, d.runCtx); err != nil {
		return &ErrTranslatorEnablePersistedRuntimeFailed{Cause: err}
	}
	return nil
}

// TranslatorDisable stops the subprocess THEN clears targetLanguage. If the
// persist fails after stop, a streamd restart will pick up the
// still-enabled on-disk config and re-spawn — operators see the disable
// did not take effect rather than a half-state.
//
// disableInProgress is set BEFORE stopTranslatorSubprocess so any
// translateOneJob in flight that observes a nil client / RPC error during
// the teardown window resolves to DispositionAbandonedOnDisable rather than
// DispositionAbandonedOnSubprocessDeath. Cleared on the way out so a later
// genuine subprocess crash classifies correctly.
func (d *StreamD) TranslatorDisable(
	ctx context.Context,
) (_err error) {
	logger.Tracef(ctx, "TranslatorDisable")
	defer func() { logger.Tracef(ctx, "/TranslatorDisable: %v", _err) }()

	d.disableInProgress.Store(true)
	defer d.disableInProgress.Store(false)

	if err := d.stopTranslatorSubprocess(ctx); err != nil {
		logger.Warnf(ctx, "stop translator subprocess: %v", err)
	}
	return d.mutateTranslationConfig(ctx, func(tc *config.TranslationConfig) {
		tc.TargetLanguage = ""
	})
}

// TranslatorRestart kills the current subprocess and spawns a fresh one,
// preserving config. Errors when no subprocess is running (callers should
// use TranslatorEnable in that case).
func (d *StreamD) TranslatorRestart(
	ctx context.Context,
) (_err error) {
	logger.Tracef(ctx, "TranslatorRestart")
	defer func() { logger.Tracef(ctx, "/TranslatorRestart: %v", _err) }()

	d.translatorLocker.Lock()
	sp := d.translatorSubprocess
	d.translatorLocker.Unlock()
	if sp == nil {
		return errors.New("translator subprocess not registered (use enable)")
	}
	// Tell the watchdog we're handling the replace ourselves so the death
	// it sees from cancelFunc does NOT trigger its own re-spawn.
	sp.manualReplace.Store(true)
	if sp.cancelFunc != nil {
		sp.cancelFunc()
	}
	if err := d.StartTranslatorSubprocess(ctx, d.runCtx); err != nil {
		return fmt.Errorf("restart spawn: %w", err)
	}
	return d.initTranslatorClient(ctx)
}

// TranslatorClearHistory forwards the request to the running subprocess.
// Splits the failure modes the same way TranslatorReload does so an
// operator running `translator history clear` gets a precise message:
//   - no subprocess registered (translation disabled) → "not registered"
//   - subprocess registered but client not yet built → "not initialised"
func (d *StreamD) TranslatorClearHistory(
	ctx context.Context,
) (_dropped int32, _err error) {
	logger.Tracef(ctx, "TranslatorClearHistory")
	defer func() { logger.Tracef(ctx, "/TranslatorClearHistory: dropped=%d err=%v", _dropped, _err) }()

	d.translatorLocker.Lock()
	sp := d.translatorSubprocess
	d.translatorLocker.Unlock()
	if sp == nil {
		return 0, errors.New("translator subprocess not registered")
	}
	client := sp.client
	if client == nil {
		return 0, errors.New("translator subprocess client not initialised")
	}
	rctx, cancel := context.WithTimeout(ctx, translatorMutationTimeout)
	defer cancel()
	reply, err := client.ClearHistory(rctx, &translator_grpc.ClearHistoryRequest{})
	if err != nil {
		return 0, fmt.Errorf("ClearHistory RPC: %w", err)
	}
	return reply.GetDroppedEntries(), nil
}

// TranslatorQueueList snapshots the streamd-side translation-queue index.
// Returns a freshly allocated slice so the caller can range over it
// without exposing translationQueueIndex's backing array (which the worker
// keeps mutating). Empty result when translation is disabled or the queue
// is currently empty — neither is treated as an error.
func (d *StreamD) TranslatorQueueList(
	ctx context.Context,
) (_ret []api.TranslationQueueEntry, _err error) {
	logger.Tracef(ctx, "TranslatorQueueList")
	defer func() { logger.Tracef(ctx, "/TranslatorQueueList: n=%d err=%v", len(_ret), _err) }()

	d.translationQueueLocker.Lock()
	defer d.translationQueueLocker.Unlock()

	out := make([]api.TranslationQueueEntry, 0, len(d.translationQueueIndex))
	for _, job := range d.translationQueueIndex {
		ev := job.msg.Event
		var user, message string
		if ev.User.Name != "" {
			user = ev.User.Name
		} else {
			user = string(ev.User.ID)
		}
		if ev.Message != nil {
			message = ev.Message.Content
		}
		out = append(out, api.TranslationQueueEntry{
			ID:         job.id.String(),
			Platform:   string(job.platID),
			User:       user,
			Message:    message,
			EnqueuedAt: job.enqueuedAt,
		})
	}
	return out, nil
}

// TranslatorQueueFlush drains every job sitting in d.translationJobs and
// clears the matching index. Returns the number of jobs dropped. Held
// under translationQueueLocker so a concurrent enqueueTranslation observes
// either a fully-flushed queue or its own append, never an interleaved
// half-state.
//
// Each drained job is Resolve'd to DispositionFlushed so the single-
// disposition partition stays balanced (every accepted job ends in exactly
// one bucket).
func (d *StreamD) TranslatorQueueFlush(
	ctx context.Context,
) (_dropped int32, _err error) {
	logger.Tracef(ctx, "TranslatorQueueFlush")
	defer func() { logger.Tracef(ctx, "/TranslatorQueueFlush: dropped=%d err=%v", _dropped, _err) }()

	d.translationQueueLocker.Lock()
	defer d.translationQueueLocker.Unlock()

	dropped := int32(0)
	for {
		select {
		case job := <-d.translationJobs:
			if job != nil {
				job.Resolve(ctx, DispositionFlushed)
			}
			dropped++
		default:
			d.translationQueueIndex = nil
			return dropped, nil
		}
	}
}

// TranslatorTranslate forwards a single ad-hoc chat message to the running
// subprocess and returns the result, the subprocess's Outcome (rendered as
// the proto enum name), the subprocess-reported latency, and any RPC error.
// Splits the failure modes the same way TranslatorClearHistory does so an
// operator running `translator translate` gets a precise message:
//   - no subprocess registered (translation disabled) → "not registered"
//   - subprocess registered but client not yet built → "not initialised"
//
// Wrapped with translatorPerCallTimeout (the LLM-call budget) rather than
// translatorStatsTimeout because Translate goes through the chain and may
// take seconds; reusing the 5s stats budget would mask healthy slow paths.
//
// Drops backend timings on the floor — operator tooling that wants the
// per-stage breakdown should call TranslatorTranslateWithTimings instead.
// Kept legacy-shaped so api.StreamD's signature does not churn.
func (d *StreamD) TranslatorTranslate(
	ctx context.Context,
	user string,
	message string,
) (_result string, _outcome string, _latency time.Duration, _err error) {
	result, outcome, latency, _, err := d.TranslatorTranslateWithTimings(ctx, user, message)
	return result, outcome, latency, err
}

// TranslatorTranslateWithTimings is the full-fat counterpart to
// TranslatorTranslate: surfaces the optional per-call backend timings the
// subprocess attached to the reply (Ollama populates it; OpenAI/Anthropic/
// ClaudeCode return nil) so streamcli can render a secondary indented row
// with the model-load / prompt-eval / generation breakdown. Returns nil
// timings when the subprocess did not attach any.
func (d *StreamD) TranslatorTranslateWithTimings(
	ctx context.Context,
	user string,
	message string,
) (_result string, _outcome string, _latency time.Duration, _timings *translator_grpc.TranslateTimings, _err error) {
	logger.Tracef(ctx, "TranslatorTranslateWithTimings(%q, %q)", user, message)
	defer func() {
		logger.Tracef(ctx, "/TranslatorTranslateWithTimings(%q, %q): outcome=%s latency=%s err=%v",
			user, message, _outcome, _latency, _err)
	}()

	d.translatorLocker.Lock()
	sp := d.translatorSubprocess
	d.translatorLocker.Unlock()
	if sp == nil {
		return "", "", 0, nil, errors.New("translator subprocess not registered")
	}
	client := sp.client
	if client == nil {
		return "", "", 0, nil, errors.New("translator subprocess client not initialised")
	}
	rctx, cancel := context.WithTimeout(ctx, translatorPerCallTimeout)
	defer cancel()
	reply, err := client.Translate(rctx, &translator_grpc.TranslateRequest{
		User:    user,
		Message: message,
	})
	if err != nil {
		return "", "", 0, nil, fmt.Errorf("Translate RPC: %w", err)
	}
	// In-band Error per the service-level rule on Translator.proto: any
	// non-empty Error means the call did not produce a usable result.
	// Forward partial telemetry (outcome, latency, timings) so the caller
	// can still log timing for the failed attempt.
	if msg := reply.GetError(); msg != "" {
		return "",
			reply.GetOutcome().String(),
			time.Duration(reply.GetLatencyNanos()),
			reply.GetTimings(),
			errors.New(msg)
	}
	return reply.GetResult(),
		reply.GetOutcome().String(),
		time.Duration(reply.GetLatencyNanos()),
		reply.GetTimings(),
		nil
}

// TranslatorTranslateViaProvider forwards an ad-hoc Translate to a single
// named provider on the active chain, bypassing the detector + spelling-
// fallback pipeline. Used by replay tooling that needs to compare a single
// provider's behaviour. Mirrors TranslatorTranslate's failure-mode split so
// the operator can distinguish "no subprocess" from "no such provider".
//
// Wrapped with translatorPerCallTimeout (the LLM-call budget) for the same
// reason as TranslatorTranslate: a healthy slow provider would be clipped by
// the 5s stats budget.
//
// Drops backend timings on the floor — operator tooling that wants the
// per-stage breakdown should call TranslatorTranslateViaProviderWithTimings
// instead. Kept legacy-shaped so api.StreamD's signature does not churn.
func (d *StreamD) TranslatorTranslateViaProvider(
	ctx context.Context,
	user string,
	message string,
	providerName string,
) (_result string, _latency time.Duration, _err error) {
	result, latency, _, err := d.TranslatorTranslateViaProviderWithTimings(ctx, user, message, providerName)
	return result, latency, err
}

// TranslatorTranslateViaProviderWithTimings is the full-fat counterpart to
// TranslatorTranslateViaProvider: surfaces the optional per-call backend
// timings the subprocess attached to the reply. Returns nil timings when
// the named provider does not populate any (e.g. OpenAI/Anthropic).
func (d *StreamD) TranslatorTranslateViaProviderWithTimings(
	ctx context.Context,
	user string,
	message string,
	providerName string,
) (_result string, _latency time.Duration, _timings *translator_grpc.TranslateTimings, _err error) {
	logger.Tracef(ctx, "TranslatorTranslateViaProviderWithTimings(%q, %q, %q)", user, message, providerName)
	defer func() {
		logger.Tracef(ctx, "/TranslatorTranslateViaProviderWithTimings(%q, %q, %q): latency=%s err=%v",
			user, message, providerName, _latency, _err)
	}()

	d.translatorLocker.Lock()
	sp := d.translatorSubprocess
	d.translatorLocker.Unlock()
	if sp == nil {
		return "", 0, nil, errors.New("translator subprocess not registered")
	}
	client := sp.client
	if client == nil {
		return "", 0, nil, errors.New("translator subprocess client not initialised")
	}
	rctx, cancel := context.WithTimeout(ctx, translatorPerCallTimeout)
	defer cancel()
	reply, err := client.TranslateViaProvider(rctx, &translator_grpc.TranslateViaProviderRequest{
		User:         user,
		Message:      message,
		ProviderName: providerName,
	})
	if err != nil {
		return "", 0, nil, fmt.Errorf("TranslateViaProvider RPC: %w", err)
	}
	if msg := reply.GetError(); msg != "" {
		return "", time.Duration(reply.GetLatencyNanos()), reply.GetTimings(), errors.New(msg)
	}
	return reply.GetResult(),
		time.Duration(reply.GetLatencyNanos()),
		reply.GetTimings(),
		nil
}

// TranslatorCompileTranslate forwards a CompileTranslate RPC to the running
// subprocess and returns the rendered system + user prompts the chain WOULD
// emit for a Translate call, plus the chain's history snapshot (rendered
// under historyMu at RPC arrival). No provider is invoked. Mirrors
// TranslatorTranslate's failure-mode split so the operator gets a precise
// "no subprocess" / "client not initialised" / in-band error message.
//
// Wrapped with translatorMutationTimeout because the call is purely
// in-process formatting on the subprocess side (no LLM round-trip); the
// short budget bounds latency without clipping any healthy path.
func (d *StreamD) TranslatorCompileTranslate(
	ctx context.Context,
	user string,
	message string,
) (_systemPrompt, _userPrompt, _targetLang, _historySnapshot string, _err error) {
	logger.Tracef(ctx, "TranslatorCompileTranslate(%q, %q)", user, message)
	defer func() {
		logger.Tracef(ctx, "/TranslatorCompileTranslate(%q, %q): err=%v", user, message, _err)
	}()

	d.translatorLocker.Lock()
	sp := d.translatorSubprocess
	d.translatorLocker.Unlock()
	if sp == nil {
		return "", "", "", "", errors.New("translator subprocess not registered")
	}
	client := sp.client
	if client == nil {
		return "", "", "", "", errors.New("translator subprocess client not initialised")
	}
	rctx, cancel := context.WithTimeout(ctx, translatorMutationTimeout)
	defer cancel()
	reply, err := client.CompileTranslate(rctx, &translator_grpc.CompileTranslateRequest{
		User:    user,
		Message: message,
	})
	if err != nil {
		return "", "", "", "", fmt.Errorf("CompileTranslate RPC: %w", err)
	}
	if msg := reply.GetError(); msg != "" {
		return "", "", "", "", errors.New(msg)
	}
	return reply.GetSystemPrompt(),
		reply.GetUserPrompt(),
		reply.GetTargetLang(),
		reply.GetHistorySnapshot(),
		nil
}

// TranslatorCompileLanguageDetect forwards a CompileLanguageDetect RPC to
// the running subprocess and returns the rendered system + user prompts the
// chain WOULD emit for the language-detection step, plus the chain's
// history snapshot (rendered under historyMu at RPC arrival). No provider
// is invoked. Mirrors TranslatorTranslate's failure-mode split.
//
// Wrapped with translatorMutationTimeout for the same reason as
// TranslatorCompileTranslate.
func (d *StreamD) TranslatorCompileLanguageDetect(
	ctx context.Context,
	message string,
) (_systemPrompt, _userPrompt, _targetLang, _historySnapshot string, _err error) {
	logger.Tracef(ctx, "TranslatorCompileLanguageDetect(%q)", message)
	defer func() {
		logger.Tracef(ctx, "/TranslatorCompileLanguageDetect(%q): err=%v", message, _err)
	}()

	d.translatorLocker.Lock()
	sp := d.translatorSubprocess
	d.translatorLocker.Unlock()
	if sp == nil {
		return "", "", "", "", errors.New("translator subprocess not registered")
	}
	client := sp.client
	if client == nil {
		return "", "", "", "", errors.New("translator subprocess client not initialised")
	}
	rctx, cancel := context.WithTimeout(ctx, translatorMutationTimeout)
	defer cancel()
	reply, err := client.CompileLanguageDetect(rctx, &translator_grpc.CompileLanguageDetectRequest{
		Message: message,
	})
	if err != nil {
		return "", "", "", "", fmt.Errorf("CompileLanguageDetect RPC: %w", err)
	}
	if msg := reply.GetError(); msg != "" {
		return "", "", "", "", errors.New(msg)
	}
	return reply.GetSystemPrompt(),
		reply.GetUserPrompt(),
		reply.GetTargetLang(),
		reply.GetHistorySnapshot(),
		nil
}
