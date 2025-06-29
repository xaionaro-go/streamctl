package youtube

import (
	"context"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/facebookincubator/go-belt/tool/logger"
	"google.golang.org/api/youtube/v3"
)

var tzLosAngeles = must(time.LoadLocation("America/Los_Angeles"))

// see also: https://developers.google.com/youtube/v3/determine_quota_cost
type YouTubeClientCalcPoints struct {
	Client          YouTubeClient
	UsedPoints      atomic.Uint64
	CheckMutex      sync.Mutex
	PreviousCheckAt time.Time
}

var _ YouTubeClient = (*YouTubeClientCalcPoints)(nil)

func NewYouTubeClientCalcPoints(client YouTubeClient) *YouTubeClientCalcPoints {
	return &YouTubeClientCalcPoints{
		Client: client,
	}
}

func getQuotaCutoffDate(t time.Time) string {
	return t.In(tzLosAngeles).Format("2006-01-02")
}

func (c *YouTubeClientCalcPoints) addUsedPointsIfNoError(
	ctx context.Context,
	points uint,
	err error,
) {
	if err != nil {
		return
	}
	v := c.UsedPoints.Add(uint64(points))
	now := time.Now()
	curDate := getQuotaCutoffDate(now)
	if v > 5000 {
		logger.Warnf(ctx, "now %d points were used", v)
	} else {
		logger.Tracef(ctx, "now %d points were used", v)
	}
	c.CheckMutex.Lock()
	defer c.CheckMutex.Unlock()
	prevDate := getQuotaCutoffDate(c.PreviousCheckAt)
	c.PreviousCheckAt = now
	if curDate != prevDate {
		logger.Infof(ctx, "new quota day in YouTube: '%s' != '%s'", curDate, prevDate)
		c.UsedPoints.Store(0)
	}
}

func (c *YouTubeClientCalcPoints) Ping(ctx context.Context) (_err error) {
	defer func() { c.addUsedPointsIfNoError(ctx, 1, _err) }()
	return c.Client.Ping(ctx)
}

func (c *YouTubeClientCalcPoints) GetBroadcasts(
	ctx context.Context,
	t BroadcastType,
	ids []string,
	parts []string,
	pageToken string,
) (_ret *youtube.LiveBroadcastListResponse, _err error) {
	defer func() { c.addUsedPointsIfNoError(ctx, 1, _err) }()
	return c.Client.GetBroadcasts(ctx, t, ids, parts, pageToken)
}

func (c *YouTubeClientCalcPoints) UpdateBroadcast(
	ctx context.Context,
	broadcast *youtube.LiveBroadcast,
	parts []string,
) (_err error) {
	defer func() { c.addUsedPointsIfNoError(ctx, 50, _err) }()
	return c.Client.UpdateBroadcast(ctx, broadcast, parts)
}

func (c *YouTubeClientCalcPoints) InsertBroadcast(
	ctx context.Context,
	broadcast *youtube.LiveBroadcast,
	parts []string,
) (_ret *youtube.LiveBroadcast, _err error) {
	defer func() { c.addUsedPointsIfNoError(ctx, 50, _err) }()
	return c.Client.InsertBroadcast(ctx, broadcast, parts)
}

func (c *YouTubeClientCalcPoints) DeleteBroadcast(
	ctx context.Context,
	broadcastID string,
) (_err error) {
	defer func() { c.addUsedPointsIfNoError(ctx, 50, _err) }()
	return c.Client.DeleteBroadcast(ctx, broadcastID)
}

func (c *YouTubeClientCalcPoints) GetStreams(
	ctx context.Context,
	parts []string,
) (_ret *youtube.LiveStreamListResponse, _err error) {
	defer func() { c.addUsedPointsIfNoError(ctx, 1, _err) }()
	return c.Client.GetStreams(ctx, parts)
}

func (c *YouTubeClientCalcPoints) GetVideos(
	ctx context.Context,
	broadcastIDs []string,
	parts []string,
) (_ret *youtube.VideoListResponse, _err error) {
	defer func() { c.addUsedPointsIfNoError(ctx, 1, _err) }()
	return c.Client.GetVideos(ctx, broadcastIDs, parts)
}

func (c *YouTubeClientCalcPoints) UpdateVideo(
	ctx context.Context,
	video *youtube.Video,
	parts []string,
) (_err error) {
	defer func() { c.addUsedPointsIfNoError(ctx, 1, _err) }()
	return c.Client.UpdateVideo(ctx, video, parts)
}

func (c *YouTubeClientCalcPoints) InsertCuepoint(
	ctx context.Context,
	cuepoint *youtube.Cuepoint,
) (_err error) {
	defer func() { c.addUsedPointsIfNoError(ctx, 50, _err) }()
	return c.Client.InsertCuepoint(ctx, cuepoint)
}

func (c *YouTubeClientCalcPoints) GetPlaylists(
	ctx context.Context,
	playlistParts []string,
) (_ret *youtube.PlaylistListResponse, _err error) {
	defer func() { c.addUsedPointsIfNoError(ctx, 1, _err) }()
	return c.Client.GetPlaylists(ctx, playlistParts)
}

func (c *YouTubeClientCalcPoints) GetPlaylistItems(
	ctx context.Context,
	playlistID string,
	videoID string,
	parts []string,
) (_ret *youtube.PlaylistItemListResponse, _err error) {
	defer func() { c.addUsedPointsIfNoError(ctx, 50, _err) }()
	return c.Client.GetPlaylistItems(ctx, playlistID, videoID, parts)
}

func (c *YouTubeClientCalcPoints) InsertPlaylistItem(
	ctx context.Context,
	item *youtube.PlaylistItem,
	parts []string,
) (_err error) {
	defer func() { c.addUsedPointsIfNoError(ctx, 50, _err) }()
	return c.Client.InsertPlaylistItem(ctx, item, parts)
}

func (c *YouTubeClientCalcPoints) SetThumbnail(
	ctx context.Context,
	broadcastID string,
	thumbnail io.Reader,
) (_err error) {
	defer func() { c.addUsedPointsIfNoError(ctx, 50, _err) }()
	return c.Client.SetThumbnail(ctx, broadcastID, thumbnail)
}

func (c *YouTubeClientCalcPoints) InsertCommentThread(
	ctx context.Context,
	t *youtube.CommentThread,
	parts []string,
) (_err error) {
	defer func() { c.addUsedPointsIfNoError(ctx, 50, _err) }()
	return c.Client.InsertCommentThread(ctx, t, parts)
}

func (c *YouTubeClientCalcPoints) ListChatMessages(
	ctx context.Context,
	chatID string,
	parts []string,
) (_ret *youtube.LiveChatMessageListResponse, _err error) {
	defer func() { c.addUsedPointsIfNoError(ctx, 1, _err) }()
	return c.Client.ListChatMessages(ctx, chatID, parts)
}

func (c *YouTubeClientCalcPoints) DeleteChatMessage(
	ctx context.Context,
	messageID string,
) (_err error) {
	defer func() { c.addUsedPointsIfNoError(ctx, 1, _err) }()
	return c.Client.DeleteChatMessage(ctx, messageID)
}
