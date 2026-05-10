package server

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/observability"
	llms "github.com/xaionaro-go/streamctl/pkg/llm"
	translator_grpc "github.com/xaionaro-go/streamctl/pkg/translator/grpc/go/translator_grpc"
)

// stubProvider is a deterministic Provider used to drive Translate without
// touching the network. It returns a fixed result and lets the test assert
// the exact text passed back through the chain.
type stubProvider struct {
	name   string
	result string
}

func (s *stubProvider) Name() string { return s.name }

func (s *stubProvider) Translate(
	ctx context.Context,
	systemPrompt string,
	userPrompt string,
) (string, error) {
	result, _, err := s.TranslateWithMetadata(ctx, systemPrompt, userPrompt)
	return result, err
}

func (s *stubProvider) TranslateWithMetadata(
	ctx context.Context,
	systemPrompt string,
	userPrompt string,
) (string, *llms.TranslateMetadata, error) {
	return s.result, nil, nil
}

// detectThenTranslateProvider returns "IS_TARGET: NO\nLANGUAGES: tr:0.95"
// the first time it is called (the detect step in TranslatorChain.Translate),
// then returns translateResult on subsequent calls. This lets a unit test
// drive the chain end-to-end with predictable outputs.
type detectThenTranslateProvider struct {
	name            string
	translateResult string
	calls           atomic.Int32
}

func (p *detectThenTranslateProvider) Name() string { return p.name }

func (p *detectThenTranslateProvider) Translate(
	ctx context.Context,
	systemPrompt string,
	userPrompt string,
) (string, error) {
	result, _, err := p.TranslateWithMetadata(ctx, systemPrompt, userPrompt)
	return result, err
}

func (p *detectThenTranslateProvider) TranslateWithMetadata(
	ctx context.Context,
	systemPrompt string,
	userPrompt string,
) (string, *llms.TranslateMetadata, error) {
	n := p.calls.Add(1)
	if n == 1 {
		return "IS_TARGET: NO\nLANGUAGES: tr:0.95", nil, nil
	}
	return p.translateResult, nil, nil
}

func newChainWithStub(translateOutput string) *llms.TranslatorChain {
	return llms.NewTranslatorChain("English", 5, []llms.ProviderEntry{{
		Provider:    &detectThenTranslateProvider{name: "stub", translateResult: translateOutput},
		Parallelism: 1,
		Timeout:     5 * time.Second,
	}})
}

func TestServer_Translate_PassesThrough(t *testing.T) {
	srv := New()
	srv.SwapChain(context.Background(), newChainWithStub("hello"))

	reply, err := srv.Translate(context.Background(), &translator_grpc.TranslateRequest{
		User:    "tester",
		Message: "merhaba",
	})
	require.NoError(t, err)
	assert.Equal(t, "hello", reply.GetResult())
	assert.Equal(t, translator_grpc.Outcome_OUTCOME_TRANSLATED, reply.GetOutcome())
	assert.Greater(t, reply.GetLatencyNanos(), int64(0))
}

func TestServer_Reconfigure_RetainsOldChainOnError(t *testing.T) {
	srv := New()
	original := newChainWithStub("hello")
	srv.SwapChain(context.Background(), original)

	// Garbage YAML must not swap the chain.
	reply, err := srv.Reconfigure(context.Background(), &translator_grpc.ReconfigureRequest{
		ConfigYaml: []byte("not: yaml: this is broken"),
	})
	require.NoError(t, err)
	assert.False(t, reply.GetApplied(), "Reconfigure must not apply on parse error")
	assert.NotEmpty(t, reply.GetError(), "Reconfigure must surface the parse error")

	// The old chain must still answer Translate after the failure.
	tr, err := srv.Translate(context.Background(), &translator_grpc.TranslateRequest{
		User:    "tester",
		Message: "merhaba",
	})
	require.NoError(t, err)
	assert.Equal(t, "hello", tr.GetResult())
}

