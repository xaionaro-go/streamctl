package streamd

import (
	"context"
	"sync"
	"testing"
	"time"

	tassert "github.com/stretchr/testify/assert"
	trequire "github.com/stretchr/testify/require"
	"github.com/xaionaro-go/eventbus"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/streamctl/pkg/chatmessagesstorage"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/kick"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/twitch"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/youtube"
	"github.com/xaionaro-go/streamctl/pkg/streamd/api"
)

// newCollapseTestStreamD assembles the smallest StreamD that can drive
// InjectChatMessage end-to-end without the translation worker. Translation
// is intentionally NOT wired: the cross-source-collapse logic must work
// independently of translator availability.
func newCollapseTestStreamD(t *testing.T) (*StreamD, context.Context, context.CancelFunc) {
	t.Helper()
	d := &StreamD{
		ChatMessagesStorage: chatmessagesstorage.New(""),
		EventBus:            eventbus.New(),
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return d, ctx, cancel
}

// drainChatMessages pulls exactly `expected` ChatMessages out of the
// subscription channel. Blocks until that many have been received OR the
// 5-second deadline fires (in which case the test fails). Using a count
// gate instead of a quiescence window removes false negatives from a
// loaded host: the previous quiet-window approach could time out with
// events still in flight.
//
// Callers that want to assert "no more arrive after N" combine
// drainChatMessages(t, ch, N) with drainNoMore(t, ch, …).
func drainChatMessages(
	t *testing.T,
	ch <-chan api.ChatMessage,
	expected int,
) []api.ChatMessage {
	t.Helper()
	out := make([]api.ChatMessage, 0, expected)
	deadline := time.After(5 * time.Second)
	for len(out) < expected {
		select {
		case m, ok := <-ch:
			if !ok {
				t.Fatalf("drainChatMessages: channel closed after %d/%d", len(out), expected)
			}
			out = append(out, m)
		case <-deadline:
			t.Fatalf("drainChatMessages: timed out waiting for %d messages, got %d", expected, len(out))
		}
	}
	return out
}

// drainChatMessagesAvailable pulls every ChatMessage currently available on
// the subscription channel, returning whatever has accumulated by the time
// `quiet` elapses with no new arrivals. For tests where the EXACT publish
// count cannot be predicted (concurrent races, race-detector probes), this
// is the right tool — drainChatMessages is wrong because the count cannot
// be expressed up-front. Tests that DO know the exact count must use
// drainChatMessages so a missed publish fails the test rather than
// silently slipping past a quiescence window.
func drainChatMessagesAvailable(
	t *testing.T,
	ch <-chan api.ChatMessage,
	quiet time.Duration,
) []api.ChatMessage {
	t.Helper()
	var out []api.ChatMessage
	for {
		select {
		case m, ok := <-ch:
			if !ok {
				return out
			}
			out = append(out, m)
		case <-time.After(quiet):
			return out
		}
	}
}

// makeCollapseEvent builds an Event suitable for the collapse tests. The
// timestamp is fixed so two events constructed with the same content land
// in the same fingerprint bucket regardless of test scheduling jitter.
func makeCollapseEvent(
	id string,
	content string,
	at time.Time,
) streamcontrol.Event {
	return streamcontrol.Event{
		ID:        streamcontrol.EventID(id),
		CreatedAt: at,
		Type:      streamcontrol.EventTypeChatMessage,
		User: streamcontrol.User{
			ID:   "u-1",
			Slug: "u-1",
			Name: "user one",
		},
		Message: &streamcontrol.Message{
			Content: content,
			Format:  streamcontrol.TextFormatTypePlain,
		},
	}
}

// fixedCollapseTime returns a deterministic timestamp. Sharing the
// timestamp across two events guarantees they land in the same
// fingerprint bucket so any non-collapse must come from the dedup logic
// itself, not from CreatedAt drift across test boundaries.
func fixedCollapseTime() time.Time {
	return time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC)
}

// fpEntryForContent looks up the fpEntry created by an event with the
// supplied platform/content/createdAt. Tests use this to assert on the
// shape of the source-group list without poking at internal hashing.
func fpEntryForContent(
	t *testing.T,
	d *StreamD,
	platID streamcontrol.PlatformName,
	content string,
	at time.Time,
) *fpEntry {
	t.Helper()
	ev := makeCollapseEvent("probe", content, at)
	fp := fingerprintEventForCollapse(platID, ev)
	e, ok := d.contentFingerprintIndex.Load(fp)
	trequire.True(t, ok, "expected fpEntry for fp=%s (content=%q)", fp, content)
	return e
}

