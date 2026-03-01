package youtube

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/streamctl/pkg/secret"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"golang.org/x/oauth2"
)

func TestParseTimestamp(t *testing.T) {
	type testCase struct {
		input  string
		output time.Time
	}

	for _, testCase := range []testCase{
		{input: "2025-07-09T11:49:53Z", output: time.Date(2025, 7, 9, 11, 49, 53, 0, time.UTC)},
	} {
		t.Run(testCase.input, func(t *testing.T) {
			r, err := ParseTimestamp(testCase.input)
			require.NoError(t, err)
			require.Equal(t, testCase.output, r.UTC())
		})
	}
}

func newTestYouTube(t *testing.T) *YouTube {
	t.Helper()
	SetDebugUseMockClient(true)
	t.Cleanup(func() { SetDebugUseMockClient(false) })

	ctx := context.Background()
	cfg := AccountConfig{
		ClientID:     "test-client-id",
		ClientSecret: secret.New[string]("test-client-secret"),
		Token: ptr(secret.New(oauth2.Token{
			AccessToken: "test-token",
			TokenType:   "Bearer",
			Expiry:      time.Now().Add(time.Hour),
		})),
	}

	yt, err := New(ctx, cfg, func(AccountConfig) error { return nil })
	require.NoError(t, err)
	t.Cleanup(func() { yt.CancelFunc() })
	return yt
}

// TestSetupStreamSequence reproduces the exact call sequence from
// panel.setupStreamNoLock: SetTitle → SetDescription → ApplyProfile →
// SetStreamActive. A regression where ApplyProfile replaces the entire
// plannedStream entry (instead of updating in-place) wipes the Title
// set by the prior SetTitle call, causing startStream to fail with
// "profile title is empty".
func TestSetupStreamSequence(t *testing.T) {
	yt := newTestYouTube(t)
	ctx := context.Background()
	streamID := streamcontrol.StreamID("test-stream")

	profile := StreamProfile{
		StreamProfileBase: streamcontrol.StreamProfileBase{
			Title:       "Profile Default Title",
			Description: "Profile Default Description",
		},
		TemplateBroadcastIDs: []string{"template-1"},
	}

	// Simulate the panel sequence: SetTitle, SetDescription, ApplyProfile.
	// The title/description set here should take precedence over the profile defaults.
	err := yt.SetTitle(ctx, streamID, "My Custom Stream Title")
	require.NoError(t, err)

	err = yt.SetDescription(ctx, streamID, "My Custom Description")
	require.NoError(t, err)

	err = yt.ApplyProfile(ctx, streamID, profile)
	require.NoError(t, err)

	// Verify Title and Description survived ApplyProfile.
	planned, ok := yt.plannedStreams[streamID]
	require.True(t, ok, "planned stream should exist after ApplyProfile")
	require.Equal(t, "My Custom Stream Title", planned.Title,
		"ApplyProfile must not overwrite Title set by SetTitle")
	require.Equal(t, "My Custom Description", planned.Description,
		"ApplyProfile must not overwrite Description set by SetDescription")

	// SetStreamActive(true) should succeed — it calls startStream which
	// validates that planned.Title is non-empty.
	err = yt.SetStreamActive(ctx, streamID, true)
	require.NoError(t, err, "SetStreamActive should succeed when Title was set before ApplyProfile")
}

// TestFetchTemplateDataMissingBroadcast verifies that fetchTemplateData
// returns a descriptive error including the requested and found IDs when
// a template broadcast does not exist.
func TestFetchTemplateDataMissingBroadcast(t *testing.T) {
	yt := newTestYouTube(t)
	ctx := context.Background()

	_, _, err := yt.fetchTemplateData(ctx, []string{"nonexistent-broadcast-id"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "nonexistent-broadcast-id")
	require.Contains(t, err.Error(), "may have been deleted from YouTube")
}

// TestApplyProfileAloneStoresProfile verifies that calling ApplyProfile
// without a prior SetTitle creates a new planned entry with the profile
// (but Title remains empty, so startStream would fail).
func TestApplyProfileAloneStoresProfile(t *testing.T) {
	yt := newTestYouTube(t)
	ctx := context.Background()
	streamID := streamcontrol.StreamID("test-stream-2")

	profile := StreamProfile{
		StreamProfileBase: streamcontrol.StreamProfileBase{
			Title:       "Profile Title",
			Description: "Profile Description",
		},
		TemplateBroadcastIDs: []string{"template-1"},
	}

	err := yt.ApplyProfile(ctx, streamID, profile)
	require.NoError(t, err)

	planned, ok := yt.plannedStreams[streamID]
	require.True(t, ok)
	require.Equal(t, profile, planned.Profile)
	// Title on plannedStream is empty because SetTitle was never called.
	require.Equal(t, "", planned.Title)
}
