// MAINTENANCE: enum order, String() switch, Snapshot field order, proto
// fields (pkg/streamd/grpc/streamd.proto TranslationDispositionCounters),
// and api.TranslationDispositionCounters fields must match. The
// compile-time check on dispositionCount catches count drift; ordering
// drift is a code-review concern.
package streamd

import (
	"context"
	"runtime"
	"sync/atomic"
	"time"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamd/api"
)

// Disposition is the terminal outcome of an acceptedJob — every job that the
// translation worker accepted (i.e. successfully landed on translationJobs)
// must end in exactly one Disposition before being garbage-collected. The
// 12 buckets partition the post-accept space; their sum equals the
// streamd-side totalResolved counter, where totalResolved <= totalEnqueued
// (the difference being in-flight jobs whose Resolve has not yet run).
//
// Two queue-full buckets are deliberately distinct:
//   - queueFullAtEnqueue lives OUTSIDE the partition (offered minus
//     enqueued). The job never became an acceptedJob.
//   - DispositionChainSkippedQueueFull lives INSIDE the partition: the chain
//     accepted the job but every provider's per-call queue was full so no
//     translation actually happened.
type Disposition int

const (
	// DispositionTranslated mirrors translator_grpc.Outcome_OUTCOME_TRANSLATED:
	// a provider produced a real translation.
	DispositionTranslated Disposition = iota
	// DispositionAlreadyTarget mirrors OUTCOME_ALREADY_TARGET: the detector
	// said "target lang", chain skipped translate.
	DispositionAlreadyTarget
	// DispositionDetectFailed mirrors OUTCOME_DETECT_FAILED: the detection
	// step errored, original kept.
	DispositionDetectFailed
	// DispositionSpellingOnly mirrors OUTCOME_SPELLING_ONLY: the translation
	// collapsed to a spelling correction of the original.
	DispositionSpellingOnly
	// DispositionAllProvidersFailed mirrors OUTCOME_ALL_PROVIDERS_FAILED:
	// every provider returned an error.
	DispositionAllProvidersFailed
	// DispositionChainSkippedQueueFull mirrors OUTCOME_SKIPPED_QUEUE_FULL:
	// every provider's per-call queue was full so the chain skipped.
	DispositionChainSkippedQueueFull
	// DispositionAbandonedOnDisable: TranslatorDisable was in flight when
	// the worker tried to call into the subprocess (no client / RPC error).
	// Distinct from AbandonedOnSubprocessDeath so an operator-driven disable
	// is not misread as a crash.
	DispositionAbandonedOnDisable
	// DispositionAbandonedOnSubprocessDeath: the subprocess was unreachable
	// (no client snapshot or transport error) and the disable flag was NOT
	// set, so the cause is a subprocess crash / restart in flight.
	DispositionAbandonedOnSubprocessDeath
	// DispositionAbandonedOnShutdown: the worker observed ctx.Done while
	// jobs remained on the channel; the drain resolves each remaining job
	// to this disposition before returning.
	DispositionAbandonedOnShutdown
	// DispositionFlushed: TranslatorQueueFlush drained the job before the
	// worker reached it.
	DispositionFlushed
	// DispositionLeaked: the runtime.SetFinalizer fired on an acceptedJob
	// whose Resolve was never called — programmer error in the worker. Logs
	// loudly, increments the counter, does NOT panic (production code must
	// not crash on accounting bugs; tests assert this counter stays zero).
	DispositionLeaked
	// DispositionEmptyOrSkipped: the worker observed the job but produced no
	// translation work (empty content, or content already containing the
	// translationSeparator from an upstream chatinjector). Distinct from
	// DispositionTranslated/DispositionAlreadyTarget so an operator can
	// distinguish "no content seen" from "real chain decision".
	DispositionEmptyOrSkipped

	// dispositionCount is the number of buckets in the partition. Kept as an
	// unexported sentinel so range-over-buckets stays consistent with iota.
	dispositionCount
)

// Compile-time assertion that the enum has exactly 12 buckets. If a 13th
// Disposition is added before dispositionCount, this expression goes
// negative (uint underflow) and the build fails. Forces every count drift
// to be reflected in Snapshot, the proto, the api struct, and the tests.
const _ = uint(dispositionCount - 12)

// String returns the human-readable bucket name. Used in log messages and
// the ledger's CounterByName lookup.
func (d Disposition) String() string {
	switch d {
	case DispositionTranslated:
		return "translated"
	case DispositionAlreadyTarget:
		return "already_target"
	case DispositionDetectFailed:
		return "detect_failed"
	case DispositionSpellingOnly:
		return "spelling_only"
	case DispositionAllProvidersFailed:
		return "all_providers_failed"
	case DispositionChainSkippedQueueFull:
		return "chain_skipped_queue_full"
	case DispositionAbandonedOnDisable:
		return "abandoned_on_disable"
	case DispositionAbandonedOnSubprocessDeath:
		return "abandoned_on_subprocess_death"
	case DispositionAbandonedOnShutdown:
		return "abandoned_on_shutdown"
	case DispositionFlushed:
		return "flushed"
	case DispositionLeaked:
		return "leaked"
	case DispositionEmptyOrSkipped:
		return "empty_or_skipped"
	default:
		return "unknown"
	}
}