// gateProvider is a deterministic two-step provider used by
// TestServer_TranslateContinuesDuringSwap. The detect step (call #1) returns
// immediately; the translate step (call #2) blocks on `gate` until the test
// releases it, then returns identity (a per-chain tag). This lets the test
// hold N translations mid-flight on chain A, swap to chain B, release, and
// assert that every released call still returned A's tag.
type gateProvider struct {
	identity string
	gate     chan struct{}
	// midFlight is signalled (one send per call) every time a translate
	// step (NOT detect) reaches the gate. The test reads it inFlight times
	// to prove every goroutine has loaded its chain pointer and entered
	// the translate step BEFORE the chain swap happens.
	midFlight chan struct{}
}

func (p *gateProvider) Name() string { return "gate-" + p.identity }

func (p *gateProvider) Translate(
	ctx context.Context,
	systemPrompt string,
	userPrompt string,
) (string, error) {
	result, _, err := p.TranslateWithMetadata(ctx, systemPrompt, userPrompt)
	return result, err
}

func (p *gateProvider) TranslateWithMetadata(
	ctx context.Context,
	systemPrompt string,
	userPrompt string,
) (string, *llms.TranslateMetadata, error) {
	// Detect step: TranslatorChain runs Provider.Translate twice (classify,
	// then translate). Return the canned detect shape so the chain
	// proceeds to the translate step.
	if strings.HasPrefix(systemPrompt, "Classify this chat message") {
		return "IS_TARGET: NO\nLANGUAGES: tr:0.95", nil, nil
	}
	// Translate step: signal "in flight on this chain", then block on the
	// gate so the test can hold us mid-flight; close(gate) releases all
	// blocked callers simultaneously.
	if p.midFlight != nil {
		// Non-blocking send: capacity matches inFlight, so a successful
		// send means we've reached the gate. Tests size the channel so
		// this never drops a signal.
		p.midFlight <- struct{}{}
	}
	select {
	case <-p.gate:
		return p.identity, nil, nil
	case <-ctx.Done():
		return "", nil, ctx.Err()
	}
}

func newChainWithGate(
	identity string,
	midFlight chan struct{},
) (*llms.TranslatorChain, chan struct{}) {
	gate := make(chan struct{})
	chain := llms.NewTranslatorChain("English", 5, []llms.ProviderEntry{{
		Provider:    &gateProvider{identity: identity, gate: gate, midFlight: midFlight},
		Parallelism: 16, // enough slots for all in-flight callers
		Timeout:     5 * time.Second,
	}})
	return chain, gate
}

