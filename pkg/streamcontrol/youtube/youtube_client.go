package youtube

import (
	"context"
	"fmt"
	"io"

	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"
	"google.golang.org/api/youtube/v3"
)

type BroadcastType int

const (
	broadcastTypeUndefined = BroadcastType(iota)
	BroadcastTypeAll
	BroadcastTypeUpcoming
	BroadcastTypeActive
)

func (t BroadcastType) String() string {
	switch t {
	case broadcastTypeUndefined:
		return "<undefined>"
	case BroadcastTypeAll:
		return "all"
	case BroadcastTypeUpcoming:
		return "upcoming"
	case BroadcastTypeActive:
		return "active"
	}
	return fmt.Sprintf("unexpected_value_%d", int(t))
}

type YouTubeClient interface {
	Ping(context.Context) error
	GetBroadcasts(ctx context.Context, t BroadcastType, ids []string, parts []string, pageToken string) (*youtube.LiveBroadcastListResponse, error)
	UpdateBroadcast(context.Context, *youtube.LiveBroadcast, []string) error
	InsertBroadcast(context.Context, *youtube.LiveBroadcast, []string) (*youtube.LiveBroadcast, error)
	DeleteBroadcast(context.Context, string) error
	GetStreams(ctx context.Context, parts []string) (*youtube.LiveStreamListResponse, error)
	GetVideos(context.Context, []string, []string) (*youtube.VideoListResponse, error)
	UpdateVideo(context.Context, *youtube.Video, []string) error
	InsertCuepoint(context.Context, *youtube.Cuepoint) error
	GetPlaylists(ctx context.Context, playlistParts []string) (*youtube.PlaylistListResponse, error)
	GetPlaylistItems(ctx context.Context, playlistID string, videoID string, parts []string) (*youtube.PlaylistItemListResponse, error)
	InsertPlaylistItem(context.Context, *youtube.PlaylistItem, []string) error
	SetThumbnail(ctx context.Context, broadcastID string, thumbnail io.Reader) error
	InsertCommentThread(ctx context.Context, t *youtube.CommentThread, parts []string) error
	ListChatMessages(ctx context.Context, chatID string, parts []string) (*youtube.LiveChatMessageListResponse, error)
	DeleteChatMessage(ctx context.Context, messageID string) error
}

type YouTubeClientV3 struct {
	*youtube.Service
}

var _ YouTubeClient = (*YouTubeClientV3)(nil)

func NewYouTubeClientV3(
	ctx context.Context,
	opts ...option.ClientOption,
) (*YouTubeClientV3, error) {
	srv, err := youtube.NewService(ctx, opts...)
	if err != nil {
		return nil, err
	}
	return &YouTubeClientV3{
		Service: srv,
	}, nil
}

func (c *YouTubeClientV3) Ping(
	ctx context.Context,
) error {
	_, err := c.I18nLanguages.List(nil).Context(ctx).Do()
	return err
}

func (c *YouTubeClientV3) GetBroadcasts(
	ctx context.Context,
	t BroadcastType,
	ids []string,
	parts []string,
	pageToken string,
) (*youtube.LiveBroadcastListResponse, error) {
	r := c.Service.LiveBroadcasts.List(append([]string{"id"}, parts...)).
		Context(ctx).Fields().MaxResults(100).Mine(true)
	if t != BroadcastTypeAll {
		r = r.BroadcastStatus(t.String())
	}
	if ids != nil {
		r = r.Id(ids...)
	}
	if pageToken != "" {
		r = r.PageToken(pageToken)
	}
	return r.Do(googleapi.QueryParameter("order", "date"))
}

func (c *YouTubeClientV3) UpdateBroadcast(
	ctx context.Context,
	broadcast *youtube.LiveBroadcast,
	parts []string,
) error {
	_, err := c.Service.LiveBroadcasts.Update(parts, broadcast).Context(ctx).Do()
	return err
}

func (c *YouTubeClientV3) InsertBroadcast(
	ctx context.Context,
	broadcast *youtube.LiveBroadcast,
	parts []string,
) (*youtube.LiveBroadcast, error) {
	return c.Service.LiveBroadcasts.Insert(parts, broadcast).Context(ctx).Do()
}

func (c *YouTubeClientV3) DeleteBroadcast(
	ctx context.Context,
	broadcastID string,
) error {
	return c.Service.LiveBroadcasts.Delete(broadcastID).Context(ctx).Do()
}

func (c *YouTubeClientV3) InsertCuepoint(
	ctx context.Context,
	p *youtube.Cuepoint,
) error {
	_, err := c.Service.LiveBroadcasts.InsertCuepoint(p).Context(ctx).Do()
	return err
}

func (c *YouTubeClientV3) GetVideos(
	ctx context.Context,
	broadcastIDs []string,
	parts []string,
) (*youtube.VideoListResponse, error) {
	return c.Service.Videos.List(videoParts).
		Id(broadcastIDs...).Context(ctx).Do()
}

func (c *YouTubeClientV3) UpdateVideo(
	ctx context.Context,
	video *youtube.Video,
	parts []string,
) error {
	_, err := c.Service.Videos.Update(videoParts, video).Context(ctx).Do()
	return err
}

func (c *YouTubeClientV3) GetPlaylists(
	ctx context.Context,
	playlistParts []string,
) (*youtube.PlaylistListResponse, error) {
	return c.Service.Playlists.List(playlistParts).
		MaxResults(1000).
		Mine(true).
		Context(ctx).
		Do()
}

func (c *YouTubeClientV3) GetPlaylistItems(
	ctx context.Context,
	playlistID string,
	videoID string,
	parts []string,
) (*youtube.PlaylistItemListResponse, error) {
	return c.Service.PlaylistItems.List(parts).
		MaxResults(1000).
		PlaylistId(playlistID).
		VideoId(videoID).
		Context(ctx).
		Do()
}

func (c *YouTubeClientV3) InsertPlaylistItem(
	ctx context.Context,
	item *youtube.PlaylistItem,
	parts []string,
) error {
	_, err := c.Service.PlaylistItems.Insert(parts, item).
		Context(ctx).Do()
	return err
}

func (c *YouTubeClientV3) SetThumbnail(
	ctx context.Context,
	broadcastID string,
	thumbnail io.Reader,
) error {
	_, err := c.Service.Thumbnails.Set(broadcastID).
		Media(thumbnail).
		Context(ctx).
		Do()
	return err
}

func (c *YouTubeClientV3) GetStreams(
	ctx context.Context,
	parts []string,
) (*youtube.LiveStreamListResponse, error) {
	return c.Service.LiveStreams.
		List(parts).
		Mine(true).MaxResults(20).
		Context(ctx).Do()
}

func (c *YouTubeClientV3) InsertCommentThread(
	ctx context.Context,
	t *youtube.CommentThread,
	parts []string,
) error {
	_, err := c.Service.CommentThreads.Insert(parts, t).Context(ctx).Do()
	return err
}

func (c *YouTubeClientV3) ListChatMessages(
	ctx context.Context,
	chatID string,
	parts []string,
) (*youtube.LiveChatMessageListResponse, error) {
	return c.Service.
		LiveChatMessages.
		List(chatID, parts).
		Context(ctx).Do()
}

func (c *YouTubeClientV3) DeleteChatMessage(
	ctx context.Context,
	messageID string,
) error {
	return c.Service.LiveChatMessages.Delete(messageID).Context(ctx).Do()
}
