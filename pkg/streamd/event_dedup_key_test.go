package streamd

import (
	"context"
	"testing"
	"time"

	tassert "github.com/stretchr/testify/assert"

	// Blank import: registers the YouTube ChatListenerFactory (and with it
	// the EventIDNormalizerProvider) so chathandler.LookupEventIDNormalizer
	// returns a real normalizer for the YouTube tests below. Without this
	// import streamd's own tests would see a nil normalizer because no
	// other init() in the streamd package wires the YouTube factory.
	_ "github.com/xaionaro-go/streamctl/pkg/chathandler/platform/youtube"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/kick"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/twitch"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/youtube"
)

// Two event IDs observed for the same logical YouTube chat message in a real
// run: the Primary listener (gRPC stream) prefixes with "LCC." and wraps the
// inner message ID under protobuf field tag 2; the Contingency listener
// (innertube scrape) wraps the same inner message ID under field tag 1 and
// emits no "LCC." prefix. Both encode the same inner message ID
// "CLvp2K_ukJQDFQfePwQd7KMleQ".
const (
	ytIDPrimary     = "LCC.EhwKGkNMdnAyS191a0pRREZRZmVQd1FkN0tNbGVR"
	ytIDContingency = "ChwKGkNMdnAyS191a0pRREZRZmVQd1FkN0tNbGVR"

	ytIDPrimary2     = "LCC.EhwKGkNNNlVwckh1a0pRREZVcm5Qd1FkR2FVM1Fn"
	ytIDContingency2 = "ChwKGkNNNlVwckh1a0pRREZVcm5Qd1FkR2FVM1Fn"
)

func TestComputeDedupKey_YouTubeCrossListenerMatch(t *testing.T) {
	ctx := context.Background()

	t.Run("same logical message via different listeners yields same key", func(t *testing.T) {
		evP := streamcontrol.Event{ID: ytIDPrimary, Type: streamcontrol.EventTypeChatMessage}
		evC := streamcontrol.Event{ID: ytIDContingency, Type: streamcontrol.EventTypeChatMessage}
		tassert.Equal(t,
			computeDedupKey(ctx, youtube.ID, evP),
			computeDedupKey(ctx, youtube.ID, evC),
		)
	})
	t.Run("different inner ID yields different keys", func(t *testing.T) {
		evA := streamcontrol.Event{ID: ytIDPrimary, Type: streamcontrol.EventTypeChatMessage}
		evB := streamcontrol.Event{ID: ytIDPrimary2, Type: streamcontrol.EventTypeChatMessage}
		tassert.NotEqual(t,
			computeDedupKey(ctx, youtube.ID, evA),
			computeDedupKey(ctx, youtube.ID, evB),
		)
	})
	t.Run("contingency variants of the two inner IDs also stay distinct", func(t *testing.T) {
		evA := streamcontrol.Event{ID: ytIDContingency, Type: streamcontrol.EventTypeChatMessage}
		evB := streamcontrol.Event{ID: ytIDContingency2, Type: streamcontrol.EventTypeChatMessage}
		tassert.NotEqual(t,
			computeDedupKey(ctx, youtube.ID, evA),
			computeDedupKey(ctx, youtube.ID, evB),
		)
	})
}

func TestComputeDedupKey_YouTubeDeletePrefixDistinct(t *testing.T) {
	ctx := context.Background()
	// Deletion events are emitted with a "delete-" prefix already
	// (see pkg/streamcontrol/youtube/chat_listener.go). The dedup gate
	// must treat the original message and its deletion as separate events.
	original := streamcontrol.Event{ID: ytIDPrimary, Type: streamcontrol.EventTypeChatMessage}
	deleted := streamcontrol.Event{ID: "delete-" + ytIDPrimary, Type: streamcontrol.EventTypeChatMessageDeleted}
	tassert.NotEqual(t,
		computeDedupKey(ctx, youtube.ID, original),
		computeDedupKey(ctx, youtube.ID, deleted),
	)
}

