package youtube

import (
	"context"
	"fmt"
	"io"

	"github.com/facebookincubator/go-belt/tool/logger"
	"google.golang.org/api/youtube/v3"
)

type YouTubeClientMock struct{}

var _ YouTubeClient = (*YouTubeClientMock)(nil)

func NewYouTubeClientMock() *YouTubeClientMock {
	return &YouTubeClientMock{}
}

func (c *YouTubeClientMock) Ping(ctx context.Context) (_err error) {
	logger.Tracef(ctx, "Ping")
	defer func() { logger.Tracef(ctx, "/Ping: %v", _err) }()
	return nil
}

func (c *YouTubeClientMock) GetBroadcasts(
	ctx context.Context,
	t BroadcastType,
	ids []string,
	parts []string,
	pageToken string,
) (_ret *youtube.LiveBroadcastListResponse, _err error) {
	logger.Tracef(ctx, "GetBroadcasts")
	defer func() { logger.Tracef(ctx, "/GetBroadcasts: %v", _err) }()
	return &youtube.LiveBroadcastListResponse{
		PageInfo:        &youtube.PageInfo{},
		PrevPageToken:   pageToken,
		TokenPagination: &youtube.TokenPagination{},
	}, nil
}

func (c *YouTubeClientMock) UpdateBroadcast(
	ctx context.Context,
	broadcast *youtube.LiveBroadcast,
	parts []string,
) (_err error) {
	logger.Tracef(ctx, "UpdateBroadcast")
	defer func() { logger.Tracef(ctx, "/UpdateBroadcast: %v", _err) }()
	return fmt.Errorf("not implemented")
}

func (c *YouTubeClientMock) InsertBroadcast(
	ctx context.Context,
	broadcast *youtube.LiveBroadcast,
	parts []string,
) (_ret *youtube.LiveBroadcast, _err error) {
	logger.Tracef(ctx, "InsertBroadcast")
	defer func() { logger.Tracef(ctx, "/InsertBroadcast: %v", _err) }()
	return nil, fmt.Errorf("not implemented")
}

func (c *YouTubeClientMock) DeleteBroadcast(
	ctx context.Context,
	broadcastID string,
) (_err error) {
	logger.Tracef(ctx, "DeleteBroadcast")
	defer func() { logger.Tracef(ctx, "/DeleteBroadcast: %v", _err) }()
	return fmt.Errorf("not implemented")
}

func (c *YouTubeClientMock) GetStreams(
	ctx context.Context,
	parts []string,
) (_ret *youtube.LiveStreamListResponse, _err error) {
	logger.Tracef(ctx, "GetStreams")
	defer func() { logger.Tracef(ctx, "/GetStreams: %v", _err) }()
	return &youtube.LiveStreamListResponse{
		PageInfo:        &youtube.PageInfo{},
		TokenPagination: &youtube.TokenPagination{},
	}, nil
}

func (c *YouTubeClientMock) GetVideos(
	ctx context.Context,
	broadcastIDs []string,
	parts []string,
) (_ret *youtube.VideoListResponse, _err error) {
	logger.Tracef(ctx, "GetVideos")
	defer func() { logger.Tracef(ctx, "/GetVideos: %v", _err) }()
	return nil, fmt.Errorf("not implemented")
}

func (c *YouTubeClientMock) UpdateVideo(
	ctx context.Context,
	video *youtube.Video,
	parts []string,
) (_err error) {
	logger.Tracef(ctx, "UpdateVideo")
	defer func() { logger.Tracef(ctx, "/UpdateVideo: %v", _err) }()
	return fmt.Errorf("not implemented")
}

func (c *YouTubeClientMock) InsertCuepoint(
	ctx context.Context,
	cuepoint *youtube.Cuepoint,
) (_err error) {
	logger.Tracef(ctx, "InsertCuepoint")
	defer func() { logger.Tracef(ctx, "/InsertCuepoint: %v", _err) }()
	return fmt.Errorf("not implemented")
}

func (c *YouTubeClientMock) GetPlaylists(
	ctx context.Context,
	playlistParts []string,
) (_ret *youtube.PlaylistListResponse, _err error) {
	logger.Tracef(ctx, "GetPlaylists")
	defer func() { logger.Tracef(ctx, "/GetPlaylists: %v", _err) }()
	return nil, fmt.Errorf("not implemented")
}

func (c *YouTubeClientMock) GetPlaylistItems(
	ctx context.Context,
	playlistID string,
	videoID string,
	parts []string,
) (_ret *youtube.PlaylistItemListResponse, _err error) {
	logger.Tracef(ctx, "GetPlaylistItems")
	defer func() { logger.Tracef(ctx, "/GetPlaylistItems: %v", _err) }()
	return nil, fmt.Errorf("not implemented")
}

func (c *YouTubeClientMock) InsertPlaylistItem(
	ctx context.Context,
	item *youtube.PlaylistItem,
	parts []string,
) (_err error) {
	logger.Tracef(ctx, "InsertPlaylistItem")
	defer func() { logger.Tracef(ctx, "/InsertPlaylistItem: %v", _err) }()
	return fmt.Errorf("not implemented")
}

func (c *YouTubeClientMock) SetThumbnail(
	ctx context.Context,
	broadcastID string,
	thumbnail io.Reader,
) (_err error) {
	logger.Tracef(ctx, "SetThumbnail")
	defer func() { logger.Tracef(ctx, "/SetThumbnail: %v", _err) }()
	return fmt.Errorf("not implemented")
}

func (c *YouTubeClientMock) InsertCommentThread(
	ctx context.Context,
	t *youtube.CommentThread,
	parts []string,
) (_err error) {
	logger.Tracef(ctx, "InsertCommentThread")
	defer func() { logger.Tracef(ctx, "/InsertCommentThread: %v", _err) }()
	return fmt.Errorf("not implemented")
}

func (c *YouTubeClientMock) ListChatMessages(
	ctx context.Context,
	chatID string,
	parts []string,
) (_ret *youtube.LiveChatMessageListResponse, _err error) {
	logger.Tracef(ctx, "ListChatMessages")
	defer func() { logger.Tracef(ctx, "/ListChatMessages: %v", _err) }()
	return nil, fmt.Errorf("not implemented")
}

func (c *YouTubeClientMock) DeleteChatMessage(
	ctx context.Context,
	messageID string,
) (_err error) {
	logger.Tracef(ctx, "DeleteChatMessage")
	defer func() { logger.Tracef(ctx, "/DeleteChatMessage: %v", _err) }()
	return fmt.Errorf("not implemented")
}
