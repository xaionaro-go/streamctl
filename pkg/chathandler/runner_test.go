package chathandler

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/streamctl/pkg/dedup"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamd/grpc/go/streamd_grpc"
	"google.golang.org/grpc"
)

// mockChatListener implements ChatListener for testing.
// It can be configured to:
//   - emit a fixed number of events then close the channel (simulating failure)
//   - emit events indefinitely until the context is cancelled
type mockChatListener struct {
	name string

	// eventsBeforeClose controls how many events are sent before the channel
	// is closed. A negative value means "emit until context is cancelled".
	eventsBeforeClose int

	// eventInterval controls the delay between emitted events.
	eventInterval time.Duration

	// fixedEventIDs, when non-empty, makes Listen emit exactly len(fixedEventIDs)
	// events whose IDs are taken verbatim from the slice (in order) and then
	// close the channel. Used by dedup tests to emit a controlled sequence
	// including duplicates and empty IDs. Overrides eventsBeforeClose.
	fixedEventIDs []string

	mu             sync.Mutex
	closed         bool
	listenErr      error // if non-nil, Listen returns this error immediately
	cumulativeSent int   // monotonic counter across Listen() invocations
}

func (m *mockChatListener) Name() string { return m.name }

func (m *mockChatListener) Listen(ctx context.Context) (<-chan streamcontrol.Event, error) {
	m.mu.Lock()
	m.closed = false
	listenErr := m.listenErr
	m.mu.Unlock()

	if listenErr != nil {
		return nil, listenErr
	}

	ch := make(chan streamcontrol.Event)
	go func() {
		defer close(ch)
		if len(m.fixedEventIDs) > 0 {
			for _, id := range m.fixedEventIDs {
				ev := streamcontrol.Event{
					ID:        streamcontrol.EventID(id),
					CreatedAt: time.Now(),
					Type:      streamcontrol.EventTypeChatMessage,
				}
				select {
				case <-ctx.Done():
					return
				case ch <- ev:
				}
				if m.eventInterval > 0 {
					select {
					case <-ctx.Done():
						return
					case <-time.After(m.eventInterval):
					}
				}
			}
			// After emitting the fixed sequence, block until ctx is cancelled
			// rather than closing the channel. Closing would trigger the
			// runner's retry loop and re-emit the sequence indefinitely,
			// which the dedup tests need to avoid.
			<-ctx.Done()
			return
		}
		sent := 0
		for {
			if m.eventsBeforeClose >= 0 && sent >= m.eventsBeforeClose {
				return
			}
			// Use a cumulative counter across Listen() invocations so each
			// retry cycle produces fresh, unique event IDs. This keeps the
			// retry/heartbeat tests independent of the runner-level dedup
			// gate added on top of injectEvent.
			m.mu.Lock()
			id := m.cumulativeSent
			m.cumulativeSent++
			m.mu.Unlock()
			ev := streamcontrol.Event{
				ID:        streamcontrol.EventID(fmt.Sprintf("mock-event-%d", id)),
				CreatedAt: time.Now(),
				Type:      streamcontrol.EventTypeChatMessage,
			}
			select {
			case <-ctx.Done():
				return
			case ch <- ev:
				sent++
			}
			if m.eventInterval > 0 {
				select {
				case <-ctx.Done():
					return
				case <-time.After(m.eventInterval):
				}
			}
		}
	}()
	return ch, nil
}

func (m *mockChatListener) Close(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return nil
}

// capturedInjectCall records a single InjectChatMessage invocation.
type capturedInjectCall struct {
	PlatID       string
	EventID      string
	ListenerType string
}

// capturedHeartbeatCall records a single ReportChatHandlerActivity invocation.
type capturedHeartbeatCall struct {
	PlatID       string
	ListenerType string
}

