package youtube

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/facebookincubator/go-belt/tool/logger"
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
	GetVideos(ctx context.Context, broadcastIDs []string, parts []string) (*youtube.VideoListResponse, error)
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
	RequestWrapper func(context.Context, func(context.Context) error) error
}

func wrapRequest(
	ctx context.Context,
	requestWrapper func(context.Context, func(context.Context) error) error,
	doFn func(opts ...googleapi.CallOption) error,
	opts ...googleapi.CallOption,
) (_err error) {
	return requestWrapper(ctx, func(ctx context.Context) error {
		return doFn(opts...)
	})
}

func wrapRequestS[T any](
	ctx context.Context,
	requestWrapper func(context.Context, func(context.Context) error) error,
	doFn func(opts ...googleapi.CallOption) (T, error),
	opts ...googleapi.CallOption,
) (_err error) {
	return requestWrapper(ctx, func(ctx context.Context) error {
		_, err := doFn(opts...)
		return err
	})
}

func wrapRequestR[T any](
	ctx context.Context,
	requestWrapper func(context.Context, func(context.Context) error) error,
	doFn func(opts ...googleapi.CallOption) (T, error),
	opts ...googleapi.CallOption,
) (_ret T, _err error) {
	_err = requestWrapper(ctx, func(ctx context.Context) error {
		_ret, _err = doFn(opts...)
		return _err
	})
	return
}

var _ YouTubeClient = (*YouTubeClientV3)(nil)

func NewYouTubeClientV3(
	ctx context.Context,
	requestWrapper func(context.Context, func(context.Context) error) error,
	opts ...option.ClientOption,
) (*YouTubeClientV3, error) {
	srv, err := youtube.NewService(ctx, opts...)
	if err != nil {
		return nil, err
	}
	return &YouTubeClientV3{
		Service:        srv,
		RequestWrapper: requestWrapper,
	}, nil
}

func (c *YouTubeClientV3) Ping(
	ctx context.Context,
) (_err error) {
	logger.Tracef(ctx, "Ping")
	defer func() { logger.Tracef(ctx, "/Ping: %v", _err) }()
	err := wrapRequestS(ctx, c.RequestWrapper, c.I18nLanguages.List(nil).Context(ctx).Do)
	gErr := &googleapi.Error{}
	if err != nil && errors.As(err, &gErr) {
		if gErr.Code == http.StatusForbidden && len(gErr.Errors) == 1 {
			gErrItem := gErr.Errors[0]
			if gErrItem.Reason == "quotaExceeded" { // this is not an authentication or/and connection problem.
				return nil
			}
		}
	}
	return err
}

func (c *YouTubeClientV3) GetBroadcasts(
	ctx context.Context,
	t BroadcastType,
	ids []string,
	parts []string,
	pageToken string,
) (_ret *youtube.LiveBroadcastListResponse, _err error) {
	logger.Tracef(ctx, "GetBroadcasts")
	defer func() { logger.Tracef(ctx, "/GetBroadcasts: %v", _err) }()
	r := c.Service.LiveBroadcasts.List(append([]string{"id"}, parts...)).
		Context(ctx).Fields().
		MaxResults(50) // see 'maxResults' in https://developers.google.com/youtube/v3/live/docs/liveBroadcasts/list
	shouldSetMine := true
	if t != BroadcastTypeAll {
		r = r.BroadcastStatus(t.String())
		shouldSetMine = false
	}
	if ids != nil {
		r = r.Id(ids...)
		shouldSetMine = false
	}
	if shouldSetMine {
		r = r.Mine(true)
	}
	if pageToken != "" {
		r = r.PageToken(pageToken)
	}
	return wrapRequestR(ctx, c.RequestWrapper, r.Do, googleapi.QueryParameter("order", "date"))
}

func (c *YouTubeClientV3) UpdateBroadcast(
	ctx context.Context,
	broadcast *youtube.LiveBroadcast,
	parts []string,
) (_err error) {
	logger.Tracef(ctx, "UpdateBroadcast")
	defer func() { logger.Tracef(ctx, "/UpdateBroadcast: %v", _err) }()
	do := c.Service.LiveBroadcasts.Update(parts, broadcast).Context(ctx).Do
	return wrapRequestS(ctx, c.RequestWrapper, do)
}

func (c *YouTubeClientV3) InsertBroadcast(
	ctx context.Context,
	broadcast *youtube.LiveBroadcast,
	parts []string,
) (_ret *youtube.LiveBroadcast, _err error) {
	logger.Tracef(ctx, "InsertBroadcast")
	defer func() { logger.Tracef(ctx, "/InsertBroadcast: %v", _err) }()
	return c.Service.LiveBroadcasts.Insert(parts, broadcast).Context(ctx).Do()
}

func (c *YouTubeClientV3) DeleteBroadcast(
	ctx context.Context,
	broadcastID string,
) (_err error) {
	logger.Tracef(ctx, "DeleteBroadcast")
	defer func() { logger.Tracef(ctx, "/DeleteBroadcast: %v", _err) }()
	do := c.Service.LiveBroadcasts.Delete(broadcastID).Context(ctx).Do
	return wrapRequest(ctx, c.RequestWrapper, do)
}

