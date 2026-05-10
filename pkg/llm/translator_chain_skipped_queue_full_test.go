package llm

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	tassert "github.com/stretchr/testify/assert"
	trequire "github.com/stretchr/testify/require"
)

// fakeProvider is a deterministic Provider for unit-testing the chain's
// outcome accounting. Each call invokes onCall (if set) with the call
// index — the first call is the detector, subsequent calls are translate
// attempts. Returning "" + nil from the translate path is interpreted by
// the chain as a successful identity translation, so onCall must drive
// either an error or a usable detect/translate response.
type fakeProvider struct {
	name   string
	calls  atomic.Int32
	onCall func(callIdx int32, systemPrompt, userPrompt string) (string, error)
}

func (f *fakeProvider) Name() string { return f.name }

func (f *fakeProvider) Translate(
	ctx context.Context,
	systemPrompt string,
	userPrompt string,
) (string, error) {
	result, _, err := f.TranslateWithMetadata(ctx, systemPrompt, userPrompt)
	return result, err
}

func (f *fakeProvider) TranslateWithMetadata(
	_ context.Context,
	systemPrompt string,
	userPrompt string,
) (string, *TranslateMetadata, error) {
	idx := f.calls.Add(1)
	if f.onCall == nil {
		return "", nil, nil
	}
	result, err := f.onCall(idx, systemPrompt, userPrompt)
	return result, nil, err
}

// TestTranslateWithOutcome_AllSkippedReportsQueueFull locks in the fix for
// the design-smell finding HIGH-1: when every provider in the chain is
// skipped before any actual Provider.Translate happens (queue full on
// non-last + circuit open on last), the chain MUST report
// OutcomeSkippedQueueFull and increment TotalSkippedQueueFull, NOT the
// previous default of OutcomeAllProvidersFailed (which was logged with
// "<nil>" for lastErr because nothing actually failed).
//
// Falsification: revert the fix in TranslateWithOutcome (drop the
// anyProviderActuallyFailed branch and unconditionally return
// OutcomeAllProvidersFailed) and this test asserts on
// TotalSkippedQueueFull == 1 / TotalAllProvidersFailed == 0 / outcome ==
// OutcomeSkippedQueueFull, all of which break.
func TestTranslateWithOutcome_AllSkippedReportsQueueFull(t *testing.T) {
	// Detector lives on the LAST provider (callFirstAvailableProvider),
	// so it must succeed before the main loop runs. We use the same fake
	// for P2 — its first call serves as the detector reply, returning a
	// non-target signal so the chain proceeds to the translate loop.
	detectResp := "IS_TARGET: NO\nLANGUAGES: tr:1.0\n"

	p1 := &fakeProvider{
		name: "p1",
		onCall: func(int32, string, string) (string, error) {
			t.Fatal("p1 must not be invoked: queue-full skip should fire before Translate")
			return "", nil
		},
	}
	p2 := &fakeProvider{
		name: "p2",
		onCall: func(idx int32, _, _ string) (string, error) {
			if idx == 1 {
				// First call = detector probe via callFirstAvailableProvider.
				return detectResp, nil
			}
			t.Fatal("p2 translate path must not be invoked: circuit-open should fire before Translate")
			return "", nil
		},
	}

	tc := NewTranslatorChain("English", 0, []ProviderEntry{
		{Provider: p1, Parallelism: 1, MaxQueueSize: 0},
		{Provider: p2, Parallelism: 1, MaxQueueSize: 0,
			CircuitBreakerThreshold: 1, CircuitBreakerCooldown: time.Hour},
	})

	// Saturate P1's queue-full check: maxWaiters = MaxQueueSize +
	// cap(Semaphore) = 0 + 1 = 1. Pre-bump Queued to 1 so the in-flight
	// Translate's Queued.Add(1) crosses the threshold and the !isLast
	// branch returns (false, nil) without ever hitting Provider.Translate.
	tc.Providers[0].Queued.Add(1)
	// Fill P1's semaphore so the detector's callFirstAvailableProvider
	// skips P1 and falls through to P2 — without this the detector would
	// pick P1 first and onCall's t.Fatal would trip.
	tc.Providers[0].Semaphore <- struct{}{}

	// Trip P2's circuit so its iteration `continue`s before
	// acquireSemaphore. Threshold is 1, so a single ConsecutiveFails
	// reaching it within the cooldown window opens the circuit.
	tc.Providers[1].ConsecutiveFails.Store(1)
	tc.Providers[1].LastFailTime.Store(time.Now().UnixNano())

	_, outcome, err := tc.TranslateWithOutcome(context.Background(), "alice", "merhaba arkadaşım")
	trequire.NoError(t, err, "skip-only path must not surface a transport error")

	tassert.Equal(t, OutcomeSkippedQueueFull, outcome,
		"every provider was skipped before Translate; outcome MUST be OutcomeSkippedQueueFull (not OutcomeAllProvidersFailed)")

	stats := tc.SnapshotStats()
	tassert.Equal(t, int64(1), stats.TotalSkippedQueueFull,
		"TotalSkippedQueueFull must increment exactly once for the skip-only run")
	tassert.Equal(t, int64(0), stats.TotalAllProvidersFailed,
		"TotalAllProvidersFailed must NOT increment when no provider actually failed (this is the metric-lie the fix closes)")
}