// TestServer_TranslateContinuesDuringSwap proves that Reconfigure/SwapChain
// can replace the active chain while in-flight Translate calls are still
// running on the previous chain — and that those in-flight calls observe
// the chain they captured at entry, not whatever the current chain is at
// completion. The atomic.Pointer field on Server is what makes this safe.
//
// Falsification: if Server.chain is downgraded from atomic.Pointer to a
// plain pointer field, this test fails under -race because Translate's
// Load() races the SwapChain Store(). The dual-side assertions (no nil
// reply, identity matches loaded chain) catch tearing even if -race isn't
// enabled.
// TestServer_TranslateContinuesDuringSwap proves identity preservation:
// a Translate call that has already entered chain A continues to drain on
// chain A even after SwapChain installs chain B. The reply identifier must
// match the chain loaded at entry.
//
// This is the deterministic side of the atomic.Pointer contract — pair it
// with TestServer_ChainSwapHasNoDataRace for the concurrency side.
func TestServer_TranslateContinuesDuringSwap(t *testing.T) {
	const inFlight = 4
	srv := New()

	midFlightA := make(chan struct{}, inFlight*2)
	midFlightB := make(chan struct{}, inFlight*2)
	chainA, gateA := newChainWithGate("A", midFlightA)
	chainB, gateB := newChainWithGate("B", midFlightB)
	srv.SwapChain(context.Background(), chainA)

	results := make([]string, inFlight)
	errs := make([]error, inFlight)
	var wg sync.WaitGroup
	wg.Add(inFlight)

	for i := 0; i < inFlight; i++ {
		i := i
		observability.Go(context.Background(), func(ctx context.Context) {
			defer wg.Done()
			reply, err := srv.Translate(context.Background(), &translator_grpc.TranslateRequest{
				User:    "racer",
				Message: "merhaba",
			})
			if err != nil {
				errs[i] = err
				return
			}
			require.NotNil(t, reply, "Translate must never return nil reply on success")
			results[i] = reply.GetResult()
		})
	}

	// Drain inFlight midFlight signals from chain A — each proves one
	// goroutine has Load()-ed chain A and dispatched into its provider.
	// Without this barrier the swap could race ahead and the goroutines
	// would Load chain B instead, making the test prove nothing.
	for i := 0; i < inFlight; i++ {
		select {
		case <-midFlightA:
		case <-time.After(2 * time.Second):
			t.Fatalf("only %d/%d goroutines reached chain A's gate within 2s", i, inFlight)
		}
	}

	// Swap to chain B; in-flight calls must still complete on chain A.
	srv.SwapChain(context.Background(), chainB)

	// Release chain A's gate so the four in-flight calls drain.
	close(gateA)

	wg.Wait()

	// Every in-flight call must have completed with chain A's identity.
	// Dual-sided: confirms A IS observed and B is NOT.
	for i := 0; i < inFlight; i++ {
		require.NoError(t, errs[i], "in-flight Translate %d must not error", i)
		assert.Equal(t, "A", results[i], "in-flight Translate %d must complete on chain A", i)
		assert.NotEqual(t, "B", results[i], "in-flight Translate %d must NOT see chain B mid-flight", i)
	}

	// Sanity check: a fresh Translate after the swap uses chain B.
	postSwapDone := make(chan struct{})
	var postSwapResult string
	var postSwapErr error
	observability.Go(context.Background(), func(ctx context.Context) {
		defer close(postSwapDone)
		reply, err := srv.Translate(context.Background(), &translator_grpc.TranslateRequest{
			User:    "racer",
			Message: "merhaba",
		})
		if err != nil {
			postSwapErr = err
			return
		}
		postSwapResult = reply.GetResult()
	})
	select {
	case <-midFlightB:
	case <-time.After(2 * time.Second):
		t.Fatal("post-swap Translate did not reach chain B's gate within 2s")
	}
	close(gateB)
	<-postSwapDone

	require.NoError(t, postSwapErr)
	assert.Equal(t, "B", postSwapResult, "post-swap Translate must use chain B")
}

// instantProvider implements llms.Provider with no blocking. Used by the
// data-race test where we want translate calls to complete fast so the
// goroutines loop frequently against the swapper.
type instantProvider struct {
	identity string
}

func (p *instantProvider) Name() string { return "instant-" + p.identity }

func (p *instantProvider) Translate(
	ctx context.Context,
	systemPrompt string,
	userPrompt string,
) (string, error) {
	result, _, err := p.TranslateWithMetadata(ctx, systemPrompt, userPrompt)
	return result, err
}

func (p *instantProvider) TranslateWithMetadata(
	ctx context.Context,
	systemPrompt string,
	userPrompt string,
) (string, *llms.TranslateMetadata, error) {
	if strings.HasPrefix(systemPrompt, "Classify this chat message") {
		return "IS_TARGET: NO\nLANGUAGES: tr:0.95", nil, nil
	}
	return p.identity, nil, nil
}

func newInstantChain(identity string) *llms.TranslatorChain {
	return llms.NewTranslatorChain("English", 5, []llms.ProviderEntry{{
		Provider:    &instantProvider{identity: identity},
		Parallelism: 16,
		Timeout:     5 * time.Second,
	}})
}