func TestInjectCollapse_TwitchPrimaryThenAlternate_OneCollapse(t *testing.T) {
	d, ctx, _ := newCollapseTestStreamD(t)
	msgs := subscribeChatMessages(t, ctx, d)

	at := fixedCollapseTime()
	primaryEv := makeCollapseEvent("twitch-id-primary", "lol", at)
	alternateEv := makeCollapseEvent("twitch-id-alt", "lol", at)

	trequire.NoError(t, d.InjectChatMessage(ctx, twitch.ID, streamcontrol.ChatListenerPrimary, primaryEv))
	trequire.NoError(t, d.InjectChatMessage(ctx, twitch.ID, streamcontrol.ChatListenerAlternate, alternateEv))

	got := drainChatMessages(t, msgs, 1)
	drainNoMore(t, msgs, 100*time.Millisecond)
	tassert.Equal(t, streamcontrol.EventID("twitch-id-primary"), got[0].ID,
		"the surviving publish must be the first one, not the collapsed one")

	// fpEntry must have one group with both sources.
	entry := fpEntryForContent(t, d, twitch.ID, "lol", at)
	entry.mu.Lock()
	tassert.Equal(t, 1, len(entry.groups),
		"one logical event => one source-group")
	tassert.Equal(t, 2, len(entry.groups[0].Sources),
		"the single group must contain both contributing sources")
	entry.mu.Unlock()

	// Both dedup keys are tracked under injectedEvents so a third repeat
	// from either listener short-circuits at Step 1.
	keyPrimary := computeDedupKey(ctx, twitch.ID, primaryEv)
	keyAlternate := computeDedupKey(ctx, twitch.ID, alternateEv)
	_, hasPrimary := d.injectedEvents.Load(keyPrimary)
	_, hasAlternate := d.injectedEvents.Load(keyAlternate)
	tassert.True(t, hasPrimary, "primary key must be in injectedEvents")
	tassert.True(t, hasAlternate, "back-reference: alternate key must be in injectedEvents")
}

func TestInjectCollapse_KickPrimaryThenContingency_OneCollapse(t *testing.T) {
	d, ctx, _ := newCollapseTestStreamD(t)
	msgs := subscribeChatMessages(t, ctx, d)

	at := fixedCollapseTime()
	primaryEv := makeCollapseEvent("kick-id-primary", "lol", at)
	contingencyEv := makeCollapseEvent("kick-id-cont", "lol", at)

	trequire.NoError(t, d.InjectChatMessage(ctx, kick.ID, streamcontrol.ChatListenerPrimary, primaryEv))
	trequire.NoError(t, d.InjectChatMessage(ctx, kick.ID, streamcontrol.ChatListenerContingency, contingencyEv))

	got := drainChatMessages(t, msgs, 1)
	drainNoMore(t, msgs, 100*time.Millisecond)
	tassert.Equal(t, streamcontrol.EventID("kick-id-primary"), got[0].ID)

	entry := fpEntryForContent(t, d, kick.ID, "lol", at)
	entry.mu.Lock()
	tassert.Equal(t, 1, len(entry.groups))
	tassert.Equal(t, 2, len(entry.groups[0].Sources))
	entry.mu.Unlock()
}

func TestInjectCollapse_YouTubePrimaryThenAlternate_OneCollapse(t *testing.T) {
	d, ctx, _ := newCollapseTestStreamD(t)
	msgs := subscribeChatMessages(t, ctx, d)

	at := fixedCollapseTime()
	primaryEv := makeCollapseEvent("yt-id-primary", "lol", at)
	alternateEv := makeCollapseEvent("yt-id-alt", "lol", at)

	trequire.NoError(t, d.InjectChatMessage(ctx, youtube.ID, streamcontrol.ChatListenerPrimary, primaryEv))
	trequire.NoError(t, d.InjectChatMessage(ctx, youtube.ID, streamcontrol.ChatListenerAlternate, alternateEv))

	_ = drainChatMessages(t, msgs, 1)
	drainNoMore(t, msgs, 100*time.Millisecond)

	entry := fpEntryForContent(t, d, youtube.ID, "lol", at)
	entry.mu.Lock()
	tassert.Equal(t, 1, len(entry.groups))
	tassert.Equal(t, 2, len(entry.groups[0].Sources))
	entry.mu.Unlock()
}

// TestInjectCollapse_SameSourceTwice_TwoPublishes_TwoGroups locks in the
// invariant: the same listener emitting the same content twice (different
// IDs) is two real user messages — both must publish, two distinct
// source-groups must be created.
func TestInjectCollapse_SameSourceTwice_TwoPublishes_TwoGroups(t *testing.T) {
	d, ctx, _ := newCollapseTestStreamD(t)
	msgs := subscribeChatMessages(t, ctx, d)

	at := fixedCollapseTime()
	first := makeCollapseEvent("twitch-A1", "lol", at)
	second := makeCollapseEvent("twitch-A2", "lol", at)

	trequire.NoError(t, d.InjectChatMessage(ctx, twitch.ID, streamcontrol.ChatListenerPrimary, first))
	trequire.NoError(t, d.InjectChatMessage(ctx, twitch.ID, streamcontrol.ChatListenerPrimary, second))

	got := drainChatMessages(t, msgs, 2)
	drainNoMore(t, msgs, 100*time.Millisecond)
	tassert.Equal(t, streamcontrol.EventID("twitch-A1"), got[0].ID)
	tassert.Equal(t, streamcontrol.EventID("twitch-A2"), got[1].ID)

	entry := fpEntryForContent(t, d, twitch.ID, "lol", at)
	entry.mu.Lock()
	tassert.Equal(t, 2, len(entry.groups),
		"each emission opened a new source-group")
	tassert.Equal(t, 1, len(entry.groups[0].Sources),
		"each group has one source after two same-source emissions")
	tassert.Equal(t, 1, len(entry.groups[1].Sources))
	entry.mu.Unlock()
}