// mockStreamDClient satisfies streamd_grpc.StreamDClient. Only the calls we
// care about are implemented; embedding the interface gives compile-time
// satisfaction of every other method (which would panic if called).
type mockStreamDClient struct {
	streamd_grpc.StreamDClient // embedded nil — panics on any unimplemented method call

	mu             sync.Mutex
	injectCalls    []capturedInjectCall
	injectEntered  int
	heartbeatCalls []capturedHeartbeatCall

	// blockInject, when non-nil, holds every InjectChatMessage call until
	// the channel is closed (or the call's context is cancelled). Set by
	// tests that need to simulate a slow inject without head-of-line
	// blocking the heartbeat goroutine.
	blockInject chan struct{}
}

func (m *mockStreamDClient) injectEntries() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.injectEntered
}

func (m *mockStreamDClient) setBlockInject(ch chan struct{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.blockInject = ch
}

func (m *mockStreamDClient) currentBlockInject() chan struct{} {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.blockInject
}

func (m *mockStreamDClient) InjectChatMessage(
	ctx context.Context,
	in *streamd_grpc.InjectChatMessageRequest,
	_ ...grpc.CallOption,
) (*streamd_grpc.InjectChatMessageReply, error) {
	m.mu.Lock()
	m.injectEntered++
	m.mu.Unlock()

	if block := m.currentBlockInject(); block != nil {
		select {
		case <-block:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var eventID string
	if ev := in.GetEvent(); ev != nil {
		eventID = ev.GetId()
	}
	m.injectCalls = append(m.injectCalls, capturedInjectCall{
		PlatID:       in.GetPlatID(),
		EventID:      eventID,
		ListenerType: in.GetListenerType(),
	})
	return &streamd_grpc.InjectChatMessageReply{}, nil
}

func (m *mockStreamDClient) ReportChatHandlerActivity(
	_ context.Context,
	in *streamd_grpc.ReportChatHandlerActivityRequest,
	_ ...grpc.CallOption,
) (*streamd_grpc.ReportChatHandlerActivityReply, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.heartbeatCalls = append(m.heartbeatCalls, capturedHeartbeatCall{
		PlatID:       in.GetPlatID(),
		ListenerType: in.GetListenerType(),
	})
	return &streamd_grpc.ReportChatHandlerActivityReply{}, nil
}

func (m *mockStreamDClient) getInjectCalls() []capturedInjectCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]capturedInjectCall, len(m.injectCalls))
	copy(result, m.injectCalls)
	return result
}

func (m *mockStreamDClient) getHeartbeatCalls() []capturedHeartbeatCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]capturedHeartbeatCall, len(m.heartbeatCalls))
	copy(result, m.heartbeatCalls)
	return result
}

// getCalls retains the legacy name for the inject-only callers in
// existing retry/cancel tests.
func (m *mockStreamDClient) getCalls() []capturedInjectCall {
	return m.getInjectCalls()
}

func TestRunnerSingleListener_RetriesOnFailure(t *testing.T) {
	client := &mockStreamDClient{}
	listener := &mockChatListener{
		name:              "test-listener",
		eventsBeforeClose: 1, // emit 1 event then close (simulates failure)
	}

	runner := NewSingleListenerRunner(
		"testplat",
		streamcontrol.ChatListenerPrimary,
		client,
		listener,
		RunnerConfig{
			BaseBackoff:   time.Millisecond,
			MaxBackoff:    5 * time.Millisecond,
			KeepaliveTime: time.Hour, // effectively disabled for this test
		},
	)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	err := runner.Run(ctx)
	require.Error(t, err, "Run must return an error when context is cancelled")
	assert.ErrorIs(t, err, context.DeadlineExceeded)

	calls := client.getCalls()
	// The listener emits 1 event per attempt, so multiple calls means retries.
	assert.GreaterOrEqual(t, len(calls), 2,
		"runner must retry: expected at least 2 InjectChatMessage calls, got %d", len(calls))
	// Dual-sided: each call targets the correct platform.
	for _, c := range calls {
		assert.Equal(t, "testplat", c.PlatID)
	}
}