// TestServer_ChainSwapHasNoDataRace concurrently calls Translate (which
// reads Server.chain) and SwapChain (which writes Server.chain). Without
// atomic.Pointer this is a textbook data race that -race flags as DATA
// RACE; with atomic.Pointer Load/Store, no race is reported.
//
// Falsification: replace atomic.Pointer with a plain pointer field. Under
// `go test -race` this test fails with a DATA RACE report on s.chain.
//
// We also assert dual-sided that every reply.GetResult() is one of the
// known chain identities — never empty, never some torn value — so the
// test still catches semantic regressions even when -race is off.
func TestServer_ChainSwapHasNoDataRace(t *testing.T) {
	const swapperRounds = 200
	const readers = 4
	srv := New()

	chains := []*llms.TranslatorChain{
		newInstantChain("c0"),
		newInstantChain("c1"),
		newInstantChain("c2"),
		newInstantChain("c3"),
	}
	srv.SwapChain(context.Background(), chains[0])

	validIdentities := map[string]bool{"c0": true, "c1": true, "c2": true, "c3": true}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	type result struct {
		identity string
		err      error
	}
	resultsCh := make(chan result, readers*swapperRounds*4)

	var wg sync.WaitGroup
	wg.Add(readers)
	for i := 0; i < readers; i++ {
		observability.Go(ctx, func(ctx context.Context) {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				default:
				}
				reply, err := srv.Translate(context.Background(), &translator_grpc.TranslateRequest{
					User:    "racer",
					Message: "merhaba",
				})
				if err != nil {
					resultsCh <- result{err: err}
					continue
				}
				resultsCh <- result{identity: reply.GetResult()}
			}
		})
	}

	// Swapper: spam SwapChain. Reads from the readers and this writer
	// touch the same Server.chain field; under a non-atomic field this is
	// the race -race catches.
	swapperDone := make(chan struct{})
	observability.Go(ctx, func(ctx context.Context) {
		defer close(swapperDone)
		for i := 0; i < swapperRounds; i++ {
			srv.SwapChain(context.Background(), chains[i%len(chains)])
		}
	})

	<-swapperDone
	cancel()
	wg.Wait()
	close(resultsCh)

	// Validate every reply identity. None may be empty (torn read) and
	// none may be unrecognised (a torn pointer would dereference an
	// arbitrary chain — caught here even without -race).
	var ok, errCount int
	for r := range resultsCh {
		if r.err != nil {
			errCount++
			continue
		}
		require.NotEmpty(t, r.identity, "reply identity must never be empty under hot swap")
		require.Truef(t, validIdentities[r.identity],
			"reply identity %q must match one of the installed chains", r.identity)
		ok++
	}
	require.Greater(t, ok, 0, "at least one Translate must have completed")
	require.Zero(t, errCount, "no Translate call must error during a chain swap")
}

func TestServer_Ping(t *testing.T) {
	srv := New()
	_, err := srv.Ping(context.Background(), &translator_grpc.PingRequest{})
	require.NoError(t, err)
}

// TestServer_Translate_NoChainConfigured pins the in-band error contract:
// when no chain is loaded the handler returns (reply, nil) with the
// ErrNoChain message on Reply.Error. Returning a gRPC error here would
// violate the service-level rule documented on Translator.proto.
func TestServer_Translate_NoChainConfigured(t *testing.T) {
	srv := New()
	reply, err := srv.Translate(context.Background(), &translator_grpc.TranslateRequest{
		User:    "tester",
		Message: "merhaba",
	})
	require.NoError(t, err, "Translate must surface no-chain via in-band Error, never gRPC error")
	require.NotNil(t, reply, "Translate must return a non-nil reply with the in-band error")
	assert.Equal(t, ErrNoChain.Error(), reply.GetError(),
		"Reply.Error must carry the ErrNoChain message verbatim so callers can match by string")
}

// TestServer_TranslateViaProvider_NoChainConfigured mirrors
// TestServer_Translate_NoChainConfigured for the provider-bypass RPC: the
// no-chain disposition flows through the same in-band channel as
// "provider not found" so callers do not have to special-case the two.
func TestServer_TranslateViaProvider_NoChainConfigured(t *testing.T) {
	srv := New()
	reply, err := srv.TranslateViaProvider(context.Background(), &translator_grpc.TranslateViaProviderRequest{
		User:         "tester",
		Message:      "merhaba",
		ProviderName: "anything",
	})
	require.NoError(t, err, "TranslateViaProvider must surface no-chain via in-band Error, never gRPC error")
	require.NotNil(t, reply, "TranslateViaProvider must return a non-nil reply with the in-band error")
	assert.Equal(t, ErrNoChain.Error(), reply.GetError())
}