// TestInjectCollapse_DoubleAcrossTwoSources_TwoPublishesTwoGroupsTwoSourcesEach
// — A "lol", A "lol", B "lol", B "lol" → 2 publishes (A1, A2); B1 links
// into G1, B2 links into G2; both groups end up {A.primary, B.alternate}.
func TestInjectCollapse_DoubleAcrossTwoSources_TwoPublishesTwoGroupsTwoSourcesEach(t *testing.T) {
	d, ctx, _ := newCollapseTestStreamD(t)
	msgs := subscribeChatMessages(t, ctx, d)

	at := fixedCollapseTime()
	a1 := makeCollapseEvent("A1", "lol", at)
	a2 := makeCollapseEvent("A2", "lol", at)
	b1 := makeCollapseEvent("B1", "lol", at)
	b2 := makeCollapseEvent("B2", "lol", at)

	trequire.NoError(t, d.InjectChatMessage(ctx, twitch.ID, streamcontrol.ChatListenerPrimary, a1))
	trequire.NoError(t, d.InjectChatMessage(ctx, twitch.ID, streamcontrol.ChatListenerPrimary, a2))
	trequire.NoError(t, d.InjectChatMessage(ctx, twitch.ID, streamcontrol.ChatListenerAlternate, b1))
	trequire.NoError(t, d.InjectChatMessage(ctx, twitch.ID, streamcontrol.ChatListenerAlternate, b2))

	got := drainChatMessages(t, msgs, 2)
	drainNoMore(t, msgs, 100*time.Millisecond)
	tassert.Equal(t, streamcontrol.EventID("A1"), got[0].ID)
	tassert.Equal(t, streamcontrol.EventID("A2"), got[1].ID)

	entry := fpEntryForContent(t, d, twitch.ID, "lol", at)
	entry.mu.Lock()
	tassert.Equal(t, 2, len(entry.groups),
		"two distinct logical events => two groups")
	srcA := eventSource{Platform: twitch.ID, ListenerType: streamcontrol.ChatListenerPrimary}
	srcB := eventSource{Platform: twitch.ID, ListenerType: streamcontrol.ChatListenerAlternate}
	for i, g := range entry.groups {
		tassert.Equal(t, 2, len(g.Sources),
			"group %d must contain both sources after the cross-source link", i)
		_, hasA := g.Sources[srcA]
		_, hasB := g.Sources[srcB]
		tassert.True(t, hasA, "group %d missing source A", i)
		tassert.True(t, hasB, "group %d missing source B", i)
	}
	entry.mu.Unlock()
}

// TestInjectCollapse_TripleSourceOneEvent_OnePublishOneGroupThreeSources —
// A, B, C (different listener-types) emit "lol" once → 1 publish (A);
// the group has 3 sources.
func TestInjectCollapse_TripleSourceOneEvent_OnePublishOneGroupThreeSources(t *testing.T) {
	d, ctx, _ := newCollapseTestStreamD(t)
	msgs := subscribeChatMessages(t, ctx, d)

	at := fixedCollapseTime()
	evA := makeCollapseEvent("A", "lol", at)
	evB := makeCollapseEvent("B", "lol", at)
	evC := makeCollapseEvent("C", "lol", at)

	trequire.NoError(t, d.InjectChatMessage(ctx, twitch.ID, streamcontrol.ChatListenerPrimary, evA))
	trequire.NoError(t, d.InjectChatMessage(ctx, twitch.ID, streamcontrol.ChatListenerAlternate, evB))
	trequire.NoError(t, d.InjectChatMessage(ctx, twitch.ID, streamcontrol.ChatListenerContingency, evC))

	got := drainChatMessages(t, msgs, 1)
	drainNoMore(t, msgs, 100*time.Millisecond)
	tassert.Equal(t, streamcontrol.EventID("A"), got[0].ID)

	entry := fpEntryForContent(t, d, twitch.ID, "lol", at)
	entry.mu.Lock()
	tassert.Equal(t, 1, len(entry.groups))
	tassert.Equal(t, 3, len(entry.groups[0].Sources),
		"all three sources must be members of the single group")
	entry.mu.Unlock()
}

