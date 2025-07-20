package youtube

import (
	"context"
	"fmt"
	"io"
	"os"
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

// see also: https://developers.google.com/youtube/v3/determine_quota_cost
type ClientCalcPoints struct {
	Client          client
	UsedPoints      atomic.Uint64
	CheckMutex      sync.Mutex
	PreviousCheckAt time.Time
}

var _ client = (*ClientCalcPoints)(nil)

func NewYouTubeClientCalcPoints(client client) *ClientCalcPoints {
	return &ClientCalcPoints{
		Client: client,
	}
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