// TestServer_ClearHistory_emptiesHistory drives the production ClearHistory
// handler against a chain that has logged two Translate calls and confirms
// the dropped count matches what addToHistory recorded plus that the chain's
// history slice is empty after the RPC.
func TestServer_ClearHistory_emptiesHistory(t *testing.T) {
	srv := New()
	chain := newChainWithStub("hello")
	srv.SwapChain(context.Background(), chain)

	// Two translations populate two entries (callable goes through the
	// detect-then-translate stub which always lands in addToHistory).
	for i := 0; i < 2; i++ {
		_, err := srv.Translate(context.Background(), &translator_grpc.TranslateRequest{
			User:    "tester",
			Message: "merhaba",
		})
		require.NoError(t, err)
	}

	reply, err := srv.ClearHistory(context.Background(), &translator_grpc.ClearHistoryRequest{})
	require.NoError(t, err)
	assert.Equal(t, int32(2), reply.GetDroppedEntries(),
		"two translations must have populated two history entries")

	// A second clear must report zero — proving the slice is actually empty.
	reply, err = srv.ClearHistory(context.Background(), &translator_grpc.ClearHistoryRequest{})
	require.NoError(t, err)
	assert.Equal(t, int32(0), reply.GetDroppedEntries(),
		"a second clear after an empty clear must report zero")
}

// TestServer_ClearHistory_noChain covers the bootstrap case: a freshly
// constructed Server with no Reconfigure has no chain, so ClearHistory must
// reply with DroppedEntries=0 and no error rather than panicking on the nil
// chain pointer.
func TestServer_ClearHistory_noChain(t *testing.T) {
	srv := New()
	reply, err := srv.ClearHistory(context.Background(), &translator_grpc.ClearHistoryRequest{})
	require.NoError(t, err)
	assert.Equal(t, int32(0), reply.GetDroppedEntries(),
		"no chain → no dropped entries, no error")
}

// cancelAndFailProvider returns a canned detect result on the first call and
// then cancels the supplied context + returns an error on the second call.
// Pairing it with a second chain provider whose Semaphore is pre-filled
// drives TranslateWithOutcome down the semaphore-acquire-under-cancel branch
// (acquireSemaphore observing ctx.Done while the semaphore is full).
type cancelAndFailProvider struct {
	name      string
	cancelCtx context.CancelFunc
}

func (p *cancelAndFailProvider) Name() string { return p.name }

func (p *cancelAndFailProvider) Translate(
	ctx context.Context,
	systemPrompt string,
	userPrompt string,
) (string, error) {
	result, _, err := p.TranslateWithMetadata(ctx, systemPrompt, userPrompt)
	return result, err
}

func (p *cancelAndFailProvider) TranslateWithMetadata(
	ctx context.Context,
	systemPrompt string,
	userPrompt string,
) (string, *llms.TranslateMetadata, error) {
	if strings.HasPrefix(systemPrompt, "Classify this chat message") {
		return "IS_TARGET: NO\nLANGUAGES: tr:0.95", nil, nil
	}
	if p.cancelCtx != nil {
		p.cancelCtx()
	}
	return "", nil, errors.New("first-provider translate failure")
}

// errorProvider always returns an error from Translate. Used by
// TestServer_TranslateViaProvider_ProviderError to drive the per-provider
// error branch in Server.TranslateViaProvider.
type errorProvider struct {
	name string
}

func (p *errorProvider) Name() string { return p.name }

func (p *errorProvider) Translate(
	ctx context.Context,
	systemPrompt string,
	userPrompt string,
) (string, error) {
	result, _, err := p.TranslateWithMetadata(ctx, systemPrompt, userPrompt)
	return result, err
}

