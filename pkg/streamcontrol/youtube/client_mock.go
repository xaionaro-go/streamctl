package youtube

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/facebookincubator/go-belt/tool/logger"
	"google.golang.org/api/youtube/v3"
)

type clientMock struct {
	locker      sync.Mutex
	broadcasts  map[string]*youtube.LiveBroadcast
	streams     map[string]*youtube.LiveStream
	videos      map[string]*youtube.Video
	playlists   map[string]*youtube.Playlist
	chatMsgNext int
}

var _ client = (*clientMock)(nil)

func newClientMock() *clientMock {
	m := &clientMock{
		broadcasts: make(map[string]*youtube.LiveBroadcast),
		streams:    make(map[string]*youtube.LiveStream),
		videos:     make(map[string]*youtube.Video),
		playlists:  make(map[string]*youtube.Playlist),
	}
	// Seed with template broadcasts so StartStream works
	m.broadcasts["template-1"] = &youtube.LiveBroadcast{
		Id: "template-1",
		Snippet: &youtube.LiveBroadcastSnippet{
			Title: "Template Broadcast 1",
		},
		ContentDetails: &youtube.LiveBroadcastContentDetails{
			BoundStreamId: "stream-1",
		},
		Status: &youtube.LiveBroadcastStatus{
			LifeCycleStatus: "upcoming",
		},
	}
	m.broadcasts["template-2"] = &youtube.LiveBroadcast{
		Id: "template-2",
		Snippet: &youtube.LiveBroadcastSnippet{
			Title: "Template Broadcast 2",
		},
		ContentDetails: &youtube.LiveBroadcastContentDetails{
			BoundStreamId: "stream-2",
		},
		Status: &youtube.LiveBroadcastStatus{
			LifeCycleStatus: "upcoming",
		},
	}
	m.videos["template-1"] = &youtube.Video{
		Id: "template-1",
		Snippet: &youtube.VideoSnippet{
			Title: "Template Broadcast 1",
		},
		Status: &youtube.VideoStatus{},
	}
	m.videos["template-2"] = &youtube.Video{
		Id: "template-2",
		Snippet: &youtube.VideoSnippet{
			Title: "Template Broadcast 2",
		},
		Status: &youtube.VideoStatus{},
	}
	m.streams["stream-1"] = &youtube.LiveStream{
		Id: "stream-1",
		Snippet: &youtube.LiveStreamSnippet{
			Title: "Stream 1",
		},
		Status: &youtube.LiveStreamStatus{
			StreamStatus: "active",
		},
	}
	m.streams["stream-2"] = &youtube.LiveStream{
		Id: "stream-2",
		Snippet: &youtube.LiveStreamSnippet{
			Title: "Stream 2",
		},
		Status: &youtube.LiveStreamStatus{
			StreamStatus: "active",
		},
	}
	return m
}

func (c *clientMock) Ping(ctx context.Context) (_err error) {
	logger.Tracef(ctx, "Ping")
	defer func() { logger.Tracef(ctx, "/Ping: %v", _err) }()
	return nil
}

func (c *clientMock) GetBroadcasts(
	ctx context.Context,
	t BroadcastType,
	ids []string,
	parts []string,
	pageToken string,
) (_ret *youtube.LiveBroadcastListResponse, _err error) {
	logger.Tracef(ctx, "GetBroadcasts Type: %s, IDs: %v", t, ids)
	defer func() { logger.Tracef(ctx, "/GetBroadcasts: %v", _err) }()
	c.locker.Lock()
	defer c.locker.Unlock()

	var items []*youtube.LiveBroadcast
	if len(ids) > 0 {
		for _, id := range ids {
			if b, ok := c.broadcasts[id]; ok {
				// For testing purposes, if we are looking for active,
				// we might want to pretend it IS active if it was just started.
				if t == BroadcastTypeActive {
					b.Status.LifeCycleStatus = "live"
					if b.Snippet != nil && b.Snippet.ActualStartTime == "" {
						b.Snippet.ActualStartTime = time.Now().Format(time.RFC3339)
					}
				}
				items = append(items, b)
			}
		}
	} else {
		for _, b := range c.broadcasts {
			// Basic filtering by type
			switch t {
			case BroadcastTypeActive:
				// If we have any broadcasts, pretend the first one is live for simple tests
				if b.Status != nil && b.Status.LifeCycleStatus == "complete" {
					continue
				}
				if b.Status == nil {
					b.Status = &youtube.LiveBroadcastStatus{}
				}
				b.Status.LifeCycleStatus = "live"
				if b.Snippet != nil && b.Snippet.ActualStartTime == "" {
					b.Snippet.ActualStartTime = time.Now().Format(time.RFC3339)
				}
				if b.ContentDetails == nil {
					b.ContentDetails = &youtube.LiveBroadcastContentDetails{}
				}
				if b.ContentDetails.MonitorStream == nil {
					b.ContentDetails.MonitorStream = &youtube.MonitorStreamInfo{}
				}
				items = append(items, b)
			case BroadcastTypeUpcoming:
				if b.Status.LifeCycleStatus == "upcoming" {
					items = append(items, b)
				}
			default:
				items = append(items, b)
			}
		}
	}

	return &youtube.LiveBroadcastListResponse{
		Items:           items,
		PageInfo:        &youtube.PageInfo{TotalResults: int64(len(items))},
		PrevPageToken:   pageToken,
		TokenPagination: &youtube.TokenPagination{},
	}, nil
}