func (c *YouTubeClientV3) InsertCuepoint(
	ctx context.Context,
	p *youtube.Cuepoint,
) (_err error) {
	logger.Tracef(ctx, "InsertCuepoint")
	defer func() { logger.Tracef(ctx, "/InsertCuepoint: %v", _err) }()
	do := c.Service.LiveBroadcasts.InsertCuepoint(p).Context(ctx).Do
	return wrapRequestS(ctx, c.RequestWrapper, do)
}

func (c *YouTubeClientV3) GetVideos(
	ctx context.Context,
	broadcastIDs []string,
	parts []string,
) (_ret *youtube.VideoListResponse, _err error) {
	logger.Tracef(ctx, "GetVideos")
	defer func() { logger.Tracef(ctx, "/GetVideos: %v", _err) }()
	do := c.Service.Videos.List(parts).Id(broadcastIDs...).Context(ctx).Do
	return wrapRequestR(ctx, c.RequestWrapper, do)
}

func (c *YouTubeClientV3) UpdateVideo(
	ctx context.Context,
	video *youtube.Video,
	parts []string,
) (_err error) {
	logger.Tracef(ctx, "UpdateVideo")
	defer func() { logger.Tracef(ctx, "/UpdateVideo: %v", _err) }()
	do := c.Service.Videos.Update(parts, video).Context(ctx).Do
	return wrapRequestS(ctx, c.RequestWrapper, do)
}

func (c *YouTubeClientV3) GetPlaylists(
	ctx context.Context,
	parts []string,
) (_ret *youtube.PlaylistListResponse, _err error) {
	logger.Tracef(ctx, "GetPlaylists")
	defer func() { logger.Tracef(ctx, "/GetPlaylists: %v", _err) }()
	do := c.Service.Playlists.List(parts).MaxResults(1000).Mine(true).Context(ctx).Do
	return wrapRequestR(ctx, c.RequestWrapper, do)
}

func (c *YouTubeClientV3) GetPlaylistItems(
	ctx context.Context,
	playlistID string,
	videoID string,
	parts []string,
) (_ret *youtube.PlaylistItemListResponse, _err error) {
	logger.Tracef(ctx, "GetPlaylistItems")
	defer func() { logger.Tracef(ctx, "/GetPlaylistItems: %v", _err) }()
	do := c.Service.PlaylistItems.List(parts).MaxResults(1000).PlaylistId(playlistID).VideoId(videoID).Context(ctx).Do
	return wrapRequestR(ctx, c.RequestWrapper, do)
}

func (c *YouTubeClientV3) InsertPlaylistItem(
	ctx context.Context,
	item *youtube.PlaylistItem,
	parts []string,
) (_err error) {
	logger.Tracef(ctx, "InsertPlaylistItem")
	defer func() { logger.Tracef(ctx, "/InsertPlaylistItem: %v", _err) }()
	do := c.Service.PlaylistItems.Insert(parts, item).Context(ctx).Do
	return wrapRequestS(ctx, c.RequestWrapper, do)
}

func (c *YouTubeClientV3) SetThumbnail(
	ctx context.Context,
	broadcastID string,
	thumbnail io.Reader,
) (_err error) {
	logger.Tracef(ctx, "SetThumbnail")
	defer func() { logger.Tracef(ctx, "/SetThumbnail: %v", _err) }()
	do := c.Service.Thumbnails.Set(broadcastID).Media(thumbnail).Context(ctx).Do
	return wrapRequestS(ctx, c.RequestWrapper, do)
}

func (c *YouTubeClientV3) GetStreams(
	ctx context.Context,
	parts []string,
) (_ret *youtube.LiveStreamListResponse, _err error) {
	logger.Tracef(ctx, "GetStreams")
	defer func() { logger.Tracef(ctx, "/GetStreams: %v", _err) }()
	do := c.Service.LiveStreams.List(parts).Mine(true).MaxResults(20).Context(ctx).Do
	return wrapRequestR(ctx, c.RequestWrapper, do)
}

func (c *YouTubeClientV3) InsertCommentThread(
	ctx context.Context,
	t *youtube.CommentThread,
	parts []string,
) (_err error) {
	logger.Tracef(ctx, "InsertCommentThread")
	defer func() { logger.Tracef(ctx, "/InsertCommentThread: %v", _err) }()
	do := c.Service.CommentThreads.Insert(parts, t).Context(ctx).Do
	return wrapRequestS(ctx, c.RequestWrapper, do)
}

func (c *YouTubeClientV3) ListChatMessages(
	ctx context.Context,
	chatID string,
	parts []string,
) (_ret *youtube.LiveChatMessageListResponse, _err error) {
	logger.Tracef(ctx, "ListChatMessages")
	defer func() { logger.Tracef(ctx, "/ListChatMessages: %v", _err) }()
	do := c.Service.LiveChatMessages.List(chatID, parts).Context(ctx).Do
	return wrapRequestR(ctx, c.RequestWrapper, do)
}

func (c *YouTubeClientV3) DeleteChatMessage(
	ctx context.Context,
	messageID string,
) (_err error) {
	logger.Tracef(ctx, "DeleteChatMessage")
	defer func() { logger.Tracef(ctx, "/DeleteChatMessage: %v", _err) }()
	do := c.Service.LiveChatMessages.Delete(messageID).Context(ctx).Do
	return wrapRequest(ctx, c.RequestWrapper, do)
}
