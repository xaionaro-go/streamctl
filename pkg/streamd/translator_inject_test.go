package streamd

import (
	"context"
	"net"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tassert "github.com/stretchr/testify/assert"
	trequire "github.com/stretchr/testify/require"
	"github.com/xaionaro-go/eventbus"
	"github.com/xaionaro-go/streamctl/pkg/chatmessagesstorage"
	"github.com/xaionaro-go/streamctl/pkg/llm/translatorbuild"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamd/api"
	"github.com/xaionaro-go/streamctl/pkg/streamd/config"
	translator_grpc "github.com/xaionaro-go/streamctl/pkg/translator/grpc/go/translator_grpc"
	"google.golang.org/grpc"
)

// fakeTranslatorServer is a deterministic translator gRPC server backing the
// streamd-side translator client tests. Each test wires it onto a UNIX
// socket and then runs the production initTranslatorClient end-to-end. The
// translateFn / reconfigureFn injection points let each test pick behaviour
// (success, error, blocking) without standing up a real LLM provider.
type fakeTranslatorServer struct {
	translator_grpc.UnimplementedTranslatorServer
	translateFn     func(*translator_grpc.TranslateRequest) (*translator_grpc.TranslateReply, error)
	reconfigureFn   func(*translator_grpc.ReconfigureRequest) (*translator_grpc.ReconfigureReply, error)
	statsFn         func(context.Context, *translator_grpc.StatsRequest) (*translator_grpc.StatsReply, error)
	clearHistoryFn  func(context.Context, *translator_grpc.ClearHistoryRequest) (*translator_grpc.ClearHistoryReply, error)

	calls atomic.Int32
}

func (f *fakeTranslatorServer) Translate(
	_ context.Context,
	req *translator_grpc.TranslateRequest,
) (*translator_grpc.TranslateReply, error) {
	f.calls.Add(1)
	return f.translateFn(req)
}

func (f *fakeTranslatorServer) Reconfigure(
	_ context.Context,
	req *translator_grpc.ReconfigureRequest,
) (*translator_grpc.ReconfigureReply, error) {
	if f.reconfigureFn != nil {
		return f.reconfigureFn(req)
	}
	// Default: accept any config so the production initTranslatorClient
	// wiring succeeds for tests that do not care about Reconfigure.
	return &translator_grpc.ReconfigureReply{Applied: true}, nil
}

func (f *fakeTranslatorServer) Stats(
	ctx context.Context,
	req *translator_grpc.StatsRequest,
) (*translator_grpc.StatsReply, error) {
	if f.statsFn != nil {
		return f.statsFn(ctx, req)
	}
	return &translator_grpc.StatsReply{}, nil
}

func (f *fakeTranslatorServer) ClearHistory(
	ctx context.Context,
	req *translator_grpc.ClearHistoryRequest,
) (*translator_grpc.ClearHistoryReply, error) {
	if f.clearHistoryFn != nil {
		return f.clearHistoryFn(ctx, req)
	}
	return &translator_grpc.ClearHistoryReply{}, nil
}

// startFakeTranslator binds a UNIX socket inside t.TempDir() and serves the
// fake translator on it. Returns the socket path so the production
// initTranslatorClient can dial it. The server is torn down when the test
// ends.
func startFakeTranslator(
	t *testing.T,
	fake *fakeTranslatorServer,
) string {
	t.Helper()
	dir := t.TempDir()
	sock := filepath.Join(dir, "t.sock")

	listener, err := net.Listen("unix", sock)
	trequire.NoError(t, err, "listen unix")
	srv := grpc.NewServer()
	translator_grpc.RegisterTranslatorServer(srv, fake)
	t.Cleanup(srv.Stop)
	go func() { _ = srv.Serve(listener) }()
	return sock
}