func (p *errorProvider) TranslateWithMetadata(
	ctx context.Context,
	systemPrompt string,
	userPrompt string,
) (string, *llms.TranslateMetadata, error) {
	return "", nil, errors.New("provider boom")
}

// TestServer_Translate_ChainTranslateError pins the in-band error contract for
// the path where chain.TranslateWithOutcome returns a non-nil error: the
// reply must carry the wrapped message, gRPC err must be nil, and the
// latency counter must be populated so callers can record timing for the
// failed attempt.
//
// This test exercises the TranslatorChain.acquireSemaphore-under-cancelled-ctx
// branch specifically — the only path inside TranslateWithOutcome that
// returns a non-nil error today. The two-provider setup
// (cancelAndFailProvider first; pre-filled-Semaphore second) and the
// pre-fill of Providers[1].Semaphore depend on TranslatorChain internals;
// if those internals change, this test must be updated to drive the new
// error-returning path rather than silently going vacuous.
func TestServer_Translate_ChainTranslateError(t *testing.T) {
	srv := New()
	ctx, cancel := context.WithCancel(context.Background())
	first := &cancelAndFailProvider{name: "first", cancelCtx: cancel}
	second := &stubProvider{name: "second", result: "unused"}
	chain := llms.NewTranslatorChain("English", 5, []llms.ProviderEntry{
		{Provider: first, Parallelism: 1, Timeout: 5 * time.Second},
		{Provider: second, Parallelism: 1, Timeout: 5 * time.Second},
	})
	chain.Providers[1].Semaphore <- struct{}{}
	srv.SwapChain(context.Background(), chain)

	reply, err := srv.Translate(ctx, &translator_grpc.TranslateRequest{User: "tester", Message: "merhaba"})
	require.NoError(t, err, "Translate must surface chain errors via in-band Error, never gRPC error")
	require.NotNil(t, reply)
	assert.NotEmpty(t, reply.GetError(), "Reply.Error must carry the wrapped chain error")
	assert.Greater(t, reply.GetLatencyNanos(), int64(0), "Reply.LatencyNanos must be set even on failure")
}

// TestServer_TranslateViaProvider_NotFound pins the error message format for
// the "provider name not in chain" disposition: the reply must name the
// requested provider AND the available list so an operator typo is visible.
func TestServer_TranslateViaProvider_NotFound(t *testing.T) {
	srv := New()
	chain := llms.NewTranslatorChain("English", 5, []llms.ProviderEntry{{
		Provider:    &stubProvider{name: "real-one", result: "hi"},
		Parallelism: 1,
		Timeout:     5 * time.Second,
	}})
	srv.SwapChain(context.Background(), chain)

	reply, err := srv.TranslateViaProvider(context.Background(), &translator_grpc.TranslateViaProviderRequest{
		User: "tester", Message: "merhaba", ProviderName: "missing",
	})
	require.NoError(t, err)
	require.NotNil(t, reply)
	assert.Contains(t, reply.GetError(), `"missing"`, "error must quote the unknown provider name")
	assert.Contains(t, reply.GetError(), "not found in chain", "error must say not found")
	assert.Contains(t, reply.GetError(), "real-one", "error must list available provider names")
}

// TestServer_TranslateViaProvider_ProviderError pins the in-band error
// contract for the path where the named provider's Translate returns an
// error: the reply must carry the provider's error string and the latency
// counter must be populated so the caller can record timing for the failed
// attempt.
func TestServer_TranslateViaProvider_ProviderError(t *testing.T) {
	srv := New()
	chain := llms.NewTranslatorChain("English", 5, []llms.ProviderEntry{{
		Provider:    &errorProvider{name: "boom"},
		Parallelism: 1,
		Timeout:     5 * time.Second,
	}})
	srv.SwapChain(context.Background(), chain)

	reply, err := srv.TranslateViaProvider(context.Background(), &translator_grpc.TranslateViaProviderRequest{
		User: "tester", Message: "merhaba", ProviderName: "boom",
	})
	require.NoError(t, err)
	require.NotNil(t, reply)
	assert.NotEmpty(t, reply.GetError(), "Reply.Error must carry the provider's error message")
	assert.Greater(t, reply.GetLatencyNanos(), int64(0), "Reply.LatencyNanos must be set even on failure")
}