func TestComputeDedupKey_EmptyIDFallsBackToFingerprint(t *testing.T) {
	ctx := context.Background()
	at := time.Date(2026, 4, 28, 16, 28, 21, 823_000_000, time.Local)
	mkEv := func(content string) streamcontrol.Event {
		return streamcontrol.Event{
			Type:      streamcontrol.EventTypeFollow,
			User:      streamcontrol.User{ID: "u1", Slug: "u1", Name: "u1"},
			CreatedAt: at,
			Message:   &streamcontrol.Message{Content: content},
		}
	}

	t.Run("two empty-ID events with different content do not collide", func(t *testing.T) {
		a := mkEv("hello")
		b := mkEv("world")
		tassert.NotEqual(t,
			computeDedupKey(ctx, kick.ID, a),
			computeDedupKey(ctx, kick.ID, b),
		)
	})

	t.Run("two empty-ID events with identical fingerprint inputs collapse", func(t *testing.T) {
		a := mkEv("same")
		b := mkEv("same")
		tassert.Equal(t,
			computeDedupKey(ctx, kick.ID, a),
			computeDedupKey(ctx, kick.ID, b),
		)
	})
}

func TestComputeDedupKey_FingerprintBucketsTimestamp(t *testing.T) {
	ctx := context.Background()
	// Two empty-ID events drift apart by less than the dedup TTL bucket
	// must collapse onto the same fingerprint key — that is precisely
	// what the bucket exists for: cross-listener clock drift inside the
	// dedup window cannot produce two distinct keys for the same logical
	// event.
	base := time.Date(2026, 4, 28, 16, 28, 21, 823_000_000, time.UTC).Truncate(injectedEventIDTTL)
	mk := func(at time.Time) streamcontrol.Event {
		return streamcontrol.Event{
			Type:      streamcontrol.EventTypeFollow,
			User:      streamcontrol.User{ID: "u1"},
			CreatedAt: at,
			Message:   &streamcontrol.Message{Content: "same"},
		}
	}
	a := mk(base.Add(10 * time.Second))
	b := mk(base.Add(injectedEventIDTTL - time.Second))
	tassert.Equal(t,
		computeDedupKey(ctx, kick.ID, a),
		computeDedupKey(ctx, kick.ID, b),
	)

	// Crossing into the next bucket does produce a distinct key — the
	// next dedup window is independent.
	c := mk(base.Add(injectedEventIDTTL + time.Second))
	tassert.NotEqual(t,
		computeDedupKey(ctx, kick.ID, a),
		computeDedupKey(ctx, kick.ID, c),
	)
}

func TestComputeDedupKey_PlatformNamespaced(t *testing.T) {
	ctx := context.Background()
	ev := streamcontrol.Event{ID: "abc", Type: streamcontrol.EventTypeChatMessage}
	tassert.NotEqual(t,
		computeDedupKey(ctx, twitch.ID, ev),
		computeDedupKey(ctx, kick.ID, ev),
	)
}

func TestComputeDedupKey_TwitchAndKickIdentityNormalize(t *testing.T) {
	ctx := context.Background()
	// Twitch and Kick keep the raw ID (no platform-specific normalization
	// registered). Two different IDs must yield different keys.
	a := streamcontrol.Event{ID: "tw-abc", Type: streamcontrol.EventTypeChatMessage}
	b := streamcontrol.Event{ID: "tw-def", Type: streamcontrol.EventTypeChatMessage}
	tassert.NotEqual(t,
		computeDedupKey(ctx, twitch.ID, a),
		computeDedupKey(ctx, twitch.ID, b),
	)
	// And a single ID stays stable across the two calls.
	tassert.Equal(t,
		computeDedupKey(ctx, twitch.ID, a),
		computeDedupKey(ctx, twitch.ID, a),
	)
}