// newTestStreamD assembles the minimal StreamD needed to drive
// InjectChatMessage / enqueueTranslation / translateOneJob through the
// translation worker. It wires:
//   - an in-memory ChatMessagesStorage (no disk),
//   - a fresh EventBus,
//   - a Config with TargetLanguage set so the translator wiring activates.
//
// The translator subprocess + translation worker are NOT yet wired. Callers
// either stay disabled (no TargetLanguage) or call wireTestTranslator to plug
// a fake server in via the production setup path.
func newTestStreamD(t *testing.T) (*StreamD, context.Context, context.CancelFunc) {
	t.Helper()
	d := &StreamD{
		ChatMessagesStorage: chatmessagesstorage.New(""),
		EventBus:            eventbus.New(),
		Config: config.Config{
			Translation: translatorbuild.Config{
				TargetLanguage: "English",
			},
		},
	}
	// onUpdateConfig (called from SetConfig) invokes
	// updateOBSRestarterConfig which dereferences d.obsRestarter. Without
	// initializing it here every translator-config-mutation test would
	// panic on the first SetConfig.
	d.obsRestarter = newOBSRestarter(d)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return d, ctx, cancel
}

// wireTestTranslator drives the production startup wiring for the translator
// against a fake server's UNIX socket. Tests exercise the same
// setupTranslationWorker + initTranslatorClient code paths the real
// StreamD.Run path uses, instead of poking private fields. The translator
// subprocess "process" is faked: we register an externalTranslator with no
// cmd/cancelFunc but a real socketPath, then let initTranslatorClient dial
// and Reconfigure it.
func wireTestTranslator(
	t *testing.T,
	ctx context.Context,
	d *StreamD,
	fake *fakeTranslatorServer,
) {
	t.Helper()
	sock := startFakeTranslator(t, fake)

	// Register the fake subprocess so initTranslatorClient finds a
	// socketPath to dial. cmd/cancelFunc are nil; that is fine because
	// nothing in the test paths reads them (Wait / cancel never fire).
	d.registerTranslator(&externalTranslator{socketPath: sock})

	d.setupTranslationWorker(ctx)
	trequire.NoError(t, d.initTranslatorClient(ctx),
		"production initTranslatorClient must succeed against the fake server")
}

// subscribeChatMessages subscribes to the EventBus topic that
// publishEvent(ctx, d.EventBus, msg) hits when msg has type api.ChatMessage.
// Returns a channel that drains immediately so the publish path never blocks.
func subscribeChatMessages(
	t *testing.T,
	ctx context.Context,
	d *StreamD,
) <-chan api.ChatMessage {
	t.Helper()
	var topic api.ChatMessage
	sub := eventbus.SubscribeWithCustomTopic[api.ChatMessage, api.ChatMessage](
		ctx, d.EventBus, topic,
		eventbus.OptionQueueSize(1),
		eventbus.OptionOnOverflow(eventbus.OnOverflowPileUpOrClose(64, 5*time.Second)),
	)
	return sub.EventChan()
}

// makeChatEvent builds a streamcontrol.Event with the supplied id and content
// — the minimal shape the inject path needs.
func makeChatEvent(id string, content string) streamcontrol.Event {
	return streamcontrol.Event{
		ID:        streamcontrol.EventID(id),
		CreatedAt: time.Now(),
		Type:      streamcontrol.EventTypeChatMessage,
		User: streamcontrol.User{
			ID:   "u1",
			Name: "tester",
		},
		Message: &streamcontrol.Message{
			Content: content,
			Format:  streamcontrol.TextFormatTypePlain,
		},
	}
}