// TestServer_Reconfigure_BuildChainError covers a config that parses cleanly
// but fails BuildChain (unknown provider type). The handler must reply
// Applied=false with the build error in Error so the streamd-side supervisor
// surfaces the message without tearing the subprocess down.
func TestServer_Reconfigure_BuildChainError(t *testing.T) {
	srv := New()

	yamlCfg := []byte("target_language: English\nproviders:\n  - type: nonsense\n")
	reply, err := srv.Reconfigure(context.Background(), &translator_grpc.ReconfigureRequest{ConfigYaml: yamlCfg})
	require.NoError(t, err)
	require.NotNil(t, reply)
	assert.False(t, reply.GetApplied(), "Reconfigure must not apply on build error")
	assert.Contains(t, reply.GetError(), "build chain", "error must be tagged as a build failure")
	assert.Contains(t, reply.GetError(), "nonsense", "error must surface the underlying cause")
}

// timingsProvider is a deterministic Provider whose TranslateWithMetadata
// returns a fixed *TranslateMetadata. Used to drive the timings-flow tests
// without touching the network. The first call returns the canned detect
// shape (no metadata, mirroring real provider behaviour where the detect
// step's metadata is intentionally dropped); subsequent calls return the
// stored metadata.
type timingsProvider struct {
	name string
	meta *llms.TranslateMetadata
}

func (p *timingsProvider) Name() string { return p.name }

func (p *timingsProvider) Translate(
	ctx context.Context,
	systemPrompt string,
	userPrompt string,
) (string, error) {
	result, _, err := p.TranslateWithMetadata(ctx, systemPrompt, userPrompt)
	return result, err
}

func (p *timingsProvider) TranslateWithMetadata(
	ctx context.Context,
	systemPrompt string,
	userPrompt string,
) (string, *llms.TranslateMetadata, error) {
	if strings.HasPrefix(systemPrompt, "Classify this chat message") {
		return "IS_TARGET: NO\nLANGUAGES: tr:0.95", nil, nil
	}
	return "hello", p.meta, nil
}

// TestServer_Translate_TimingsPropagated pins the timings flow: when the
// provider populates *TranslateMetadata, Server.Translate's reply MUST
// carry a non-nil Timings sub-message with every field round-tripped
// verbatim. Falsification: if Server.Translate stops populating Timings
// (e.g. drops the meta from TranslateWithOutcomeAndMetadata), this test
// fails on require.NotNil. If a field is dropped, the matching field
// assertion fails.
func TestServer_Translate_TimingsPropagated(t *testing.T) {
	srv := New()
	wantMeta := &llms.TranslateMetadata{
		TotalDurationNanos:      1_500_000_000,
		LoadDurationNanos:       100_000_000,
		PromptEvalDurationNanos: 200_000_000,
		EvalDurationNanos:       1_200_000_000,
		PromptEvalTokens:        42,
		EvalTokens:              123,
	}
	chain := llms.NewTranslatorChain("English", 5, []llms.ProviderEntry{{
		Provider:    &timingsProvider{name: "stub", meta: wantMeta},
		Parallelism: 1,
		Timeout:     5 * time.Second,
	}})
	srv.SwapChain(context.Background(), chain)

	reply, err := srv.Translate(context.Background(), &translator_grpc.TranslateRequest{
		User: "tester", Message: "merhaba",
	})
	require.NoError(t, err)
	require.NotNil(t, reply)
	require.Empty(t, reply.GetError())

	timings := reply.GetTimings()
	require.NotNil(t, timings, "reply must carry timings when the provider populates them")
	assert.Equal(t, wantMeta.TotalDurationNanos, timings.GetTotalDurationNanos())
	assert.Equal(t, wantMeta.LoadDurationNanos, timings.GetLoadDurationNanos())
	assert.Equal(t, wantMeta.PromptEvalDurationNanos, timings.GetPromptEvalDurationNanos())
	assert.Equal(t, wantMeta.EvalDurationNanos, timings.GetEvalDurationNanos())
	assert.Equal(t, wantMeta.PromptEvalTokens, timings.GetPromptEvalTokens())
	assert.Equal(t, wantMeta.EvalTokens, timings.GetEvalTokens())
}

