package youtube

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"google.golang.org/api/youtube/v3"
)

func newTestClientCalcPoints(cl client) *ClientCalcPoints {
	c := NewYouTubeClientCalcPoints(cl)
	c.PreviousCheckAt = time.Now()
	return c
}

type mockClient struct {
	client
}

func (m *mockClient) UpdateVideo(
	_ context.Context,
	_ *youtube.Video,
	_ []string,
) error {
	return nil
}

func (m *mockClient) DeleteChatMessage(
	_ context.Context,
	_ string,
) error {
	return nil
}

func (m *mockClient) GetBroadcasts(
	_ context.Context,
	_ BroadcastType,
	_ []string,
	_ []string,
	_ string,
) (*youtube.LiveBroadcastListResponse, error) {
	return &youtube.LiveBroadcastListResponse{}, nil
}

func (m *mockClient) InsertBroadcast(
	_ context.Context,
	_ *youtube.LiveBroadcast,
	_ []string,
) (*youtube.LiveBroadcast, error) {
	return &youtube.LiveBroadcast{}, nil
}

func (m *mockClient) UpdateBroadcast(
	_ context.Context,
	_ *youtube.LiveBroadcast,
	_ []string,
) error {
	return nil
}

func (m *mockClient) SetThumbnail(
	_ context.Context,
	_ string,
	_ io.Reader,
) error {
	return nil
}

func (m *mockClient) Search(
	_ context.Context,
	_ string,
	_ EventType,
	_ []string,
) (*youtube.SearchListResponse, error) {
	return &youtube.SearchListResponse{}, nil
}

func TestQuotaCostUpdateVideo(t *testing.T) {
	c := newTestClientCalcPoints(&mockClient{})
	ctx := context.Background()

	err := c.UpdateVideo(ctx, &youtube.Video{}, []string{"snippet"})
	assert.NoError(t, err)
	assert.Equal(t, uint64(50), c.UsedPoints.Load(), "UpdateVideo should cost 50 points")
}

func TestQuotaCostDeleteChatMessage(t *testing.T) {
	c := newTestClientCalcPoints(&mockClient{})
	ctx := context.Background()

	err := c.DeleteChatMessage(ctx, "msg-123")
	assert.NoError(t, err)
	assert.Equal(t, uint64(50), c.UsedPoints.Load(), "DeleteChatMessage should cost 50 points")
}

func TestQuotaCostGetBroadcasts(t *testing.T) {
	c := newTestClientCalcPoints(&mockClient{})
	ctx := context.Background()

	_, err := c.GetBroadcasts(ctx, BroadcastTypeAll, nil, []string{"snippet"}, "")
	assert.NoError(t, err)
	assert.Equal(t, uint64(1), c.UsedPoints.Load(), "GetBroadcasts should cost 1 point")
}

func TestQuotaCostInsertBroadcast(t *testing.T) {
	c := newTestClientCalcPoints(&mockClient{})
	ctx := context.Background()

	_, err := c.InsertBroadcast(ctx, &youtube.LiveBroadcast{}, []string{"snippet"})
	assert.NoError(t, err)
	assert.Equal(t, uint64(50), c.UsedPoints.Load(), "InsertBroadcast should cost 50 points")
}

func TestQuotaCostUpdateBroadcast(t *testing.T) {
	c := newTestClientCalcPoints(&mockClient{})
	ctx := context.Background()

	err := c.UpdateBroadcast(ctx, &youtube.LiveBroadcast{}, []string{"snippet"})
	assert.NoError(t, err)
	assert.Equal(t, uint64(50), c.UsedPoints.Load(), "UpdateBroadcast should cost 50 points")
}

func TestQuotaCostSetThumbnail(t *testing.T) {
	c := newTestClientCalcPoints(&mockClient{})
	ctx := context.Background()

	err := c.SetThumbnail(ctx, "bc-1", nil)
	assert.NoError(t, err)
	assert.Equal(t, uint64(50), c.UsedPoints.Load(), "SetThumbnail should cost 50 points")
}

func TestQuotaCostSearch(t *testing.T) {
	c := newTestClientCalcPoints(&mockClient{})
	ctx := context.Background()

	_, err := c.Search(ctx, "chan-1", EventTypeLive, []string{"snippet"})
	assert.NoError(t, err)
	assert.Equal(t, uint64(100), c.UsedPoints.Load(), "Search should cost 100 points")
}

func TestPerOperationQuotaTracking(t *testing.T) {
	c := newTestClientCalcPoints(&mockClient{})
	ctx := context.Background()

	_, _ = c.GetBroadcasts(ctx, BroadcastTypeAll, nil, []string{"snippet"}, "")
	_, _ = c.GetBroadcasts(ctx, BroadcastTypeAll, nil, []string{"snippet"}, "")
	_ = c.UpdateVideo(ctx, &youtube.Video{}, []string{"snippet"})
	_, _ = c.Search(ctx, "chan-1", EventTypeLive, []string{"snippet"})

	getBc, ok := c.UsedPointsByOp.Load("GetBroadcasts")
	assert.True(t, ok)
	assert.Equal(t, uint64(2), getBc)

	updVid, ok := c.UsedPointsByOp.Load("UpdateVideo")
	assert.True(t, ok)
	assert.Equal(t, uint64(50), updVid)

	search, ok := c.UsedPointsByOp.Load("Search")
	assert.True(t, ok)
	assert.Equal(t, uint64(100), search)

	assert.Equal(t, uint64(152), c.UsedPoints.Load())
}