// TestInjectCollapse_SourceRepeatsAfterLink_NewEvent — A emits, B links
// onto it, A emits again. The second A's source is already in the only
// group, so a NEW group must open and a second publish must fire.
func TestInjectCollapse_SourceRepeatsAfterLink_NewEvent(t *testing.T) {
	d, ctx, _ := newCollapseTestStreamD(t)
	msgs := subscribeChatMessages(t, ctx, d)

	at := fixedCollapseTime()
	a1 := makeCollapseEvent("A1", "lol", at)
	b1 := makeCollapseEvent("B1", "lol", at)
	a2 := makeCollapseEvent("A2", "lol", at)

	trequire.NoError(t, d.InjectChatMessage(ctx, twitch.ID, streamcontrol.ChatListenerPrimary, a1))
	trequire.NoError(t, d.InjectChatMessage(ctx, twitch.ID, streamcontrol.ChatListenerAlternate, b1))
	trequire.NoError(t, d.InjectChatMessage(ctx, twitch.ID, streamcontrol.ChatListenerPrimary, a2))

	got := drainChatMessages(t, msgs, 2)
	drainNoMore(t, msgs, 100*time.Millisecond)
	tassert.Equal(t, streamcontrol.EventID("A1"), got[0].ID)
	tassert.Equal(t, streamcontrol.EventID("A2"), got[1].ID)

	entry := fpEntryForContent(t, d, twitch.ID, "lol", at)
	entry.mu.Lock()
	tassert.Equal(t, 2, len(entry.groups),
		"two logical events => two groups")
	tassert.Equal(t, 2, len(entry.groups[0].Sources),
		"first group: {A.primary, B.alternate}")
	tassert.Equal(t, 1, len(entry.groups[1].Sources),
		"second group: {A.primary} only — B did not (yet) emit again")
	entry.mu.Unlock()
}

func TestInjectCollapse_EmptyContent_NoCollapse_NoFPIndex(t *testing.T) {
	d, ctx, _ := newCollapseTestStreamD(t)
	msgs := subscribeChatMessages(t, ctx, d)

	at := fixedCollapseTime()
	a := makeCollapseEvent("twitch-empty-A", "", at)
	b := makeCollapseEvent("twitch-empty-B", "", at)

	trequire.NoError(t, d.InjectChatMessage(ctx, twitch.ID, streamcontrol.ChatListenerPrimary, a))
	trequire.NoError(t, d.InjectChatMessage(ctx, twitch.ID, streamcontrol.ChatListenerAlternate, b))

	_ = drainChatMessages(t, msgs, 2)
	drainNoMore(t, msgs, 100*time.Millisecond)

	count := 0
	d.contentFingerprintIndex.Range(func(string, *fpEntry) bool {
		count++
		return true
	})
	tassert.Equal(t, 0, count,
		"contentFingerprintIndex must not record empty-content events")
}

func TestInjectCollapse_NilMessage_NoCollapse(t *testing.T) {
	d, ctx, _ := newCollapseTestStreamD(t)
	msgs := subscribeChatMessages(t, ctx, d)

	at := fixedCollapseTime()
	a := streamcontrol.Event{
		ID:        "twitch-nil-A",
		CreatedAt: at,
		Type:      streamcontrol.EventTypeFollow,
		User:      streamcontrol.User{ID: "u-1"},
	}
	b := a
	b.ID = "twitch-nil-B"

	trequire.NoError(t, d.InjectChatMessage(ctx, twitch.ID, streamcontrol.ChatListenerPrimary, a))
	trequire.NoError(t, d.InjectChatMessage(ctx, twitch.ID, streamcontrol.ChatListenerAlternate, b))

	_ = drainChatMessages(t, msgs, 2)
	drainNoMore(t, msgs, 100*time.Millisecond)

	count := 0
	d.contentFingerprintIndex.Range(func(string, *fpEntry) bool {
		count++
		return true
	})
	tassert.Equal(t, 0, count,
		"contentFingerprintIndex must not record nil-message events")
}

func TestInjectCollapse_DifferentTimeBuckets_NoCollapse(t *testing.T) {
	d, ctx, _ := newCollapseTestStreamD(t)
	msgs := subscribeChatMessages(t, ctx, d)

	bucketA := time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC).Truncate(injectedEventIDTTL)
	bucketB := bucketA.Add(injectedEventIDTTL + time.Minute)

	a := makeCollapseEvent("twitch-bucket-A", "msg", bucketA)
	b := makeCollapseEvent("twitch-bucket-B", "msg", bucketB)

	trequire.NoError(t, d.InjectChatMessage(ctx, twitch.ID, streamcontrol.ChatListenerPrimary, a))
	trequire.NoError(t, d.InjectChatMessage(ctx, twitch.ID, streamcontrol.ChatListenerAlternate, b))

	_ = drainChatMessages(t, msgs, 2)
	drainNoMore(t, msgs, 100*time.Millisecond)

	count := 0
	d.contentFingerprintIndex.Range(func(string, *fpEntry) bool {
		count++
		return true
	})
	tassert.Equal(t, 2, count,
		"two distinct fp entries (one per bucket)")
}

func TestInjectCollapse_CrossPlatformSameContent_NoCollapse(t *testing.T) {
	d, ctx, _ := newCollapseTestStreamD(t)
	msgs := subscribeChatMessages(t, ctx, d)

	at := fixedCollapseTime()
	twitchEv := makeCollapseEvent("twitch-cross-id", "hi", at)
	kickEv := makeCollapseEvent("kick-cross-id", "hi", at)

	trequire.NoError(t, d.InjectChatMessage(ctx, twitch.ID, streamcontrol.ChatListenerPrimary, twitchEv))
	trequire.NoError(t, d.InjectChatMessage(ctx, kick.ID, streamcontrol.ChatListenerPrimary, kickEv))

	_ = drainChatMessages(t, msgs, 2)
	drainNoMore(t, msgs, 100*time.Millisecond)

	entries := 0
	d.contentFingerprintIndex.Range(func(string, *fpEntry) bool {
		entries++
		return true
	})
	tassert.Equal(t, 2, entries,
		"each platform must register its own fingerprint entry")
}