// TestServer_Translate_TimingsAbsentWhenProviderReturnsNil locks the
// "absent timings" half of the contract (dual-sided): when the provider
// returns nil metadata, the reply's Timings field MUST stay nil so
// streamcli can omit the secondary indented row entirely. Falsification:
// if mapTimings starts fabricating an empty TranslateTimings on nil meta,
// this test fails on require.Nil.
func TestServer_Translate_TimingsAbsentWhenProviderReturnsNil(t *testing.T) {
	srv := New()
	chain := llms.NewTranslatorChain("English", 5, []llms.ProviderEntry{{
		Provider:    &timingsProvider{name: "stub", meta: nil},
		Parallelism: 1,
		Timeout:     5 * time.Second,
	}})
	srv.SwapChain(context.Background(), chain)

	reply, err := srv.Translate(context.Background(), &translator_grpc.TranslateRequest{
		User: "tester", Message: "merhaba",
	})
	require.NoError(t, err)
	require.NotNil(t, reply)
	require.Empty(t, reply.GetError())

	require.Nil(t, reply.GetTimings(),
		"reply.Timings must remain nil when the provider returns nil metadata "+
			"so streamcli omits the secondary indented row")
}

// TestServer_TranslateViaProvider_TimingsPropagated mirrors the timings
// flow on the provider-bypass RPC: a populated *TranslateMetadata from the
// named provider MUST appear on the reply's Timings field with every
// value round-tripped verbatim.
func TestServer_TranslateViaProvider_TimingsPropagated(t *testing.T) {
	srv := New()
	wantMeta := &llms.TranslateMetadata{
		TotalDurationNanos:      9_000_000_000,
		LoadDurationNanos:       0,
		PromptEvalDurationNanos: 800_000_000,
		EvalDurationNanos:       8_000_000_000,
		PromptEvalTokens:        77,
		EvalTokens:              456,
	}
	chain := llms.NewTranslatorChain("English", 5, []llms.ProviderEntry{{
		Provider:    &timingsProvider{name: "stub", meta: wantMeta},
		Parallelism: 1,
		Timeout:     5 * time.Second,
	}})
	srv.SwapChain(context.Background(), chain)

	reply, err := srv.TranslateViaProvider(context.Background(), &translator_grpc.TranslateViaProviderRequest{
		User: "tester", Message: "merhaba", ProviderName: "stub",
	})
	require.NoError(t, err)
	require.NotNil(t, reply)
	require.Empty(t, reply.GetError())

	timings := reply.GetTimings()
	require.NotNil(t, timings)
	assert.Equal(t, wantMeta.TotalDurationNanos, timings.GetTotalDurationNanos())
	assert.Equal(t, wantMeta.PromptEvalDurationNanos, timings.GetPromptEvalDurationNanos())
	assert.Equal(t, wantMeta.EvalDurationNanos, timings.GetEvalDurationNanos())
	assert.Equal(t, wantMeta.PromptEvalTokens, timings.GetPromptEvalTokens())
	assert.Equal(t, wantMeta.EvalTokens, timings.GetEvalTokens())
}

// ensure stubProvider satisfies the Provider contract at compile time.
var _ llms.Provider = (*stubProvider)(nil)
var _ llms.Provider = (*detectThenTranslateProvider)(nil)
var _ llms.Provider = (*gateProvider)(nil)
var _ llms.Provider = (*cancelAndFailProvider)(nil)
var _ llms.Provider = (*errorProvider)(nil)
var _ llms.Provider = (*timingsProvider)(nil)