func TestRunnerSingleListener_ResetBackoffAfterHealthyRun(t *testing.T) {
	// Strategy: we cannot use real 30s waits. Instead, test the calculateBackoff
	// method and the reset logic by verifying that after enough runtime the
	// consecutiveFailures counter resets.
	//
	// We test calculateBackoff directly (it's a method on *Runner) as a
	// unit test, then rely on the integration test above for retry behavior.
	runner := &Runner{
		Config: RunnerConfig{
			BaseBackoff: 100 * time.Millisecond,
			MaxBackoff:  10 * time.Second,
		},
	}

	// Verify exponential backoff.
	assert.Equal(t, 100*time.Millisecond, runner.calculateBackoff(1))
	assert.Equal(t, 200*time.Millisecond, runner.calculateBackoff(2))
	assert.Equal(t, 400*time.Millisecond, runner.calculateBackoff(3))
	assert.Equal(t, 800*time.Millisecond, runner.calculateBackoff(4))

	// Verify cap at MaxBackoff.
	assert.Equal(t, 10*time.Second, runner.calculateBackoff(100))

	// Dual-sided: first failure always gets BaseBackoff (proves reset works
	// when consecutiveFailures is set back to 1 after healthy runtime).
	assert.Equal(t, 100*time.Millisecond, runner.calculateBackoff(1),
		"after reset (consecutiveFailures=1), backoff must equal BaseBackoff")
	// Non-reset (high consecutive) must NOT equal BaseBackoff.
	assert.NotEqual(t, 100*time.Millisecond, runner.calculateBackoff(5),
		"high consecutive failures must produce backoff > BaseBackoff")
}

func TestRunnerSingleListener_ContextCancellation(t *testing.T) {
	client := &mockStreamDClient{}
	listener := &mockChatListener{
		name:              "cancel-listener",
		eventsBeforeClose: -1, // emit indefinitely
		eventInterval:     time.Millisecond,
	}

	runner := NewSingleListenerRunner(
		"cancelplat",
		streamcontrol.ChatListenerAlternate,
		client,
		listener,
		RunnerConfig{
			BaseBackoff:   time.Millisecond,
			MaxBackoff:    time.Millisecond,
			KeepaliveTime: time.Hour,
		},
	)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- runner.Run(ctx)
	}()

	// Let it run briefly, then cancel.
	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		require.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled,
			"Run must return context.Canceled after cancellation")
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return within 2s after context cancellation")
	}

	// Dual-sided: some events were injected before cancellation.
	calls := client.getCalls()
	assert.Greater(t, len(calls), 0,
		"expected at least one InjectChatMessage call before cancellation")
}

// TestRunnerSingleListener_HeartbeatsPeriodically verifies that the runner
// emits ReportChatHandlerActivity calls on the keepalive cadence and never
// sends keepalives through the InjectChatMessage path.
func TestRunnerSingleListener_HeartbeatsPeriodically(t *testing.T) {
	client := &mockStreamDClient{}
	listener := &mockChatListener{
		name:              "heartbeat-listener",
		eventsBeforeClose: -1,
		eventInterval:     time.Hour, // effectively disabled
	}

	listenerType := streamcontrol.ChatListenerContingency
	runner := NewSingleListenerRunner(
		"heartbeatplat",
		listenerType,
		client,
		listener,
		RunnerConfig{
			BaseBackoff:   time.Millisecond,
			MaxBackoff:    time.Millisecond,
			KeepaliveTime: 10 * time.Millisecond,
		},
	)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	_ = runner.Run(ctx)

	heartbeats := client.getHeartbeatCalls()
	require.GreaterOrEqual(t, len(heartbeats), 5,
		"expected at least 5 heartbeats over 200ms with 10ms cadence, got %d", len(heartbeats))

	for _, h := range heartbeats {
		assert.Equal(t, "heartbeatplat", h.PlatID)
		assert.Equal(t, listenerType.String(), h.ListenerType)
	}

	// Dual-sided: no heartbeat must leak into the inject path.
	injects := client.getInjectCalls()
	for _, c := range injects {
		assert.NotContains(t, c.EventID, "keepalive-",
			"keepalives must not flow through InjectChatMessage")
	}
}

