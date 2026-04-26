package youtube

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
	_ "time/tzdata"

	"github.com/facebookincubator/go-belt/tool/logger"
	"google.golang.org/api/youtube/v3"
)

var tzLosAngeles *time.Location

func init() {
	var err error
	tzLosAngeles, err = time.LoadLocation("America/Los_Angeles")
	if err != nil {
		fmt.Fprintf(os.Stderr, "unable to get the timezone of Los_Angeles")
		tzLosAngeles = time.FixedZone("America/Los_Angeles", -7*3600)
	}
}

// quotaStateFileName is the filename written under the user's config dir
// (e.g. ~/.config/wingout/) to persist YouTube Data API quota counters
// across process restarts. The directory itself is shared with other
// streamctl/streampanel state files; do not put YAML config here.
const quotaStateFileName = "youtube_quota.json"

// DefaultQuotaStatePath resolves to <UserConfigDir>/wingout/<quotaStateFileName>.
// On Linux this is ~/.config/wingout/youtube_quota.json. Returns an empty string
// (not an error) when the user config dir cannot be resolved — callers treat
// "" as "do not persist", so a missing HOME does not crash the controller.
func DefaultQuotaStatePath() string {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(configDir, "wingout", quotaStateFileName)
}

// quotaState is the on-disk shape. JSON keys are stable; rename = data loss.
type quotaState struct {
	UsedPoints      uint64    `json:"used_points"`
	PreviousCheckAt time.Time `json:"previous_check_at"`
}

// see also: https://developers.google.com/youtube/v3/determine_quota_cost
type ClientCalcPoints struct {
	Client          client
	UsedPoints      atomic.Uint64
	CheckMutex      sync.Mutex
	PreviousCheckAt time.Time
	StatePath       string
}

var _ client = (*ClientCalcPoints)(nil)

// NewYouTubeClientCalcPoints constructs a quota-tracking wrapper. When statePath
// is non-empty, prior counters are loaded from disk and every successful API
// call persists the new counter back to that file (atomic temp+rename). Pass
// "" to disable persistence (test-only).
func NewYouTubeClientCalcPoints(
	ctx context.Context,
	client client,
	statePath string,
) *ClientCalcPoints {
	c := &ClientCalcPoints{
		Client:    client,
		StatePath: statePath,
	}
	if statePath == "" {
		return c
	}
	switch err := c.loadStateLocked(); {
	case err == nil:
		logger.Infof(ctx, "loaded YouTube quota state from '%s': used=%d previous_check_at=%s",
			statePath, c.UsedPoints.Load(), c.PreviousCheckAt.Format(time.RFC3339))
	case errors.Is(err, os.ErrNotExist):
		logger.Debugf(ctx, "YouTube quota state file '%s' does not exist yet; starting fresh", statePath)
	default:
		logger.Warnf(ctx, "unable to load YouTube quota state from '%s' (starting fresh): %v", statePath, err)
	}
	return c
}

// loadStateLocked reads the on-disk counter into the receiver. Caller need not
// hold CheckMutex — this runs before the receiver escapes the constructor and
// no goroutine can observe it concurrently.
func (c *ClientCalcPoints) loadStateLocked() error {
	data, err := os.ReadFile(c.StatePath)
	if err != nil {
		return err
	}
	var s quotaState
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("decode quota state: %w", err)
	}
	c.UsedPoints.Store(s.UsedPoints)
	c.PreviousCheckAt = s.PreviousCheckAt
	return nil
}

// saveStateLocked writes the current counter atomically (temp file + rename).
// Caller MUST hold CheckMutex. Creates the parent directory on first use.
func (c *ClientCalcPoints) saveStateLocked() error {
	if c.StatePath == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(c.StatePath), 0o755); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}
	data, err := json.Marshal(quotaState{
		UsedPoints:      c.UsedPoints.Load(),
		PreviousCheckAt: c.PreviousCheckAt,
	})
	if err != nil {
		return fmt.Errorf("encode quota state: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(c.StatePath), filepath.Base(c.StatePath)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp state file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() {
		// Best-effort cleanup if rename never succeeds.
		_ = os.Remove(tmpName)
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp state file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp state file: %w", err)
	}
	if err := os.Rename(tmpName, c.StatePath); err != nil {
		return fmt.Errorf("rename temp state file: %w", err)
	}
	return nil
}