func TestInjectCollapse_DifferentEventType_NoCollapse(t *testing.T) {
	d, ctx, _ := newCollapseTestStreamD(t)
	msgs := subscribeChatMessages(t, ctx, d)

	at := fixedCollapseTime()
	subscribe := makeCollapseEvent("evtype-sub", "thanks!", at)
	subscribe.Type = streamcontrol.EventTypeSubscriptionNew
	follow := makeCollapseEvent("evtype-follow", "thanks!", at)
	follow.Type = streamcontrol.EventTypeFollow

	trequire.NoError(t, d.InjectChatMessage(ctx, twitch.ID, streamcontrol.ChatListenerPrimary, subscribe))
	trequire.NoError(t, d.InjectChatMessage(ctx, twitch.ID, streamcontrol.ChatListenerAlternate, follow))

	_ = drainChatMessages(t, msgs, 2)
	drainNoMore(t, msgs, 100*time.Millisecond)

	entries := 0
	d.contentFingerprintIndex.Range(func(string, *fpEntry) bool {
		entries++
		return true
	})
	tassert.Equal(t, 2, entries,
		"two distinct fp entries (one per event type)")
}

// TestInjectCollapse_TTLBoundaryEviction_GroupExpires — inject A "lol";
// rewrite group.InsertedAt past the cutoff; trigger cleanup; B "lol"
// must publish (new group, evicted from old).
func TestInjectCollapse_TTLBoundaryEviction_GroupExpires(t *testing.T) {
	d, ctx, _ := newCollapseTestStreamD(t)
	msgs := subscribeChatMessages(t, ctx, d)

	at := fixedCollapseTime()
	first := makeCollapseEvent("twitch-ttl-A", "evict-me", at)
	second := makeCollapseEvent("twitch-ttl-B", "evict-me", at)

	trequire.NoError(t, d.InjectChatMessage(ctx, twitch.ID, streamcontrol.ChatListenerPrimary, first))

	// Force the group stale by rewriting InsertedAt to before the TTL cutoff.
	staleAt := time.Now().Add(-2 * injectedEventIDTTL)
	d.contentFingerprintIndex.Range(func(_ string, entry *fpEntry) bool {
		entry.mu.Lock()
		for _, g := range entry.groups {
			g.InsertedAt = staleAt
		}
		entry.mu.Unlock()
		return true
	})
	// Also stale-out the injectedEvents key so the second inject cannot
	// short-circuit at Step 1 either.
	d.injectedEvents.Range(func(k dedupKey, _ time.Time) bool {
		d.injectedEvents.Store(k, staleAt)
		return true
	})

	// Manual sweep: reproduce the cleanup goroutine logic.
	cutoff := time.Now().Add(-injectedEventIDTTL)
	d.injectedEvents.Range(func(k dedupKey, insertedAt time.Time) bool {
		if insertedAt.Before(cutoff) {
			d.injectedEvents.Delete(k)
		}
		return true
	})
	d.contentFingerprintIndex.Range(func(fp string, entry *fpEntry) bool {
		entry.mu.Lock()
		kept := entry.groups[:0]
		for _, g := range entry.groups {
			if g.InsertedAt.After(cutoff) || g.InsertedAt.Equal(cutoff) {
				kept = append(kept, g)
			}
		}
		entry.groups = kept
		if len(entry.groups) == 0 {
			d.contentFingerprintIndex.Delete(fp)
		}
		entry.mu.Unlock()
		return true
	})

	// After eviction, the second inject must publish.
	trequire.NoError(t, d.InjectChatMessage(ctx, twitch.ID, streamcontrol.ChatListenerAlternate, second))

	_ = drainChatMessages(t, msgs, 2)
	drainNoMore(t, msgs, 100*time.Millisecond)
}