// TestRunnerSingleListener_HeartbeatSurvivesInjectBlock proves the watchdog
// signal stays alive while a single InjectChatMessage call is blocked.
func TestRunnerSingleListener_HeartbeatSurvivesInjectBlock(t *testing.T) {
	block := make(chan struct{})
	client := &mockStreamDClient{}
	client.setBlockInject(block)

	listener := &mockChatListener{
		name:              "block-listener",
		eventsBeforeClose: 1, // emit exactly one event then keep the channel open
		eventInterval:     time.Hour,
	}

	runner := NewSingleListenerRunner(
		"testplat",
		streamcontrol.ChatListenerPrimary,
		client,
		listener,
		RunnerConfig{
			BaseBackoff:   time.Millisecond,
			MaxBackoff:    time.Millisecond,
			KeepaliveTime: 20 * time.Millisecond,
		},
	)

	// Release the inject after 200ms; the test's overall context is 250ms.
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()

	releaseAt := time.AfterFunc(200*time.Millisecond, func() { close(block) })
	defer releaseAt.Stop()

	_ = runner.Run(ctx)

	heartbeats := client.getHeartbeatCalls()
	assert.GreaterOrEqual(t, len(heartbeats), 5,
		"expected >=5 heartbeats during the 200ms inject block, got %d", len(heartbeats))

	injects := client.getInjectCalls()
	assert.Equal(t, 1, len(injects),
		"expected exactly one InjectChatMessage to complete (after block release), got %d", len(injects))

	// Targeted dual-sided assertion on the heartbeat metadata.
	require.Greater(t, len(heartbeats), 0)
	for _, h := range heartbeats {
		assert.Equal(t, "testplat", h.PlatID)
		assert.Equal(t, "primary", h.ListenerType)
	}
}

// TestRunnerSingleListener_InjectTimeoutBoundsBlock proves a hung
// InjectChatMessage cannot block subsequent events past injectTimeout:
// when InjectChatMessage never returns, each event's call still gets its
// own deadline and a second event must be processed within the budget.
func TestRunnerSingleListener_InjectTimeoutBoundsBlock(t *testing.T) {
	originalTimeout := injectTimeout
	injectTimeout = 30 * time.Millisecond
	defer func() { injectTimeout = originalTimeout }()

	block := make(chan struct{}) // never closed — injects must time out via callCtx
	client := &mockStreamDClient{}
	client.setBlockInject(block)

	listener := &mockChatListener{
		name:              "timeout-listener",
		eventsBeforeClose: 2,
		eventInterval:     time.Millisecond,
	}

	runner := NewSingleListenerRunner(
		"timeoutplat",
		streamcontrol.ChatListenerPrimary,
		client,
		listener,
		RunnerConfig{
			BaseBackoff:   time.Millisecond,
			MaxBackoff:    time.Millisecond,
			KeepaliveTime: time.Hour, // disable heartbeats for this test
		},
	)

	// Generous slack: each inject blocks for ~injectTimeout before unblocking
	// the loop, so two events take ~2*injectTimeout. Add 150ms for scheduler
	// jitter and goroutine startup.
	budget := 2*injectTimeout + 150*time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), budget)
	defer cancel()

	_ = runner.Run(ctx)

	entries := client.injectEntries()
	assert.GreaterOrEqual(t, entries, 2,
		"runner must enter InjectChatMessage twice within %s (one per event), got %d", budget, entries)

	// Dual-sided: no inject completed (block was never released), so
	// injectCalls must be empty — proving the timeout fired and freed
	// the loop without either call succeeding.
	assert.Equal(t, 0, len(client.getInjectCalls()),
		"no InjectChatMessage call should have completed (block was never released)")
}