// TestInjectChatMessage_RawFirstThenTranslationUpdate is the central
// assertion of the raw-first contract: ONE InjectChatMessage call must
// produce TWO publishes — first the untranslated message (IsLive=true),
// then the translated update (IsLive=false, content carries the
// translationSeparator). Both must share the same ID so the UI can replace
// the row.
//
// Falsification gap: a naive "publish first, await second" assertion would
// pass coincidentally even if InjectChatMessage swapped the order, because
// the synchronous publish would beat the async gRPC even on a swapped path.
// We close that gap by BLOCKING the fake translator on a release channel.
// While the translator is blocked, the only way the raw publish can land is
// if processChatMessage runs BEFORE enqueueTranslation. After releasing the
// gate, the translation update must follow.
func TestInjectChatMessage_RawFirstThenTranslationUpdate(t *testing.T) {
	d, ctx, cancel := newTestStreamD(t)
	defer cancel()

	releaseTranslate := make(chan struct{})
	translateEntered := make(chan struct{}, 1)
	fake := &fakeTranslatorServer{
		translateFn: func(req *translator_grpc.TranslateRequest) (*translator_grpc.TranslateReply, error) {
			// Signal arrival at the gate (non-blocking — buffered to 1
			// so a duplicate translate call cannot deadlock the test).
			select {
			case translateEntered <- struct{}{}:
			default:
			}
			<-releaseTranslate
			return &translator_grpc.TranslateReply{
				Result:  "hello",
				Outcome: translator_grpc.Outcome_OUTCOME_TRANSLATED,
			}, nil
		},
	}
	wireTestTranslator(t, ctx, d, fake)

	msgs := subscribeChatMessages(t, ctx, d)

	trequire.NoError(t, d.InjectChatMessage(ctx, "twitch", streamcontrol.ChatListenerPrimary, makeChatEvent("evt-1", "hola")))

	// First publish must be the raw message AND must arrive while the
	// translator is still blocked. If InjectChatMessage swapped order,
	// processChatMessage would not run until after the (blocked) translate
	// returns — receiveOrFail would time out.
	first := receiveOrFail(t, msgs, "raw publish (translator blocked)")
	tassert.Equal(t, streamcontrol.EventID("evt-1"), first.ID)
	tassert.True(t, first.IsLive, "raw publish must be marked live")
	tassert.Equal(t, "hola", first.Message.Content,
		"raw publish must NOT carry the translation separator yet")
	tassert.NotContains(t, first.Message.Content, translationSeparator)

	// Confirm the translator did get the call (so the test would not pass
	// vacuously if no Translate ever fired).
	select {
	case <-translateEntered:
	case <-time.After(2 * time.Second):
		t.Fatalf("translator was never invoked")
	}

	// Release the translator. The translation update must now follow.
	close(releaseTranslate)

	second := receiveOrFail(t, msgs, "translation update")
	tassert.Equal(t, streamcontrol.EventID("evt-1"), second.ID,
		"translation update must reuse the original event ID so the UI can replace by ID")
	tassert.False(t, second.IsLive, "translation update must NOT re-trigger live notifications")
	tassert.Contains(t, second.Message.Content, translationSeparator,
		"translation update must include the translationSeparator")
	tassert.True(t, strings.HasPrefix(second.Message.Content, "hola"+translationSeparator),
		"translation update must keep the original prefix")
	tassert.Contains(t, second.Message.Content, "hello",
		"translation update must include the translated text")
}

// TestEnqueueTranslation_QueueFullSkips locks in the must-not-block contract:
// when the worker channel is full, enqueueTranslation drops the job (does NOT
// block, does NOT spawn an extra worker) and increments the drop counter.
// Without this, a slow translator would back-pressure ingestion.
//
// This test does not need a translator subprocess; it pokes translationJobs
// directly to a length-1 channel with no consumer so it can lock in the
// drop-counter behaviour with no timing race. Other tests that need real
// wiring use wireTestTranslator.
func TestEnqueueTranslation_QueueFullSkips(t *testing.T) {
	d, ctx, cancel := newTestStreamD(t)
	defer cancel()

	// Capacity 1, no consumer: the first send fills it, every subsequent
	// send must hit the default case.
	d.translationJobs = make(chan *acceptedJob, 1)

	d.enqueueTranslation(ctx, translationJob{platID: "twitch", msg: api.ChatMessage{
		Event: makeChatEvent("evt-fill", "filler"),
	}})
	trequire.Equal(t, int64(0), d.translationWorkerQueueDrops.Load(),
		"the first job must fit and not increment the drop counter")

	for i := 0; i < 5; i++ {
		d.enqueueTranslation(ctx, translationJob{platID: "twitch", msg: api.ChatMessage{
			Event: makeChatEvent("evt-overflow", "overflow"),
		}})
	}

	tassert.Equal(t, int64(5), d.translationWorkerQueueDrops.Load(),
		"every overflowed enqueue must increment the drop counter")
}

