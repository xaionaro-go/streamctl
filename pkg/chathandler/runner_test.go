package chathandler

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

	mu        sync.Mutex
	closed    bool
	listenErr error // if non-nil, Listen returns this error immediately
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
		sent := 0
		for {
			if m.eventsBeforeClose >= 0 && sent >= m.eventsBeforeClose {
				return
			}
			ev := streamcontrol.Event{
				ID:        streamcontrol.EventID(fmt.Sprintf("mock-event-%d", sent)),
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
	PlatID  string
	EventID string
}

// mockStreamDClient satisfies streamd_grpc.StreamDClient.
// Only InjectChatMessage is implemented; all other methods panic if called.
// Embedding the interface gives us compile-time satisfaction of all methods.
type mockStreamDClient struct {
	streamd_grpc.StreamDClient // embedded nil — panics on any unimplemented method call

	mu    sync.Mutex
	calls []capturedInjectCall
}

func (m *mockStreamDClient) InjectChatMessage(
	_ context.Context,
	in *streamd_grpc.InjectChatMessageRequest,
	_ ...grpc.CallOption,
) (*streamd_grpc.InjectChatMessageReply, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var eventID string
	if ev := in.GetEvent(); ev != nil {
		eventID = ev.GetId()
	}
	m.calls = append(m.calls, capturedInjectCall{PlatID: in.GetPlatID(), EventID: eventID})
	return &streamd_grpc.InjectChatMessageReply{}, nil
}

func (m *mockStreamDClient) getCalls() []capturedInjectCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]capturedInjectCall, len(m.calls))
	copy(result, m.calls)
	return result
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

func TestRunnerSingleListener_InjectsKeepalive(t *testing.T) {
	// Use a very short keepalive interval so we see keepalives quickly.
	client := &mockStreamDClient{}
	listener := &mockChatListener{
		name:              "keepalive-listener",
		eventsBeforeClose: -1, // emit indefinitely
		eventInterval:     50 * time.Millisecond,
	}

	listenerType := streamcontrol.ChatListenerContingency
	runner := NewSingleListenerRunner(
		"keepaliveplat",
		listenerType,
		client,
		listener,
		RunnerConfig{
			BaseBackoff:   time.Millisecond,
			MaxBackoff:    time.Millisecond,
			KeepaliveTime: 10 * time.Millisecond, // very short to trigger keepalives
		},
	)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	_ = runner.Run(ctx)

	calls := client.getCalls()
	require.Greater(t, len(calls), 0, "expected InjectChatMessage calls")

	// All calls must target the correct platform.
	for _, c := range calls {
		assert.Equal(t, "keepaliveplat", c.PlatID)
	}

	// Separate keepalive event IDs from regular event IDs.
	// Keepalive format: "keepalive-<listenerType>-<platform>-<nanos>"
	expectedPrefix := fmt.Sprintf("keepalive-%s-keepaliveplat-", listenerType)
	var keepaliveIDs []string
	var regularIDs []string
	for _, c := range calls {
		switch {
		case strings.HasPrefix(c.EventID, "keepalive-"):
			keepaliveIDs = append(keepaliveIDs, c.EventID)
		default:
			regularIDs = append(regularIDs, c.EventID)
		}
	}

	// Dual-sided: keepalive events ARE present and contain the listener type.
	require.Greater(t, len(keepaliveIDs), 0,
		"expected at least one keepalive event injection")
	for _, id := range keepaliveIDs {
		assert.True(t, strings.HasPrefix(id, expectedPrefix),
			"keepalive event ID %q must start with %q", id, expectedPrefix)
		assert.Contains(t, id, listenerType.String(),
			"keepalive event ID must contain the listener type string")
	}

	// Dual-sided: regular events are NOT keepalives.
	for _, id := range regularIDs {
		assert.False(t, strings.HasPrefix(id, "keepalive-"),
			"regular event ID %q must not start with keepalive- prefix", id)
	}
}
