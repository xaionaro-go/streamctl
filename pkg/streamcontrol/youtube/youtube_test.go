package youtube

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/streamctl/pkg/secret"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	yttypes "github.com/xaionaro-go/streamctl/pkg/streamcontrol/youtube/types"
	"golang.org/x/oauth2"
	youtube "google.golang.org/api/youtube/v3"
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

func newTestAccountConfig() AccountConfig {
	return AccountConfig{
		ClientID:     "test-client-id",
		ClientSecret: secret.New("test-client-secret"),
		Token: ptr(secret.New(oauth2.Token{
			AccessToken: "test-token",
			TokenType:   "Bearer",
			Expiry:      time.Now().Add(time.Hour),
		})),
	}
}

func newTestYouTubeWithConfig(
	t *testing.T,
	cfg AccountConfig,
	saveCfgFn func(AccountConfig) error,
) *YouTube {
	t.Helper()
	SetDebugUseMockClient(true)
	t.Cleanup(func() { SetDebugUseMockClient(false) })

	ctx := context.Background()
	yt, err := New(ctx, cfg, saveCfgFn)
	require.NoError(t, err)
	t.Cleanup(func() { yt.CancelFunc() })
	return yt
}

func newTestYouTube(t *testing.T) *YouTube {
	t.Helper()
	return newTestYouTubeWithConfig(t, newTestAccountConfig(), func(AccountConfig) error { return nil })
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

func TestGetInfoReturnsQuotaAndListeners(t *testing.T) {
	yt := newTestYouTube(t)
	ctx := context.Background()

	// Verify baseline: fresh YouTube instance should have zero quota, no listeners, no broadcasts.
	info := yt.GetInfo(ctx)
	assert.Equal(t, uint64(0), info.QuotaUsage.UsedPoints.Load())
	assert.Equal(t, uint64(yttypes.YouTubeDailyQuotaLimit), info.QuotaUsage.DailyLimit)
	assert.False(t, info.QuotaUsage.ResetTime.IsZero())
	assert.Empty(t, info.ChatListeners)
	assert.Empty(t, info.ActiveBroadcasts)

	// Simulate some quota usage via per-op tracking.
	yt.YouTubeClient.UsedPointsByOp.Store("GetBroadcasts", 42)
	yt.YouTubeClient.UsedPoints.Store(42)
	info = yt.GetInfo(ctx)
	assert.Equal(t, uint64(42), info.QuotaUsage.UsedPoints.Load())

	// Simulate active broadcasts.
	yt.currentLiveBroadcasts = []*youtube.LiveBroadcast{
		{
			Id: "bc-1",
			Snippet: &youtube.LiveBroadcastSnippet{
				Title:           "Test Stream",
				ActualStartTime: "2025-07-09T11:49:53Z",
			},
			Statistics: &youtube.LiveBroadcastStatistics{
				ConcurrentViewers: 123,
			},
		},
	}
	info = yt.GetInfo(ctx)
	require.Len(t, info.ActiveBroadcasts, 1)
	assert.Equal(t, "bc-1", info.ActiveBroadcasts[0].ID)
	assert.Equal(t, "Test Stream", info.ActiveBroadcasts[0].Title)
	assert.Equal(t, "active", info.ActiveBroadcasts[0].Status)
	assert.Equal(t, uint64(123), info.ActiveBroadcasts[0].ViewerCount)
	assert.False(t, info.ActiveBroadcasts[0].ActualStart.IsZero())
}

func TestTemplateBroadcastIDSet(t *testing.T) {
	yt := newTestYouTube(t)

	// No profiles configured: empty set.
	ids := yt.TemplateBroadcastIDSet()
	assert.Empty(t, ids)

	// Configure profiles with template broadcast IDs.
	yt.Config.StreamProfiles = map[streamcontrol.StreamID]streamcontrol.StreamProfiles[StreamProfile]{
		"stream-1": {
			"profile-a": StreamProfile{
				TemplateBroadcastIDs: []string{"tmpl-1", "tmpl-2"},
			},
		},
		"stream-2": {
			"profile-b": StreamProfile{
				TemplateBroadcastIDs: []string{"tmpl-2", "tmpl-3"},
			},
		},
	}
	ids = yt.TemplateBroadcastIDSet()
	assert.Len(t, ids, 3)
	assert.Contains(t, ids, "tmpl-1")
	assert.Contains(t, ids, "tmpl-2")
	assert.Contains(t, ids, "tmpl-3")
}

func TestQuotaPersistenceLoad(t *testing.T) {
	cfg := newTestAccountConfig()
	cfg.QuotaUsedByOp = map[string]uint64{
		"GetBroadcasts": 200,
		"Search":        300,
	}
	cfg.QuotaUsedDate = getQuotaCutoffDate(time.Now())

	yt := newTestYouTubeWithConfig(t, cfg, func(AccountConfig) error { return nil })

	// 500 loaded from per-op map + 1 from Ping during New()
	assert.Equal(t, uint64(501), yt.YouTubeClient.UsedPoints.Load())
}

func TestQuotaPersistenceLoadPerOp(t *testing.T) {
	cfg := newTestAccountConfig()
	cfg.QuotaUsedDate = getQuotaCutoffDate(time.Now())
	cfg.QuotaUsedByOp = map[string]uint64{
		"GetBroadcasts": 3,
		"Search":        100,
		"UpdateVideo":   97,
	}

	yt := newTestYouTubeWithConfig(t, cfg, func(AccountConfig) error { return nil })

	// Per-op data should be restored.
	getBc, ok := yt.YouTubeClient.UsedPointsByOp.Load("GetBroadcasts")
	assert.True(t, ok)
	assert.Equal(t, uint64(3), getBc)

	search, ok := yt.YouTubeClient.UsedPointsByOp.Load("Search")
	assert.True(t, ok)
	assert.Equal(t, uint64(100), search)

	updVid, ok := yt.YouTubeClient.UsedPointsByOp.Load("UpdateVideo")
	assert.True(t, ok)
	assert.Equal(t, uint64(97), updVid)

	// Total should be derived from per-op sum + 1 from Ping during New()
	assert.Equal(t, uint64(201), yt.YouTubeClient.UsedPoints.Load())
}

func TestQuotaPersistenceLoadStaleDate(t *testing.T) {
	cfg := newTestAccountConfig()
	cfg.QuotaUsedByOp = map[string]uint64{
		"GetBroadcasts": 200,
		"Search":        300,
	}
	cfg.QuotaUsedDate = "2020-01-01"

	yt := newTestYouTubeWithConfig(t, cfg, func(AccountConfig) error { return nil })

	// Stale date: persisted quota is not loaded; Ping adds 1 but date
	// mismatch in addUsedPointsIfNoError resets to 0.
	assert.Equal(t, uint64(0), yt.YouTubeClient.UsedPoints.Load())
}

func TestPersistQuotaSavesOnChange(t *testing.T) {
	cfg := newTestAccountConfig()

	var savedCfg AccountConfig
	saveCount := 0
	saveFn := func(c AccountConfig) error {
		savedCfg = c
		saveCount++
		return nil
	}

	yt := newTestYouTubeWithConfig(t, cfg, saveFn)
	ctx := context.Background()

	// After New(), Ping added 1 point. First explicit persist should save.
	saveCount = 0
	yt.persistQuota(ctx)
	assert.Equal(t, 1, saveCount)
	assert.Equal(t, map[string]uint64{"Ping": 1}, savedCfg.QuotaUsedByOp)
	assert.Equal(t, getQuotaCutoffDate(time.Now()), savedCfg.QuotaUsedDate)

	// Second persist with same value should not save again.
	yt.persistQuota(ctx)
	assert.Equal(t, 1, saveCount, "persistQuota should skip save when values unchanged")

	// Add more per-op usage; persist should save again.
	yt.YouTubeClient.UsedPointsByOp.Store("GetBroadcasts", 5)
	yt.YouTubeClient.UsedPointsByOp.Store("Search", 95)
	yt.persistQuota(ctx)
	assert.Equal(t, 2, saveCount)
	assert.Equal(t, map[string]uint64{
		"GetBroadcasts": 5,
		"Ping":          1,
		"Search":        95,
	}, savedCfg.QuotaUsedByOp)
}
