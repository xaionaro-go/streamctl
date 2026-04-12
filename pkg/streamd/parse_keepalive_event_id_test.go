package streamd

import (
	"fmt"
	"testing"

	tassert "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
)

func TestParseKeepaliveEventID_Valid(t *testing.T) {
	eventID := streamcontrol.EventID("keepalive-primary-twitch-123")
	platID := streamcontrol.PlatformName("twitch")

	key, ok := parseKeepaliveEventID(eventID, platID)

	require.True(t, ok, "valid keepalive event ID must parse successfully")
	tassert.Equal(t, platID, key.Platform)
	tassert.Equal(t, streamcontrol.ChatListenerPrimary, key.ListenerType)
}

func TestParseKeepaliveEventID_NotKeepalive(t *testing.T) {
	cases := []streamcontrol.EventID{
		"msg-12345",
		"greeting-twitch-user-1234",
		"diag-youtube-9999",
		"",
	}

	for _, id := range cases {
		t.Run(string(id), func(t *testing.T) {
			_, ok := parseKeepaliveEventID(id, "twitch")
			tassert.False(t, ok, "non-keepalive event ID %q must not parse", id)
		})
	}
}

func TestParseKeepaliveEventID_MalformedKeepalive(t *testing.T) {
	cases := []struct {
		name string
		id   streamcontrol.EventID
	}{
		{name: "no_dash_after_prefix", id: "keepalive"},
		{name: "prefix_only_with_dash", id: "keepalive-"},
		{name: "unknown_type", id: "keepalive-bogus-twitch-123"},
		{name: "no_segments_after_type", id: "keepalive-primary"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, ok := parseKeepaliveEventID(tc.id, "twitch")
			tassert.False(t, ok, "malformed keepalive %q must not parse", tc.id)
		})
	}
}

func TestParseKeepaliveEventID_AllTypes(t *testing.T) {
	allTypes := []streamcontrol.ChatListenerType{
		streamcontrol.ChatListenerPrimary,
		streamcontrol.ChatListenerAlternate,
		streamcontrol.ChatListenerContingency,
		streamcontrol.ChatListenerEmergency,
	}

	platID := streamcontrol.PlatformName("youtube")

	for _, lt := range allTypes {
		t.Run(lt.String(), func(t *testing.T) {
			eventID := streamcontrol.EventID(
				fmt.Sprintf("keepalive-%s-%s-%d", lt.String(), platID, 1234567890),
			)
			key, ok := parseKeepaliveEventID(eventID, platID)
			require.True(t, ok, "keepalive with type %q must parse", lt.String())
			tassert.Equal(t, lt, key.ListenerType)
			tassert.Equal(t, platID, key.Platform)
		})
	}
}

// TestParseKeepaliveEventID_DualSided confirms keepalive IDs ARE parsed
// and non-keepalive IDs are NOT parsed.
func TestParseKeepaliveEventID_DualSided(t *testing.T) {
	platID := streamcontrol.PlatformName("twitch")

	t.Run("keepalive_IS_parsed", func(t *testing.T) {
		id := streamcontrol.EventID("keepalive-primary-twitch-999")
		key, ok := parseKeepaliveEventID(id, platID)
		tassert.True(t, ok, "keepalive event ID must be recognized")
		tassert.Equal(t, streamcontrol.ChatListenerPrimary, key.ListenerType)
	})

	t.Run("non_keepalive_is_NOT_parsed", func(t *testing.T) {
		id := streamcontrol.EventID("chat-message-twitch-999")
		_, ok := parseKeepaliveEventID(id, platID)
		tassert.False(t, ok, "non-keepalive event ID must not be recognized")
	})
}