func (c *clientMock) UpdateBroadcast(
	ctx context.Context,
	broadcast *youtube.LiveBroadcast,
	parts []string,
) (_err error) {
	logger.Tracef(ctx, "UpdateBroadcast ID: %s", broadcast.Id)
	defer func() { logger.Tracef(ctx, "/UpdateBroadcast: %v", _err) }()
	c.locker.Lock()
	defer c.locker.Unlock()

	if _, ok := c.broadcasts[broadcast.Id]; !ok {
		return fmt.Errorf("broadcast not found: %s", broadcast.Id)
	}

	if broadcast.ContentDetails != nil && broadcast.ContentDetails.EnableAutoStop {
		if broadcast.Status == nil {
			broadcast.Status = &youtube.LiveBroadcastStatus{}
		}
		broadcast.Status.LifeCycleStatus = "complete"
	}

	c.broadcasts[broadcast.Id] = broadcast
	return nil
}

func (c *clientMock) InsertBroadcast(
	ctx context.Context,
	broadcast *youtube.LiveBroadcast,
	parts []string,
) (_ret *youtube.LiveBroadcast, _err error) {
	logger.Tracef(ctx, "InsertBroadcast")
	defer func() { logger.Tracef(ctx, "/InsertBroadcast: %v", _err) }()
	c.locker.Lock()
	defer c.locker.Unlock()

	if broadcast.Id == "" {
		broadcast.Id = fmt.Sprintf("b-%d", time.Now().UnixNano())
	}
	c.broadcasts[broadcast.Id] = broadcast
	return broadcast, nil
}

func (c *clientMock) DeleteBroadcast(
	ctx context.Context,
	broadcastID string,
) (_err error) {
	logger.Tracef(ctx, "DeleteBroadcast ID: %s", broadcastID)
	defer func() { logger.Tracef(ctx, "/DeleteBroadcast: %v", _err) }()
	c.locker.Lock()
	defer c.locker.Unlock()

	delete(c.broadcasts, broadcastID)
	return nil
}

func (c *clientMock) GetStreams(
	ctx context.Context,
	parts []string,
) (_ret *youtube.LiveStreamListResponse, _err error) {
	logger.Tracef(ctx, "GetStreams")
	defer func() { logger.Tracef(ctx, "/GetStreams: %v", _err) }()
	c.locker.Lock()
	defer c.locker.Unlock()

	var items []*youtube.LiveStream
	for _, s := range c.streams {
		items = append(items, s)
	}

	return &youtube.LiveStreamListResponse{
		Items:           items,
		PageInfo:        &youtube.PageInfo{TotalResults: int64(len(items))},
		TokenPagination: &youtube.TokenPagination{},
	}, nil
}

func (c *clientMock) InsertStream(
	ctx context.Context,
	s *youtube.LiveStream,
	parts []string,
) (_ret *youtube.LiveStream, _err error) {
	c.locker.Lock()
	defer c.locker.Unlock()
	if s.Id == "" {
		s.Id = fmt.Sprintf("stream-%d", len(c.streams)+1)
	}
	c.streams[s.Id] = s
	return s, nil
}

func (c *clientMock) DeleteStream(
	ctx context.Context,
	id string,
) (_err error) {
	c.locker.Lock()
	defer c.locker.Unlock()
	delete(c.streams, id)
	return nil
}

