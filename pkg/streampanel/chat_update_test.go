package streampanel

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	tassert "github.com/stretchr/testify/assert"
	trequire "github.com/stretchr/testify/require"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamd/api"
)

// fakeChatUI counts every chatUIInterface method invocation so the panel-side
// update-vs-append routing can be asserted in isolation from Fyne. Each
// method's call counter is exposed for the tests; OnAdd is wired through
// GetOnAdd so the test can verify that it does NOT fire on the update path.
type fakeChatUI struct {
	onAddCalls   atomic.Int32
	appendCalls  atomic.Int32
	rebuildCalls atomic.Int32
	removeCalls  atomic.Int32
	updateCalls  atomic.Int32

	lastUpdateIdx int
	lastUpdateMsg api.ChatMessage
}

func (f *fakeChatUI) GetOnAdd() func(context.Context, api.ChatMessage) {
	return func(_ context.Context, _ api.ChatMessage) {
		f.onAddCalls.Add(1)
	}
}
func (f *fakeChatUI) Remove(_ context.Context, _ api.ChatMessage) { f.removeCalls.Add(1) }
func (f *fakeChatUI) Rebuild(_ context.Context)                   { f.rebuildCalls.Add(1) }
func (f *fakeChatUI) Append(_ context.Context, _ int)             { f.appendCalls.Add(1) }
func (f *fakeChatUI) Update(_ context.Context, idx int, msg api.ChatMessage) {
	f.updateCalls.Add(1)
	f.lastUpdateIdx = idx
	f.lastUpdateMsg = msg
}
func (f *fakeChatUI) GetTotalHeight(_ context.Context) float32 { return 0 }
func (f *fakeChatUI) ScrollToBottom(_ context.Context)         {}

// makeMsg builds a ChatMessage with the given identity + content. CreatedAt
// is set to "well before now" so the notification block in onReceiveMessage
// short-circuits before touching ChatConfig (which lives in p.Config and we
// don't initialise for this test).
func makeMsg(
	id streamcontrol.EventID,
	platform streamcontrol.PlatformName,
	content string,
) api.ChatMessage {
	return api.ChatMessage{
		Event: streamcontrol.Event{
			ID: id,
			// Older than one hour so onReceiveMessage skips the
			// notifications block (which dereferences p.Config).
			CreatedAt: time.Now().Add(-2 * time.Hour),
			Type:      streamcontrol.EventTypeChatMessage,
			User: streamcontrol.User{
				ID:   "u1",
				Name: "tester",
			},
			Message: &streamcontrol.Message{
				Content: content,
				Format:  streamcontrol.TextFormatTypePlain,
			},
		},
		Platform: platform,
	}
}

// TestOnReceiveMessage_UpdatesExistingByID locks in the central streampanel
// contract: when the same (ID, Platform) is delivered twice, the panel does
// NOT append a second history entry, calls Update on each chatUI exactly
// once, and does NOT re-fire OnAdd. Without this, the UI would show two
// rows for one logical message after a translation update.
func TestOnReceiveMessage_UpdatesExistingByID(t *testing.T) {
	p := &Panel{}
	ui := &fakeChatUI{}
	ctx := context.Background()
	p.addChatUI(ctx, ui)

	trequire.NotPanics(t, func() {
		p.onReceiveMessage(ctx, makeMsg("evt-1", "twitch", "hola"))
	}, "first onReceiveMessage must succeed against the bare panel")
	trequire.Equal(t, int32(1), ui.appendCalls.Load(),
		"first delivery must take the Append path")
	trequire.Equal(t, int32(0), ui.updateCalls.Load(),
		"first delivery must NOT take the Update path")
	trequire.Equal(t, int32(1), ui.onAddCalls.Load(),
		"first delivery must fire OnAdd exactly once")

	p.onReceiveMessage(ctx, makeMsg("evt-1", "twitch", "hola -文A-> hello"))

	historyLen := func() int {
		var n int
		p.MessagesHistoryLocker.Do(ctx, func() { n = len(p.MessagesHistory) })
		return n
	}()
	tassert.Equal(t, 1, historyLen,
		"second delivery with same (ID, Platform) must REPLACE the entry, not append")

	tassert.Equal(t, int32(1), ui.updateCalls.Load(),
		"second delivery must fire Update exactly once")
	tassert.Equal(t, int32(1), ui.onAddCalls.Load(),
		"OnAdd MUST NOT re-fire on the update path (notifications already played for this message)")
	tassert.Equal(t, int32(1), ui.appendCalls.Load(),
		"Append MUST NOT fire again on the update path")

	tassert.Equal(t, "hola -文A-> hello", ui.lastUpdateMsg.Message.Content,
		"Update must receive the new content")
	tassert.Equal(t, 0, ui.lastUpdateIdx,
		"Update must receive the index of the existing entry")

	// Verify the storage was actually replaced (not just re-bound) — the new
	// content must be the one queryable from MessagesHistory.
	p.MessagesHistoryLocker.Do(ctx, func() {
		trequire.Len(t, p.MessagesHistory, 1)
		tassert.Equal(t, "hola -文A-> hello", p.MessagesHistory[0].Message.Content)
	})
}

// TestOnReceiveMessage_DifferentPlatformAppends covers the dual-side
// assertion: the same bare ID arriving from a different Platform must be
// treated as a NEW message — the dedup is on (ID, Platform), not ID alone.
// Without this, twitch and youtube messages with identical IDs would
// silently overwrite each other.
func TestOnReceiveMessage_DifferentPlatformAppends(t *testing.T) {
	p := &Panel{}
	ui := &fakeChatUI{}
	ctx := context.Background()
	p.addChatUI(ctx, ui)

	p.onReceiveMessage(ctx, makeMsg("evt-1", "twitch", "twitch-content"))
	p.onReceiveMessage(ctx, makeMsg("evt-1", "youtube", "youtube-content"))

	var historyLen int
	p.MessagesHistoryLocker.Do(ctx, func() { historyLen = len(p.MessagesHistory) })
	tassert.Equal(t, 2, historyLen,
		"different platforms with the same bare ID must remain distinct entries")
	tassert.Equal(t, int32(2), ui.appendCalls.Load(),
		"both deliveries must take the Append path")
	tassert.Equal(t, int32(0), ui.updateCalls.Load(),
		"neither delivery must take the Update path")
	tassert.Equal(t, int32(2), ui.onAddCalls.Load(),
		"OnAdd must fire once per net-new message")
}
