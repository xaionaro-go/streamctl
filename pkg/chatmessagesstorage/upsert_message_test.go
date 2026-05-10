package chatmessagesstorage

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamd/api"
)

// mkMsg builds a minimal ChatMessage with the given identifying fields plus a
// content string used by tests to assert which message variant survived an
// upsert. CreatedAt is set so the storage's append/sort path can keep the
// invariant intact.
func mkMsg(
	id streamcontrol.EventID,
	platform streamcontrol.PlatformName,
	content string,
	createdAt time.Time,
) api.ChatMessage {
	return api.ChatMessage{
		Event: streamcontrol.Event{
			ID:        id,
			CreatedAt: createdAt,
			Message: &streamcontrol.Message{
				Content: content,
			},
		},
		Platform: platform,
		IsLive:   true,
	}
}

// TestUpsertMessage_ReplacesExisting locks in the primary contract: an upsert
// for an (ID, Platform) already in storage MUST replace the existing entry
// in place — not append a duplicate. This is the path the translation worker
// takes after publishing the raw message; without replacement the UI would
// show two rows.
func TestUpsertMessage_ReplacesExisting(t *testing.T) {
	s := New("")
	ctx := context.Background()

	now := time.Now()
	original := mkMsg("evt-1", "twitch", "hola", now)
	require.NoError(t, s.AddMessage(ctx, original))
	require.Len(t, s.Messages, 1, "precondition: storage has the original")

	updated := mkMsg("evt-1", "twitch", "hola -文A-> hello", now)
	require.NoError(t, s.UpsertMessage(ctx, updated))

	assert.Len(t, s.Messages, 1, "upsert must NOT add a second entry for the same (ID, Platform)")
	assert.Equal(t, "hola -文A-> hello", s.Messages[0].Message.Content,
		"upsert must replace content with the new message")
	assert.False(t, s.Messages[0].IsLive,
		"upsert must clear IsLive (mirrors AddMessage so storage stays archive-shaped)")
	assert.True(t, s.IsChanged, "upsert must mark storage dirty so the next Store flushes it")
}

// TestUpsertMessage_PlatformDisambiguates locks in that the (ID, Platform)
// tuple is the identity, not ID alone: the same bare ID on two platforms is
// two distinct messages and an upsert against one must not touch the other.
func TestUpsertMessage_PlatformDisambiguates(t *testing.T) {
	s := New("")
	ctx := context.Background()

	now := time.Now()
	twitchMsg := mkMsg("evt-1", "twitch", "twitch-content", now)
	youtubeMsg := mkMsg("evt-1", "youtube", "youtube-content", now)
	require.NoError(t, s.AddMessage(ctx, twitchMsg))
	require.NoError(t, s.AddMessage(ctx, youtubeMsg))
	require.Len(t, s.Messages, 2)

	upsert := mkMsg("evt-1", "twitch", "twitch-translated", now)
	require.NoError(t, s.UpsertMessage(ctx, upsert))

	assert.Len(t, s.Messages, 2, "different platforms must remain distinct entries")

	twitchIdx, youtubeIdx := -1, -1
	for i, m := range s.Messages {
		if m.Platform == "twitch" {
			twitchIdx = i
		}
		if m.Platform == "youtube" {
			youtubeIdx = i
		}
	}
	require.NotEqual(t, -1, twitchIdx, "twitch entry must still exist")
	require.NotEqual(t, -1, youtubeIdx, "youtube entry must still exist")
	assert.Equal(t, "twitch-translated", s.Messages[twitchIdx].Message.Content,
		"twitch entry must be replaced")
	assert.Equal(t, "youtube-content", s.Messages[youtubeIdx].Message.Content,
		"youtube entry must NOT be touched")
}

// TestUpsertMessage_NotFoundFallsBackToAdd locks in the fallback contract:
// when no entry matches (ID, Platform), upsert appends. This is what makes
// it safe to call upsert eagerly from the translation worker without a
// separate "exists?" check.
func TestUpsertMessage_NotFoundFallsBackToAdd(t *testing.T) {
	s := New("")
	ctx := context.Background()

	now := time.Now()
	require.NoError(t, s.UpsertMessage(ctx, mkMsg("evt-new", "twitch", "first sighting", now)))

	require.Len(t, s.Messages, 1, "upsert with no match must append a new entry")
	assert.Equal(t, "first sighting", s.Messages[0].Message.Content)
	assert.False(t, s.Messages[0].IsLive,
		"appended entry must follow AddMessage's IsLive=false invariant")
	assert.True(t, s.IsChanged)
}