func (c *clientMock) GetVideos(
	ctx context.Context,
	broadcastIDs []string,
	parts []string,
) (_ret *youtube.VideoListResponse, _err error) {
	logger.Tracef(ctx, "GetVideos IDs: %v", broadcastIDs)
	defer func() { logger.Tracef(ctx, "/GetVideos: %v", _err) }()
	c.locker.Lock()
	defer c.locker.Unlock()

	var items []*youtube.Video
	for _, id := range broadcastIDs {
		if v, ok := c.videos[id]; ok {
			items = append(items, v)
		}
	}

	return &youtube.VideoListResponse{
		Items: items,
	}, nil
}

func (c *clientMock) UpdateVideo(
	ctx context.Context,
	video *youtube.Video,
	parts []string,
) (_err error) {
	logger.Tracef(ctx, "UpdateVideo ID: %s", video.Id)
	defer func() { logger.Tracef(ctx, "/UpdateVideo: %v", _err) }()
	c.locker.Lock()
	defer c.locker.Unlock()

	c.videos[video.Id] = video
	return nil
}

func (c *clientMock) InsertCuepoint(
	ctx context.Context,
	cuepoint *youtube.Cuepoint,
) (_err error) {
	logger.Tracef(ctx, "InsertCuepoint")
	defer func() { logger.Tracef(ctx, "/InsertCuepoint: %v", _err) }()
	return nil
}

func (c *clientMock) GetPlaylists(
	ctx context.Context,
	playlistParts []string,
) (_ret *youtube.PlaylistListResponse, _err error) {
	logger.Tracef(ctx, "GetPlaylists")
	defer func() { logger.Tracef(ctx, "/GetPlaylists: %v", _err) }()
	c.locker.Lock()
	defer c.locker.Unlock()

	var items []*youtube.Playlist
	for _, p := range c.playlists {
		items = append(items, p)
	}

	return &youtube.PlaylistListResponse{
		Items: items,
	}, nil
}

func (c *clientMock) GetPlaylistItems(
	ctx context.Context,
	playlistID string,
	videoID string,
	parts []string,
) (_ret *youtube.PlaylistItemListResponse, _err error) {
	logger.Tracef(ctx, "GetPlaylistItems")
	defer func() { logger.Tracef(ctx, "/GetPlaylistItems: %v", _err) }()
	return &youtube.PlaylistItemListResponse{}, nil
}

func (c *clientMock) InsertPlaylistItem(
	ctx context.Context,
	item *youtube.PlaylistItem,
	parts []string,
) (_err error) {
	logger.Tracef(ctx, "InsertPlaylistItem")
	defer func() { logger.Tracef(ctx, "/InsertPlaylistItem: %v", _err) }()
	return nil
}

func (c *clientMock) SetThumbnail(
	ctx context.Context,
	broadcastID string,
	thumbnail io.Reader,
) (_err error) {
	logger.Tracef(ctx, "SetThumbnail")
	defer func() { logger.Tracef(ctx, "/SetThumbnail: %v", _err) }()
	return nil
}

func (c *clientMock) InsertCommentThread(
	ctx context.Context,
	t *youtube.CommentThread,
	parts []string,
) (_err error) {
	logger.Tracef(ctx, "InsertCommentThread")
	defer func() { logger.Tracef(ctx, "/InsertCommentThread: %v", _err) }()
	return nil
}

func (c *clientMock) ListChatMessages(
	ctx context.Context,
	chatID string,
	parts []string,
) (_ret *youtube.LiveChatMessageListResponse, _err error) {
	logger.Tracef(ctx, "ListChatMessages")
	defer func() { logger.Tracef(ctx, "/ListChatMessages: %v", _err) }()
	return &youtube.LiveChatMessageListResponse{}, nil
}

func (c *clientMock) DeleteChatMessage(
	ctx context.Context,
	messageID string,
) (_err error) {
	logger.Tracef(ctx, "DeleteChatMessage")
	defer func() { logger.Tracef(ctx, "/DeleteChatMessage: %v", _err) }()
	return nil
}

func (c *clientMock) GetLiveChatMessages(
	ctx context.Context,
	chatID string,
	pageToken string,
	parts []string,
) (_ret *youtube.LiveChatMessageListResponse, _err error) {
	logger.Tracef(ctx, "GetLiveChatMessages")
	defer func() { logger.Tracef(ctx, "/GetLiveChatMessages: %v", _err) }()
	return &youtube.LiveChatMessageListResponse{}, nil
}

func (c *clientMock) Search(
	ctx context.Context,
	chanID string,
	eventType EventType,
	parts []string,
) (_ret *youtube.SearchListResponse, _err error) {
	return &youtube.SearchListResponse{}, nil
}