func getQuotaCutoffDate(t time.Time) string {
	return t.In(tzLosAngeles).Format("2006-01-02")
}

func (c *ClientCalcPoints) addUsedPointsIfNoError(
	ctx context.Context,
	points uint,
	err error,
) {
	if err != nil {
		return
	}
	c.CheckMutex.Lock()
	defer c.CheckMutex.Unlock()

	// Rollover check must run BEFORE Add: otherwise the very call that
	// crosses the day boundary loses its own point to the subsequent Store(0).
	now := time.Now()
	curDate := getQuotaCutoffDate(now)
	prevDate := getQuotaCutoffDate(c.PreviousCheckAt)
	if curDate != prevDate {
		if !c.PreviousCheckAt.IsZero() {
			// Suppress the misleading 'XXXX-XX-XX != 0000-12-31' line that fires
			// on every cold start; treat fresh state as "no previous day yet".
			logger.Infof(ctx, "new quota day in YouTube: '%s' != '%s'", curDate, prevDate)
		}
		c.UsedPoints.Store(0)
	}
	c.PreviousCheckAt = now
	v := c.UsedPoints.Add(uint64(points))
	if v > 5000 {
		logger.Warnf(ctx, "now %d points were used", v)
	} else {
		logger.Tracef(ctx, "now %d points were used", v)
	}
	if err := c.saveStateLocked(); err != nil {
		logger.Warnf(ctx, "unable to persist YouTube quota state to '%s': %v", c.StatePath, err)
	}
}

func (c *ClientCalcPoints) Ping(ctx context.Context) (_err error) {
	defer func() { c.addUsedPointsIfNoError(ctx, 1, _err) }()
	return c.Client.Ping(ctx)
}

func (c *ClientCalcPoints) GetBroadcasts(
	ctx context.Context,
	t BroadcastType,
	ids []string,
	parts []string,
	pageToken string,
) (_ret *youtube.LiveBroadcastListResponse, _err error) {
	defer func() { c.addUsedPointsIfNoError(ctx, 1, _err) }()
	return c.Client.GetBroadcasts(ctx, t, ids, parts, pageToken)
}

func (c *ClientCalcPoints) UpdateBroadcast(
	ctx context.Context,
	broadcast *youtube.LiveBroadcast,
	parts []string,
) (_err error) {
	defer func() { c.addUsedPointsIfNoError(ctx, 50, _err) }()
	return c.Client.UpdateBroadcast(ctx, broadcast, parts)
}

func (c *ClientCalcPoints) InsertBroadcast(
	ctx context.Context,
	broadcast *youtube.LiveBroadcast,
	parts []string,
) (_ret *youtube.LiveBroadcast, _err error) {
	defer func() { c.addUsedPointsIfNoError(ctx, 50, _err) }()
	return c.Client.InsertBroadcast(ctx, broadcast, parts)
}

func (c *ClientCalcPoints) DeleteBroadcast(
	ctx context.Context,
	broadcastID string,
) (_err error) {
	defer func() { c.addUsedPointsIfNoError(ctx, 50, _err) }()
	return c.Client.DeleteBroadcast(ctx, broadcastID)
}

func (c *ClientCalcPoints) GetStreams(
	ctx context.Context,
	parts []string,
) (_ret *youtube.LiveStreamListResponse, _err error) {
	defer func() { c.addUsedPointsIfNoError(ctx, 1, _err) }()
	return c.Client.GetStreams(ctx, parts)
}

func (c *ClientCalcPoints) GetVideos(
	ctx context.Context,
	broadcastIDs []string,
	parts []string,
) (_ret *youtube.VideoListResponse, _err error) {
	defer func() { c.addUsedPointsIfNoError(ctx, 1, _err) }()
	return c.Client.GetVideos(ctx, broadcastIDs, parts)
}

func (c *ClientCalcPoints) UpdateVideo(
	ctx context.Context,
	video *youtube.Video,
	parts []string,
) (_err error) {
	defer func() { c.addUsedPointsIfNoError(ctx, 1, _err) }()
	return c.Client.UpdateVideo(ctx, video, parts)
}