// TestInjectCollapse_Step1ShortCircuitsRepeatedAlternateCall — A "lol";
// B "lol" (collapse); B "lol" again with same key → third call drops
// at Step 1 because injectedEvents.LoadOrStore at Step 1 of the second
// call already stored keyB. The third call is a vanilla repeat that is
// caught by the LoadOrStore on injectedEvents — no Step 2 entry needed.
func TestInjectCollapse_Step1ShortCircuitsRepeatedAlternateCall(t *testing.T) {
	d, ctx, _ := newCollapseTestStreamD(t)
	msgs := subscribeChatMessages(t, ctx, d)

	at := fixedCollapseTime()
	primaryEv := makeCollapseEvent("backref-A", "ping", at)
	alternateEv := makeCollapseEvent("backref-B", "ping", at)

	trequire.NoError(t, d.InjectChatMessage(ctx, twitch.ID, streamcontrol.ChatListenerPrimary, primaryEv))
	trequire.NoError(t, d.InjectChatMessage(ctx, twitch.ID, streamcontrol.ChatListenerAlternate, alternateEv))
	// Third call: same alternate event again. Must drop at Step 1 because
	// the back-reference in injectedEvents already holds keyB.
	trequire.NoError(t, d.InjectChatMessage(ctx, twitch.ID, streamcontrol.ChatListenerAlternate, alternateEv))

	_ = drainChatMessages(t, msgs, 1)
	drainNoMore(t, msgs, 100*time.Millisecond)

	entry := fpEntryForContent(t, d, twitch.ID, "ping", at)
	entry.mu.Lock()
	tassert.Equal(t, 1, len(entry.groups),
		"the third call must not change the groups list")
	tassert.Equal(t, 2, len(entry.groups[0].Sources))
	entry.mu.Unlock()
}

// TestInjectCollapse_ConcurrentSameContent_5DistinctSources — 50 goroutines
// rotating across 5 distinct (platform, listener) sources, all same
// content. Expected: number of publishes is bounded by the cardinality
// of distinct sources times the number of times any single source
// repeats. Use LessOrEqual since the exact count depends on race ordering.
func TestInjectCollapse_ConcurrentSameContent_5DistinctSources(t *testing.T) {
	d, ctx, _ := newCollapseTestStreamD(t)
	msgs := subscribeChatMessages(t, ctx, d)

	at := fixedCollapseTime()
	const goroutines = 50

	type srcSpec struct {
		platID streamcontrol.PlatformName
		lt     streamcontrol.ChatListenerType
	}
	// 5 distinct sources. Mixing platforms here ensures cross-platform
	// events do NOT collapse — only same-platform same-listener-type
	// pairings can chain into the same fp entry. So the bound is 5
	// (distinct sources) * N where N = goroutines/sources.
	specs := []srcSpec{
		{twitch.ID, streamcontrol.ChatListenerPrimary},
		{twitch.ID, streamcontrol.ChatListenerAlternate},
		{kick.ID, streamcontrol.ChatListenerPrimary},
		{kick.ID, streamcontrol.ChatListenerContingency},
		{youtube.ID, streamcontrol.ChatListenerPrimary},
	}

	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range goroutines {
		wg.Add(1)
		i := i
		observability.Go(ctx, func(ctx context.Context) {
			defer wg.Done()
			<-start
			s := specs[i%len(specs)]
			ev := makeCollapseEvent(
				"concurrent-id-"+string(rune('A'+i%26)),
				"race-content",
				at,
			)
			_ = d.InjectChatMessage(ctx, s.platID, s.lt, ev)
		})
	}
	close(start)
	wg.Wait()

	got := drainChatMessagesAvailable(t, msgs, 200*time.Millisecond)
	expectedMax := goroutines // strict upper bound: each call publishes at most once
	tassert.LessOrEqual(t, len(got), expectedMax,
		"publish count must never exceed total injects")
	tassert.GreaterOrEqual(t, len(got), 1,
		"at least one publish must succeed")
}

// TestInjectCollapse_RunnerPropagatesListenerType — streamd-side check
// that listenerType is the discriminator for the collapse decision. The
// runner-mock test (pkg/chathandler/runner_test.go::TestRunner_InjectListenerTypeIsPassedThrough)
// covers the wire path; here we lock in that switching listener types
// across two same-content emissions causes them to collapse onto one
// group rather than two.
func TestInjectCollapse_RunnerPropagatesListenerType(t *testing.T) {
	d, ctx, _ := newCollapseTestStreamD(t)
	msgs := subscribeChatMessages(t, ctx, d)

	at := fixedCollapseTime()
	primaryEv := makeCollapseEvent("propagate-A", "value", at)
	alternateEv := makeCollapseEvent("propagate-B", "value", at)

	trequire.NoError(t, d.InjectChatMessage(ctx, twitch.ID, streamcontrol.ChatListenerPrimary, primaryEv))
	trequire.NoError(t, d.InjectChatMessage(ctx, twitch.ID, streamcontrol.ChatListenerAlternate, alternateEv))

	_ = drainChatMessages(t, msgs, 1)
	drainNoMore(t, msgs, 100*time.Millisecond)
}

// TestInjectCollapse_PrimaryListenerCollapsesUnderInjectChatMessageDirectly
// locks in the streamd-side invariant that two primary-source injects of
// identical content open two distinct source-groups (same-source publishes
// invariant). The gRPC server's empty-listenerType→Primary coercion path
// is covered separately by
// pkg/streamd/server.TestGRPCInjectChatMessage_EmptyListenerType_DefaultsToPrimary.
func TestInjectCollapse_PrimaryListenerCollapsesUnderInjectChatMessageDirectly(t *testing.T) {
	d, ctx, _ := newCollapseTestStreamD(t)
	msgs := subscribeChatMessages(t, ctx, d)

	at := fixedCollapseTime()
	first := makeCollapseEvent("oldclient-A", "legacy", at)
	second := makeCollapseEvent("oldclient-B", "legacy", at)

	trequire.NoError(t, d.InjectChatMessage(ctx, twitch.ID, streamcontrol.ChatListenerPrimary, first))
	trequire.NoError(t, d.InjectChatMessage(ctx, twitch.ID, streamcontrol.ChatListenerPrimary, second))

	_ = drainChatMessages(t, msgs, 2)
	drainNoMore(t, msgs, 100*time.Millisecond)

	entry := fpEntryForContent(t, d, twitch.ID, "legacy", at)
	entry.mu.Lock()
	tassert.Equal(t, 2, len(entry.groups))
	entry.mu.Unlock()
}