// TestTranslationWorker_DropsOnRPCError locks in that an RPC failure does NOT
// cause an update publish. The raw publish has already happened upstream;
// the worker swallows the error and moves on so chat keeps flowing.
func TestTranslationWorker_DropsOnRPCError(t *testing.T) {
	d, ctx, cancel := newTestStreamD(t)
	defer cancel()

	fake := &fakeTranslatorServer{
		translateFn: func(req *translator_grpc.TranslateRequest) (*translator_grpc.TranslateReply, error) {
			return nil, assertGRPCError("translate boom")
		},
	}
	wireTestTranslator(t, ctx, d, fake)

	msgs := subscribeChatMessages(t, ctx, d)
	trequire.NoError(t, d.InjectChatMessage(ctx, "twitch", streamcontrol.ChatListenerPrimary, makeChatEvent("evt-err", "halo")))

	// Raw publish must arrive.
	first := receiveOrFail(t, msgs, "raw publish")
	tassert.Equal(t, streamcontrol.EventID("evt-err"), first.ID)

	// No update publish: drainNoMore asserts that nothing else lands.
	drainNoMore(t, msgs, 200*time.Millisecond)

	tassert.GreaterOrEqual(t, fake.calls.Load(), int32(1),
		"the worker must have attempted the Translate RPC")
}

// TestTranslationWorker_NoUpdateWhenUnchanged locks in that when the
// translator returns the same string (or empty), the worker does NOT
// re-publish. Otherwise the UI would replace the row with an identical
// copy and re-fire any update-side handlers that consult content.
func TestTranslationWorker_NoUpdateWhenUnchanged(t *testing.T) {
	d, ctx, cancel := newTestStreamD(t)
	defer cancel()

	fake := &fakeTranslatorServer{
		translateFn: func(req *translator_grpc.TranslateRequest) (*translator_grpc.TranslateReply, error) {
			// Echo: the translator decided the message was already in target.
			return &translator_grpc.TranslateReply{Result: req.GetMessage()}, nil
		},
	}
	wireTestTranslator(t, ctx, d, fake)

	msgs := subscribeChatMessages(t, ctx, d)

	trequire.NoError(t, d.InjectChatMessage(ctx, "twitch", streamcontrol.ChatListenerPrimary, makeChatEvent("evt-noop", "hello")))

	first := receiveOrFail(t, msgs, "raw publish")
	tassert.Equal(t, streamcontrol.EventID("evt-noop"), first.ID)
	drainNoMore(t, msgs, 200*time.Millisecond)
}

// receiveOrFail returns the next message or fails the test if none arrives in
// time. Avoids open-coded select+timeout in every assertion.
func receiveOrFail(
	t *testing.T,
	ch <-chan api.ChatMessage,
	what string,
) api.ChatMessage {
	t.Helper()
	select {
	case m, ok := <-ch:
		if !ok {
			t.Fatalf("%s: channel closed before message arrived", what)
		}
		return m
	case <-time.After(2 * time.Second):
		t.Fatalf("%s: no message within 2s", what)
		return api.ChatMessage{}
	}
}

// drainNoMore asserts the channel stays empty for the supplied window.
// Used to confirm "no update was published" without racing the worker.
func drainNoMore(
	t *testing.T,
	ch <-chan api.ChatMessage,
	d time.Duration,
) {
	t.Helper()
	select {
	case m := <-ch:
		t.Fatalf("expected no further publishes, got %#+v", m)
	case <-time.After(d):
	}
}

// assertGRPCError builds an error that propagates through gRPC as a
// non-nil RPC error without depending on grpc/status — keeps the test
// surface small.
func assertGRPCError(msg string) error { return errString(msg) }

type errString string

func (e errString) Error() string { return string(e) }