func (c *ClientCalcPoints) InsertCuepoint(
	ctx context.Context,
	cuepoint *youtube.Cuepoint,
) (_err error) {
	defer func() { c.addUsedPointsIfNoError(ctx, 50, _err) }()
	return c.Client.InsertCuepoint(ctx, cuepoint)
}

func (c *ClientCalcPoints) GetPlaylists(
	ctx context.Context,
	playlistParts []string,
) (_ret *youtube.PlaylistListResponse, _err error) {
	defer func() { c.addUsedPointsIfNoError(ctx, 1, _err) }()
	return c.Client.GetPlaylists(ctx, playlistParts)
}

func (c *ClientCalcPoints) GetPlaylistItems(
	ctx context.Context,
	playlistID string,
	videoID string,
	parts []string,
) (_ret *youtube.PlaylistItemListResponse, _err error) {
	defer func() { c.addUsedPointsIfNoError(ctx, 50, _err) }()
	return c.Client.GetPlaylistItems(ctx, playlistID, videoID, parts)
}

func (c *ClientCalcPoints) InsertPlaylistItem(
	ctx context.Context,
	item *youtube.PlaylistItem,
	parts []string,
) (_err error) {
	defer func() { c.addUsedPointsIfNoError(ctx, 50, _err) }()
	return c.Client.InsertPlaylistItem(ctx, item, parts)
}

func (c *ClientCalcPoints) SetThumbnail(
	ctx context.Context,
	broadcastID string,
	thumbnail io.Reader,
) (_err error) {
	defer func() { c.addUsedPointsIfNoError(ctx, 50, _err) }()
	return c.Client.SetThumbnail(ctx, broadcastID, thumbnail)
}

func (c *ClientCalcPoints) InsertCommentThread(
	ctx context.Context,
	t *youtube.CommentThread,
	parts []string,
) (_err error) {
	defer func() { c.addUsedPointsIfNoError(ctx, 50, _err) }()
	return c.Client.InsertCommentThread(ctx, t, parts)
}

func (c *ClientCalcPoints) ListChatMessages(
	ctx context.Context,
	chatID string,
	parts []string,
) (_ret *youtube.LiveChatMessageListResponse, _err error) {
	defer func() { c.addUsedPointsIfNoError(ctx, 1, _err) }()
	return c.Client.ListChatMessages(ctx, chatID, parts)
}

func (c *ClientCalcPoints) DeleteChatMessage(
	ctx context.Context,
	messageID string,
) (_err error) {
	defer func() { c.addUsedPointsIfNoError(ctx, 1, _err) }()
	return c.Client.DeleteChatMessage(ctx, messageID)
}

func (c *ClientCalcPoints) InsertLiveChatMessage(
	ctx context.Context,
	msg *youtube.LiveChatMessage,
	parts []string,
) (_err error) {
	defer func() { c.addUsedPointsIfNoError(ctx, 200, _err) }()
	return c.Client.InsertLiveChatMessage(ctx, msg, parts)
}

func (c *ClientCalcPoints) InsertLiveChatBan(
	ctx context.Context,
	ban *youtube.LiveChatBan,
	parts []string,
) (_err error) {
	defer func() { c.addUsedPointsIfNoError(ctx, 200, _err) }()
	return c.Client.InsertLiveChatBan(ctx, ban, parts)
}

func (c *ClientCalcPoints) GetLiveChatMessages(
	ctx context.Context,
	chatID string,
	pageToken string,
	parts []string,
) (_ret *youtube.LiveChatMessageListResponse, _err error) {
	defer func() { c.addUsedPointsIfNoError(ctx, 1, _err) }()
	return c.Client.GetLiveChatMessages(ctx, chatID, pageToken, parts)
}

func (c *ClientCalcPoints) Search(
	ctx context.Context,
	chanID string,
	eventType EventType,
	parts []string,
) (_ret *youtube.SearchListResponse, _err error) {
	defer func() { c.addUsedPointsIfNoError(ctx, 1, _err) }()
	return c.Client.Search(ctx, chanID, eventType, parts)
}