// TestInjectCollapse_CleanupEvictsExpiredGroupsAndEmptyEntries —
// pre-populate one fpEntry with one stale + one fresh group; sweep;
// stale gone, entry kept; expire fresh; sweep; fp key deleted.
func TestInjectCollapse_CleanupEvictsExpiredGroupsAndEmptyEntries(t *testing.T) {
	d, _, _ := newCollapseTestStreamD(t)

	now := time.Now()
	stale := &sourceGroup{
		DedupKey: dedupKey{Platform: twitch.ID, Source: dedupKeyFromID, Key: "stale"},
		Sources: map[eventSource]struct{}{
			{Platform: twitch.ID, ListenerType: streamcontrol.ChatListenerPrimary}: {},
		},
		InsertedAt: now.Add(-2 * injectedEventIDTTL),
	}
	fresh := &sourceGroup{
		DedupKey: dedupKey{Platform: twitch.ID, Source: dedupKeyFromID, Key: "fresh"},
		Sources: map[eventSource]struct{}{
			{Platform: twitch.ID, ListenerType: streamcontrol.ChatListenerAlternate}: {},
		},
		InsertedAt: now,
	}
	entry := &fpEntry{groups: []*sourceGroup{stale, fresh}}
	d.contentFingerprintIndex.Store("fp-1", entry)

	// Sweep: stale falls below cutoff, fresh stays.
	cutoff := time.Now().Add(-injectedEventIDTTL)
	d.contentFingerprintIndex.Range(func(fp string, e *fpEntry) bool {
		e.mu.Lock()
		kept := e.groups[:0]
		for _, g := range e.groups {
			if g.InsertedAt.After(cutoff) || g.InsertedAt.Equal(cutoff) {
				kept = append(kept, g)
			}
		}
		e.groups = kept
		if len(e.groups) == 0 {
			d.contentFingerprintIndex.Delete(fp)
		}
		e.mu.Unlock()
		return true
	})

	got, ok := d.contentFingerprintIndex.Load("fp-1")
	trequire.True(t, ok, "fp entry must remain because the fresh group survived")
	got.mu.Lock()
	tassert.Equal(t, 1, len(got.groups), "stale group must be evicted, fresh must survive")
	tassert.Equal(t, dedupKey{Platform: twitch.ID, Source: dedupKeyFromID, Key: "fresh"}, got.groups[0].DedupKey,
		"the surviving group must be the fresh one")
	got.mu.Unlock()

	// Expire the surviving group and sweep again — the entry must be deleted.
	got.mu.Lock()
	got.groups[0].InsertedAt = time.Now().Add(-2 * injectedEventIDTTL)
	got.mu.Unlock()

	cutoff2 := time.Now().Add(-injectedEventIDTTL)
	d.contentFingerprintIndex.Range(func(fp string, e *fpEntry) bool {
		e.mu.Lock()
		kept := e.groups[:0]
		for _, g := range e.groups {
			if g.InsertedAt.After(cutoff2) || g.InsertedAt.Equal(cutoff2) {
				kept = append(kept, g)
			}
		}
		e.groups = kept
		if len(e.groups) == 0 {
			d.contentFingerprintIndex.Delete(fp)
		}
		e.mu.Unlock()
		return true
	})

	_, exists := d.contentFingerprintIndex.Load("fp-1")
	tassert.False(t, exists, "fp entry must be deleted once all groups expire")
}

// TestInjectCollapse_LogReadIsRaceFree — falsifies REJECT-1: the cross-source
// link branch in InjectChatMessage Step 2 must NOT read target.DedupKey or
// len(target.Sources) AFTER releasing entry.mu. Many goroutines drive the
// same fingerprint, each picking one of the 3 listener types; under -race
// any concurrent read+write on target.Sources surfaces a "DATA RACE" report.
//
// Pre-fix this test fails under -race with a write on target.Sources at
// chat.go:211 racing the read at chat.go:215. Post-fix the read must come
// before Unlock — this test must pass cleanly.
func TestInjectCollapse_LogReadIsRaceFree(t *testing.T) {
	d, ctx, _ := newCollapseTestStreamD(t)
	msgs := subscribeChatMessages(t, ctx, d)

	at := fixedCollapseTime()
	listenerTypes := []streamcontrol.ChatListenerType{
		streamcontrol.ChatListenerPrimary,
		streamcontrol.ChatListenerAlternate,
		streamcontrol.ChatListenerContingency,
	}

	const goroutines = 1000
	var wg sync.WaitGroup
	start := make(chan struct{})
	wg.Add(goroutines)
	for i := range goroutines {
		i := i
		observability.Go(ctx, func(ctx context.Context) {
			defer wg.Done()
			<-start
			lt := listenerTypes[i%len(listenerTypes)]
			ev := makeCollapseEvent(
				"race-id-"+string(rune('A'+i%26))+"-"+string(rune('0'+i/26%10)),
				"race-content",
				at,
			)
			_ = d.InjectChatMessage(ctx, twitch.ID, lt, ev)
		})
	}
	close(start)
	wg.Wait()

	// Drain to keep the eventbus from blocking publishers; the count is
	// not the property under test — the race detector is.
	_ = drainChatMessagesAvailable(t, msgs, 50*time.Millisecond)
}

