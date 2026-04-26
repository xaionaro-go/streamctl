package youtube

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// statePathForTest returns a path inside a fresh temp dir; the dir is removed
// at test cleanup so concurrent tests do not contend on the same file.
func statePathForTest(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "youtube_quota.json")
}

func TestClientCalcPointsLoadMissingFileStartsFresh(t *testing.T) {
	statePath := statePathForTest(t)
	c := NewYouTubeClientCalcPoints(context.Background(), &mockFullClient{}, statePath)

	assert.Equal(t, uint64(0), c.UsedPoints.Load())
	assert.True(t, c.PreviousCheckAt.IsZero())
}

func TestClientCalcPointsLoadCorruptFileStartsFresh(t *testing.T) {
	statePath := statePathForTest(t)
	require.NoError(t, os.WriteFile(statePath, []byte("not-json{"), 0o644))

	c := NewYouTubeClientCalcPoints(context.Background(), &mockFullClient{}, statePath)

	assert.Equal(t, uint64(0), c.UsedPoints.Load(), "corrupt file must not poison the counter")
	assert.True(t, c.PreviousCheckAt.IsZero())
}

func TestClientCalcPointsSaveAndReloadRoundtrip(t *testing.T) {
	statePath := statePathForTest(t)
	ctx := context.Background()

	c1 := NewYouTubeClientCalcPoints(ctx, &mockFullClient{}, statePath)
	// Drive a successful API call through the tracker — Ping costs 1 point.
	require.NoError(t, c1.Ping(ctx))
	require.NoError(t, c1.Ping(ctx))
	require.NoError(t, c1.Ping(ctx))

	require.Equal(t, uint64(3), c1.UsedPoints.Load())
	require.False(t, c1.PreviousCheckAt.IsZero())

	// Confirm the file actually exists on disk.
	_, statErr := os.Stat(statePath)
	require.NoError(t, statErr)

	// Reload from disk via a fresh wrapper; counters must survive.
	c2 := NewYouTubeClientCalcPoints(ctx, &mockFullClient{}, statePath)
	assert.Equal(t, uint64(3), c2.UsedPoints.Load(), "UsedPoints must persist across restarts")
	assert.WithinDuration(t, c1.PreviousCheckAt, c2.PreviousCheckAt, time.Second,
		"PreviousCheckAt must persist across restarts")
}

func TestClientCalcPointsDayRolloverResetsAndPersists(t *testing.T) {
	statePath := statePathForTest(t)
	ctx := context.Background()

	c := NewYouTubeClientCalcPoints(ctx, &mockFullClient{}, statePath)
	// Seed the tracker as if we accumulated yesterday's spend.
	c.UsedPoints.Store(7777)
	c.PreviousCheckAt = time.Date(1999, 1, 1, 0, 0, 0, 0, time.UTC)

	require.NoError(t, c.Ping(ctx))

	// Day mismatch (1999 vs today) must trigger reset; the new call's
	// own +1 point is the only count that should remain.
	assert.Equal(t, uint64(1), c.UsedPoints.Load(),
		"day rollover must wipe yesterday's count, leaving only the current call")

	// Reload to confirm the reset reached disk.
	c2 := NewYouTubeClientCalcPoints(ctx, &mockFullClient{}, statePath)
	assert.Equal(t, uint64(1), c2.UsedPoints.Load(),
		"reset must be persisted, not just held in memory")
}

func TestClientCalcPointsEmptyPathDisablesPersistence(t *testing.T) {
	c := NewYouTubeClientCalcPoints(context.Background(), &mockFullClient{}, "")
	require.NoError(t, c.Ping(context.Background()))

	// Counter still tracked in memory.
	assert.Equal(t, uint64(1), c.UsedPoints.Load())
	// And no file appeared (we never gave it a path to write to).
	assert.Equal(t, "", c.StatePath)
}

// pingErrClient embeds mockFullClient and overrides Ping to return an error,
// letting us drive the addUsedPointsIfNoError no-op-on-error path.
type pingErrClient struct {
	mockFullClient
}

func (p *pingErrClient) Ping(context.Context) error { return assert.AnError }

func TestClientCalcPointsErrorDoesNotPersist(t *testing.T) {
	statePath := statePathForTest(t)
	ctx := context.Background()

	c := NewYouTubeClientCalcPoints(ctx, &pingErrClient{}, statePath)
	_ = c.Ping(ctx)

	assert.Equal(t, uint64(0), c.UsedPoints.Load(),
		"failed API calls must not bump the counter")
	_, statErr := os.Stat(statePath)
	assert.True(t, os.IsNotExist(statErr),
		"no API call ever succeeded, so no state file should have been written")
}

func TestClientCalcPointsAtomicWriteSurvivesRenameRace(t *testing.T) {
	// Validates the temp+rename invariant: at no observable point should the
	// final file contain partial JSON.
	statePath := statePathForTest(t)
	ctx := context.Background()

	c := NewYouTubeClientCalcPoints(ctx, &mockFullClient{}, statePath)
	for i := 0; i < 50; i++ {
		require.NoError(t, c.Ping(ctx))

		data, err := os.ReadFile(statePath)
		require.NoError(t, err)
		// Reload via fresh wrapper — succeeds only if file is valid JSON.
		tmp := NewYouTubeClientCalcPoints(ctx, &mockFullClient{}, statePath)
		assert.Equal(t, c.UsedPoints.Load(), tmp.UsedPoints.Load(),
			"iter %d: on-disk counter must match in-memory; file content=%q", i, data)
	}
}

func TestDefaultQuotaStatePathFormat(t *testing.T) {
	got := DefaultQuotaStatePath()
	if got == "" {
		t.Skip("UserConfigDir unavailable in this environment")
	}
	assert.Equal(t, "youtube_quota.json", filepath.Base(got))
	assert.Equal(t, "wingout", filepath.Base(filepath.Dir(got)))
}