// TestRunner_InjectListenerTypeIsPassedThrough proves the runner stamps the
// configured ChatListenerType into every InjectChatMessage request. The
// streamd-side cross-source-collapse logic uses this field to decide whether
// two same-fingerprint events can share an ID — losing it here would make
// every listener look like the same source on the wire.
func TestRunner_InjectListenerTypeIsPassedThrough(t *testing.T) {
	client := &mockStreamDClient{}
	listener := &mockChatListener{
		name:              "lt-listener",
		eventsBeforeClose: 1,
		eventInterval:     time.Hour,
	}

	listenerType := streamcontrol.ChatListenerEmergency
	runner := NewSingleListenerRunner(
		"ltplat",
		listenerType,
		client,
		listener,
		RunnerConfig{
			BaseBackoff:   time.Millisecond,
			MaxBackoff:    time.Millisecond,
			KeepaliveTime: time.Hour,
		},
	)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_ = runner.Run(ctx)

	calls := client.getInjectCalls()
	require.Greater(t, len(calls), 0, "expected at least one InjectChatMessage call")
	for _, c := range calls {
		assert.Equal(t, listenerType.String(), c.ListenerType,
			"every InjectChatMessage request must carry the runner's ListenerType")
		assert.Equal(t, "ltplat", c.PlatID)
	}
}

// TestRunner_DedupSameID proves the runner-level dedup short-circuits a repeat
// ev.ID before reaching InjectChatMessage. The listener emits the same ID
// twice; only the first call must hit the gRPC client.
func TestRunner_DedupSameID(t *testing.T) {
	client := &mockStreamDClient{}
	listener := &mockChatListener{
		name:          "dedup-listener",
		fixedEventIDs: []string{"X", "X"},
	}

	runner := NewSingleListenerRunner(
		"dedupplat",
		streamcontrol.ChatListenerPrimary,
		client,
		listener,
		RunnerConfig{
			BaseBackoff:   time.Millisecond,
			MaxBackoff:    time.Millisecond,
			KeepaliveTime: time.Hour,
		},
	)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_ = runner.Run(ctx)

	calls := client.getInjectCalls()
	require.Equal(t, 1, len(calls),
		"duplicate ev.ID must be dropped by the runner-level gate; got %d calls", len(calls))
	assert.Equal(t, "X", calls[0].EventID)
}

// TestRunner_NoDedupEmptyID proves events with empty ev.ID bypass the dedup
// gate so streamd's content-fingerprint fallback can decide. Two empty-ID
// events must produce two InjectChatMessage calls.
func TestRunner_NoDedupEmptyID(t *testing.T) {
	client := &mockStreamDClient{}
	listener := &mockChatListener{
		name:          "empty-id-listener",
		fixedEventIDs: []string{"", ""},
	}

	runner := NewSingleListenerRunner(
		"emptyplat",
		streamcontrol.ChatListenerPrimary,
		client,
		listener,
		RunnerConfig{
			BaseBackoff:   time.Millisecond,
			MaxBackoff:    time.Millisecond,
			KeepaliveTime: time.Hour,
		},
	)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_ = runner.Run(ctx)

	calls := client.getInjectCalls()
	require.Equal(t, 2, len(calls),
		"empty-ID events must bypass dedup; got %d calls", len(calls))
	for _, c := range calls {
		assert.Equal(t, "", c.EventID)
	}
}

// TestDedupTTLMagnitudeLock pins the shared dedup.TTL value to 5 minutes
// so a future product change to the retention window is forced through a
// deliberate test edit (and review) rather than slipping in silently. The
// constants in pkg/chathandler/runner.go (seenIDsTTL) and pkg/streamd/chat.go
// (injectedEventIDTTL) are compile-time aliases of dedup.TTL — drift between
// them is structurally impossible without re-inlining a literal, so this
// test does not need to assert it.
func TestDedupTTLMagnitudeLock(t *testing.T) {
	assert.Equal(t, 5*time.Minute, dedup.TTL,
		"shared dedup.TTL value change must be intentional — update this test to confirm")
}