// TestInjectCollapse_CleanupRaceDoesNotOrphan — falsifies CONDITIONAL-1:
// concurrent cleanup of a fpEntry interleaved with InjectChatMessage must
// NOT leave a freshly-installed source-group orphaned in a deleted entry.
// Drives many goroutines that:
//   - inject events (each a fresh content fingerprint based on i),
//   - run a cleanup-style sweep that aggressively expires + deletes.
//
// The contract: after the storm, for every (fp, src) pair that was
// successfully linked to a sourceGroup, the group must be reachable
// either via the live contentFingerprintIndex entry OR have been
// cleanly evicted with no orphans. The race detector under -race
// will flag any write-after-delete or read-after-delete on the same
// fpEntry across goroutines.
//
// We also assert NO PANIC and NO DATA RACE — both the cleanup-side
// `entry.mu.Lock()` and the inject-side use of an already-deleted
// entry must be coherent.
func TestInjectCollapse_CleanupRaceDoesNotOrphan(t *testing.T) {
	d, ctx, _ := newCollapseTestStreamD(t)
	msgs := subscribeChatMessages(t, ctx, d)

	at := fixedCollapseTime()
	const injectGoroutines = 200
	const cleanupGoroutines = 20
	const iterationsPerCleaner = 200

	var wg sync.WaitGroup
	start := make(chan struct{})

	// Inject path.
	wg.Add(injectGoroutines)
	for i := range injectGoroutines {
		i := i
		observability.Go(ctx, func(ctx context.Context) {
			defer wg.Done()
			<-start
			// Use a small fp space so cleanup repeatedly hits the same
			// fpEntries the injectors are still using.
			content := "race-orphan-content-" + string(rune('A'+i%4))
			ev := makeCollapseEvent("orphan-id-"+string(rune('A'+i%26))+"-"+string(rune('0'+i/26%10)), content, at)
			lt := []streamcontrol.ChatListenerType{
				streamcontrol.ChatListenerPrimary,
				streamcontrol.ChatListenerAlternate,
				streamcontrol.ChatListenerContingency,
			}[i%3]
			_ = d.InjectChatMessage(ctx, twitch.ID, lt, ev)
		})
	}

	// Cleanup path: aggressively expire all groups and run the sweep.
	wg.Add(cleanupGoroutines)
	for range cleanupGoroutines {
		observability.Go(ctx, func(_ context.Context) {
			defer wg.Done()
			<-start
			for range iterationsPerCleaner {
				cutoff := time.Now().Add(time.Hour) // expire EVERYTHING
				d.contentFingerprintIndex.Range(func(fp string, entry *fpEntry) bool {
					entry.mu.Lock()
					kept := entry.groups[:0]
					for _, g := range entry.groups {
						if g.InsertedAt.After(cutoff) || g.InsertedAt.Equal(cutoff) {
							kept = append(kept, g)
						}
					}
					entry.groups = kept
					if len(entry.groups) == 0 {
						d.contentFingerprintIndex.Delete(fp)
					}
					entry.mu.Unlock()
					return true
				})
			}
		})
	}

	close(start)
	wg.Wait()
	_ = drainChatMessagesAvailable(t, msgs, 50*time.Millisecond)

	// Sanity: any surviving fpEntry must be a live one (i.e. its current
	// pointer matches what's in the map). We re-read each entry, lock it,
	// and verify the post-condition.
	d.contentFingerprintIndex.Range(func(fp string, entry *fpEntry) bool {
		entry.mu.Lock()
		// Re-validate the entry is still in the map under fp — i.e.,
		// no orphaned mutation persisted. If the cleanup deleted it
		// while we were holding the read view, the map either no
		// longer has fp or has a different pointer; both are OK.
		current, ok := d.contentFingerprintIndex.Load(fp)
		entry.mu.Unlock()
		if !ok {
			return true
		}
		// Live entry: groups slice must be coherent (cardinality ≥ 0,
		// every group must have a non-nil Sources map).
		current.mu.Lock()
		for i, g := range current.groups {
			tassert.NotNil(t, g, "group %d in live entry must not be nil", i)
			tassert.NotNil(t, g.Sources, "group %d Sources map must be initialized", i)
		}
		current.mu.Unlock()
		return true
	})
}