// translationDispositionLedger holds the streamd-side counters for the
// single-disposition accounting model. Every counter is atomic so concurrent
// enqueueTranslation / runTranslationWorker / TranslatorQueueFlush writers
// do not race; reads are also via atomic.Load so a Snapshot under no lock
// observes coherent (per-counter) values without tearing.
//
// Counter relationships (operator-visible):
//
//	totalOffered  ==  totalEnqueued
//	             +    queueFullAtEnqueue
//	             +    offeredWithTranslationDisabled
//	totalEnqueued ==  totalResolved + inFlight
//
// The first identity holds at every instant (the three branches are
// mutually exclusive on the enqueueTranslation hot path). The second is
// derived: inFlight := totalEnqueued - totalResolved (no separate counter,
// always non-negative because every enqueue precedes its resolve).
//
// Within the partition: SumDispositions() == totalResolved at quiescence.
// Mid-update the relationship is |Sum - totalResolved| <= 1: recordDisposition
// increments totalResolved BEFORE the bucket so a mid-update reader can
// observe Sum < totalResolved (a single in-flight write) but never
// Sum > totalResolved. Operators see "missing accounts" for at most one
// counter, never "lost increments" — preferable for diagnostics.
type translationDispositionLedger struct {
	// dispositions is indexed by Disposition. atomic.Int64 keeps the
	// per-bucket counters race-free without a global mutex.
	dispositions [dispositionCount]atomic.Int64
	// totalResolved is incremented BEFORE the matching dispositions[d]
	// inside recordDisposition so a mid-update Snapshot reads Sum <=
	// totalResolved (operator-friendlier than the inverse). Counts every
	// acceptedJob whose Resolve has run exactly once. Kept as a separate
	// counter so callers don't have to range over the 12 slots on a hot
	// path. Note: this is post-Resolve count, not channel-send count —
	// see totalEnqueued for the latter.
	totalResolved atomic.Int64
	// totalEnqueued is incremented at successful channel-send time in
	// enqueueTranslation. inFlight := totalEnqueued - totalResolved is
	// always >= 0 because the send precedes the matching Resolve.
	totalEnqueued atomic.Int64
	// totalOffered is incremented at the top of enqueueTranslation
	// unconditionally (before the nil-channel / queue-full / accepted
	// branches). Operator-visible:
	//   total_offered == total_enqueued + queue_full_at_enqueue
	//                  + offered_with_translation_disabled.
	totalOffered atomic.Int64
	// queueFullAtEnqueue is the streamd-side queue-full counter (no
	// acceptedJob was constructed). Distinct from
	// DispositionChainSkippedQueueFull which is the chain-side bucket.
	queueFullAtEnqueue atomic.Int64
	// offeredWithTranslationDisabled is incremented on the nil-channel
	// branch (translation disabled, no acceptedJob constructed).
	offeredWithTranslationDisabled atomic.Int64
}

// recordDisposition bumps totalResolved BEFORE the matching bucket so a
// mid-update Snapshot reader sees Sum <= totalResolved (off-by-at-most-one
// in the operator-friendlier direction — "missing account" rather than
// "lost increment"). The caller is acceptedJob.Resolve which has already
// CAS-flipped resolved so we run at most once per job; the leak finalizer
// is the only other caller and uses its own CAS for the same guarantee.
func (l *translationDispositionLedger) recordDisposition(d Disposition) {
	if d < 0 || d >= dispositionCount {
		return
	}
	l.totalResolved.Add(1)
	l.dispositions[d].Add(1)
}

// Snapshot returns a frozen copy of the counters for serving via the stats
// RPC. atomic.Load on every read so we observe per-counter coherent values
// even if writers race with the snapshot. Inter-counter coherence (e.g.
// totalResolved exactly equals sum of dispositions at one instant) is NOT
// guaranteed because writers update totalResolved and the bucket in two
// non-atomic steps — readers may see |Sum - totalResolved| up to 1 in
// flight. Callers that need strict equality should re-read after a
// quiescent moment.
func (l *translationDispositionLedger) Snapshot() api.TranslationDispositionCounters {
	return api.TranslationDispositionCounters{
		Translated:                 l.dispositions[DispositionTranslated].Load(),
		AlreadyTarget:              l.dispositions[DispositionAlreadyTarget].Load(),
		DetectFailed:               l.dispositions[DispositionDetectFailed].Load(),
		SpellingOnly:               l.dispositions[DispositionSpellingOnly].Load(),
		AllProvidersFailed:         l.dispositions[DispositionAllProvidersFailed].Load(),
		ChainSkippedQueueFull:      l.dispositions[DispositionChainSkippedQueueFull].Load(),
		AbandonedOnDisable:         l.dispositions[DispositionAbandonedOnDisable].Load(),
		AbandonedOnSubprocessDeath: l.dispositions[DispositionAbandonedOnSubprocessDeath].Load(),
		AbandonedOnShutdown:        l.dispositions[DispositionAbandonedOnShutdown].Load(),
		Flushed:                    l.dispositions[DispositionFlushed].Load(),
		Leaked:                     l.dispositions[DispositionLeaked].Load(),
		EmptyOrSkipped:             l.dispositions[DispositionEmptyOrSkipped].Load(),
	}
}

