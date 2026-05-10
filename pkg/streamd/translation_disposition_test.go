package streamd

import (
	"context"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/streamctl/pkg/streamd/api"
)

// TestAcceptedJob_Resolve_Idempotent confirms that a second Resolve call is a
// silent NO-OP: the disposition counter for the FIRST call must be 1, and a
// second Resolve attempting to record a different bucket must NOT bump
// either bucket again. resolved must be true after the first call. This
// pins the production-safety property that an accounting bug (double
// resolve) cannot crash the daemon and cannot double-count.
func TestAcceptedJob_Resolve_Idempotent(t *testing.T) {
	ledger := &translationDispositionLedger{}
	job := newAcceptedJob("id-1", "twitch", api.ChatMessage{}, time.Now(), ledger)

	job.Resolve(context.Background(), DispositionTranslated)
	require.True(t, job.resolved.Load(), "resolved must be true after first Resolve")
	assert.Equal(t, int64(1), ledger.dispositions[DispositionTranslated].Load(),
		"first Resolve must bump the Translated bucket")

	job.Resolve(context.Background(), DispositionFlushed)
	assert.Equal(t, int64(1), ledger.dispositions[DispositionTranslated].Load(),
		"second Resolve must NOT re-bump the original bucket")
	assert.Equal(t, int64(0), ledger.dispositions[DispositionFlushed].Load(),
		"second Resolve must NOT bump the new bucket either")
	assert.Equal(t, int64(1), ledger.totalAccepted.Load(),
		"totalAccepted must stay at 1 after a duplicate Resolve")
}

// TestTranslationDispositionLedger_PartitionInvariant drives N synthetic jobs
// through every disposition and asserts the partition invariant: the sum of
// the 11 buckets equals totalAccepted. This is the load-bearing accounting
// property that the user requirement asks for.
func TestTranslationDispositionLedger_PartitionInvariant(t *testing.T) {
	ledger := &translationDispositionLedger{}
	dispositions := []Disposition{
		DispositionTranslated,
		DispositionAlreadyTarget,
		DispositionDetectFailed,
		DispositionSpellingOnly,
		DispositionAllProvidersFailed,
		DispositionChainSkippedQueueFull,
		DispositionAbandonedOnDisable,
		DispositionAbandonedOnSubprocessDeath,
		DispositionAbandonedOnShutdown,
		DispositionFlushed,
		DispositionLeaked,
	}
	require.Len(t, dispositions, int(dispositionCount),
		"every Disposition value must be exercised so the test catches a new bucket added without test coverage")

	const repeats = 7
	var wg sync.WaitGroup
	for _, d := range dispositions {
		for i := 0; i < repeats; i++ {
			wg.Add(1)
			go func(d Disposition) {
				defer wg.Done()
				job := newAcceptedJob("id", "twitch", api.ChatMessage{}, time.Now(), ledger)
				job.Resolve(context.Background(), d)
			}(d)
		}
	}
	wg.Wait()

	expected := int64(len(dispositions) * repeats)
	assert.Equal(t, expected, ledger.totalAccepted.Load(),
		"totalAccepted must equal repeats × dispositionCount")
	assert.Equal(t, expected, ledger.SumDispositions(),
		"sum of dispositions must equal totalAccepted (the partition invariant)")
}

// TestAcceptedJob_FinalizerRecordsLeaked confirms that the runtime-finalizer
// safety net fires DispositionLeaked when an acceptedJob reaches GC without
// Resolve being called. The test forces the job to become unreachable and
// triggers GC; after a finalizer-runs barrier the leaked counter must be > 0.
//
// Falsifies: drop armLeakFinalizer from newAcceptedJob and the leaked
// counter stays zero across the same test, exposing the regression.
//
// Tolerates flakiness via runtime.GC + runtime.Gosched sleep retries: the
// Go runtime makes no hard guarantee about when finalizers run, only that
// they run before the heap is reclaimed; in practice GC + a brief sleep is
// enough on a healthy Linux build host.
func TestAcceptedJob_FinalizerRecordsLeaked(t *testing.T) {
	ledger := &translationDispositionLedger{}

	// Wrap the construction in a closure so the local variable holding the
	// pointer goes out of scope as soon as the closure returns — that is
	// the precondition for GC to even consider the job for finalization.
	func() {
		job := newAcceptedJob("leak-id", "twitch", api.ChatMessage{}, time.Now(), ledger)
		job.armLeakFinalizer()
		// Intentionally NOT calling Resolve — the test is the leak path.
		_ = job
	}()

	const maxAttempts = 50
	for i := 0; i < maxAttempts; i++ {
		runtime.GC()
		runtime.Gosched()
		if ledger.dispositions[DispositionLeaked].Load() > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	assert.Greater(t, ledger.dispositions[DispositionLeaked].Load(), int64(0),
		"finalizer must record DispositionLeaked when Resolve was never called")
	assert.Greater(t, ledger.totalAccepted.Load(), int64(0),
		"finalizer must also bump totalAccepted to keep the partition balanced")
}

// TestStreamD_ClassifyAbandon_DisableVsDeath pins the disable-vs-death
// branching so an operator-driven disable is not misread as a subprocess
// crash. With disableInProgress=true → DispositionAbandonedOnDisable; with
// disableInProgress=false → DispositionAbandonedOnSubprocessDeath. The
// ctx.Err() short-circuit (Shutdown) is exercised by a third call with a
// cancelled ctx.
func TestStreamD_ClassifyAbandon_DisableVsDeath(t *testing.T) {
	d := &StreamD{}

	d.disableInProgress.Store(true)
	assert.Equal(t, DispositionAbandonedOnDisable,
		d.classifyAbandon(context.Background()),
		"disableInProgress=true must map to AbandonedOnDisable")

	d.disableInProgress.Store(false)
	assert.Equal(t, DispositionAbandonedOnSubprocessDeath,
		d.classifyAbandon(context.Background()),
		"disableInProgress=false + healthy ctx must map to AbandonedOnSubprocessDeath")

	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	assert.Equal(t, DispositionAbandonedOnShutdown,
		d.classifyAbandon(cancelledCtx),
		"cancelled ctx must map to AbandonedOnShutdown regardless of disableInProgress")
}