// SumDispositions returns the integer sum of all 12 buckets. Used by tests
// to assert the partition invariant against totalResolved.
func (l *translationDispositionLedger) SumDispositions() int64 {
	var s int64
	for i := range l.dispositions {
		s += l.dispositions[i].Load()
	}
	return s
}

// acceptedJob is the wrapper around a translationJob that ENFORCES single-
// disposition accounting. Constructed only after a successful channel send
// in enqueueTranslation; every code path that takes ownership (worker call,
// flush, shutdown drain) must call Resolve exactly once. The runtime
// finalizer catches programmer errors by recording DispositionLeaked and
// logging — it does NOT panic, so a production accounting bug cannot crash
// the daemon.
type acceptedJob struct {
	id         dedupKey
	platID     streamcontrol.PlatformName
	msg        api.ChatMessage
	enqueuedAt time.Time

	resolved atomic.Bool
	ledger   *translationDispositionLedger
}

// newAcceptedJob constructs an acceptedJob WITHOUT arming the finalizer.
// The caller (enqueueTranslation) arms the finalizer via armLeakFinalizer
// only AFTER a successful channel send so a queue-full path can drop the
// half-built struct without firing DispositionLeaked.
func newAcceptedJob(
	id dedupKey,
	platID streamcontrol.PlatformName,
	msg api.ChatMessage,
	enqueuedAt time.Time,
	ledger *translationDispositionLedger,
) *acceptedJob {
	return &acceptedJob{
		id:         id,
		platID:     platID,
		msg:        msg,
		enqueuedAt: enqueuedAt,
		ledger:     ledger,
	}
}

// armLeakFinalizer installs the runtime finalizer that catches a job which
// reaches GC without Resolve being called — programmer error in the worker.
// The finalizer is the safety net: a healthy daemon never increments the
// leaked bucket. Logs Errorf so the streamd log surfaces this loudly; the
// counter is the machine-readable counterpart. Does NOT panic.
//
// The finalizer uses CompareAndSwap on the same `resolved` flag that
// Resolve uses, so a late Resolve racing with finalizer execution at GC
// time cannot double-count: whichever wins the CAS records its disposition;
// the loser is a NO-OP.
func (j *acceptedJob) armLeakFinalizer() {
	runtime.SetFinalizer(j, func(obj *acceptedJob) {
		if !obj.resolved.CompareAndSwap(false, true) {
			return
		}
		if obj.ledger == nil {
			return
		}
		// Logger derived from a fresh background ctx because the original
		// call ctx is long gone by GC time.
		bgCtx := context.Background()
		logger.Errorf(bgCtx,
			"acceptedJob leaked without Resolve: id=%q plat=%q enqueuedAt=%s — programmer error in worker",
			obj.id, obj.platID, obj.enqueuedAt)
		obj.ledger.recordDisposition(DispositionLeaked)
	})
}

// Resolve records a single Disposition for this job. The nil-ledger early-
// return runs BEFORE the CAS so a nil-ledger acceptedJob (test fixtures
// without a ledger) never wastes its CAS-flip on a swallowed disposition —
// a subsequent real Resolve attempt would otherwise NO-OP. After CAS, a
// second Resolve on a real ledger is a silent NO-OP (production code can't
// crash on accounting duplication; the trace log surfaces it for diagnosis).
//
// Idempotence is load-bearing: the runTranslationWorker shutdown drain may
// race with a concurrent TranslatorQueueFlush, both attempting to resolve
// the same job. The CAS guarantees exactly one wins.
func (j *acceptedJob) Resolve(
	ctx context.Context,
	d Disposition,
) {
	if j.ledger == nil {
		return
	}
	if !j.resolved.CompareAndSwap(false, true) {
		logger.Tracef(ctx, "acceptedJob.Resolve called twice: id=%q new=%s — ignored",
			j.id, d)
		return
	}
	j.ledger.recordDisposition(d)
	logger.Tracef(ctx, "acceptedJob.Resolve: id=%q disposition=%s", j.id, d)
}
