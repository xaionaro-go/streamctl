package youtube

import (
	"context"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"google.golang.org/api/youtube/v3"
)

func TestParseCurrencyString(t *testing.T) {
	for _, tc := range []struct {
		input    string
		expected streamcontrol.Currency
	}{
		{"USD", streamcontrol.CurrencyUSD},
		{"EUR", streamcontrol.CurrencyEUR},
		{"GBP", streamcontrol.CurrencyGBP},
		{"JPY", streamcontrol.CurrencyJPY},
		{"BRL", streamcontrol.CurrencyOther},
		{"", streamcontrol.CurrencyOther},
	} {
		t.Run(tc.input, func(t *testing.T) {
			got := parseCurrencyString(tc.input)
			assert.Equal(t, tc.expected, got)
		})
	}
}

type mockChatClient struct {
	GetLiveChatMessagesFunc func(ctx context.Context, chatID string, pageToken string, parts []string) (*youtube.LiveChatMessageListResponse, error)
}

func (m *mockChatClient) GetLiveChatMessages(
	ctx context.Context,
	chatID string,
	pageToken string,
	parts []string,
) (*youtube.LiveChatMessageListResponse, error) {
	return m.GetLiveChatMessagesFunc(ctx, chatID, pageToken, parts)
}

func TestSuperChatCreatesMoneyNotNil(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	callCount := 0
	mock := &mockChatClient{
		GetLiveChatMessagesFunc: func(ctx context.Context, chatID string, pageToken string, parts []string) (*youtube.LiveChatMessageListResponse, error) {
			callCount++
			switch callCount {
			case 1:
				return &youtube.LiveChatMessageListResponse{
					Items: []*youtube.LiveChatMessage{
						{
							Id: "msg-1",
							Snippet: &youtube.LiveChatMessageSnippet{
								PublishedAt:    "2025-01-01T00:00:00Z",
								DisplayMessage: "Super Chat message",
								SuperChatDetails: &youtube.LiveChatSuperChatDetails{
									AmountMicros: 5000000,
									Currency:     "USD",
								},
							},
							AuthorDetails: &youtube.LiveChatMessageAuthorDetails{
								ChannelId:   "UC123",
								DisplayName: "TestUser",
							},
						},
					},
					PollingIntervalMillis: 0,
				}, nil
			default:
				// Cancel after first poll to stop the listener
				cancel()
				return &youtube.LiveChatMessageListResponse{
					PollingIntervalMillis: 0,
				}, ctx.Err()
			}
		},
	}

	listener, err := NewChatListener(ctx, mock, "video-1", "chat-1")
	require.NoError(t, err)

	msg, ok := <-listener.MessagesChan()
	require.True(t, ok, "expected a message from the channel")

	require.NotNil(t, msg.Paid, "SuperChat must allocate *Money, not leave nil")
	assert.Equal(t, streamcontrol.CurrencyUSD, msg.Paid.Currency)
	assert.InDelta(t, 5.0, msg.Paid.Amount, 0.001)
	assert.Equal(t, streamcontrol.EventID("msg-1"), msg.ID)
}

func TestSuperChatNonPaidMessageHasNilMoney(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	callCount := 0
	mock := &mockChatClient{
		GetLiveChatMessagesFunc: func(ctx context.Context, chatID string, pageToken string, parts []string) (*youtube.LiveChatMessageListResponse, error) {
			callCount++
			switch callCount {
			case 1:
				return &youtube.LiveChatMessageListResponse{
					Items: []*youtube.LiveChatMessage{
						{
							Id: "msg-2",
							Snippet: &youtube.LiveChatMessageSnippet{
								PublishedAt:    "2025-01-01T00:00:00Z",
								DisplayMessage: "Normal message",
							},
							AuthorDetails: &youtube.LiveChatMessageAuthorDetails{
								ChannelId:   "UC456",
								DisplayName: "NormalUser",
							},
						},
					},
					PollingIntervalMillis: 0,
				}, nil
			default:
				cancel()
				return &youtube.LiveChatMessageListResponse{
					PollingIntervalMillis: 0,
				}, ctx.Err()
			}
		},
	}

	listener, err := NewChatListener(ctx, mock, "video-1", "chat-1")
	require.NoError(t, err)

	msg, ok := <-listener.MessagesChan()
	require.True(t, ok)
	assert.Nil(t, msg.Paid, "non-SuperChat message must have nil Paid")
}

// mockFullClient implements the full client interface for testing YouTube methods.
type mockFullClient struct {
	InsertLiveChatMessageFunc func(ctx context.Context, msg *youtube.LiveChatMessage, parts []string) error
	InsertLiveChatBanFunc     func(ctx context.Context, ban *youtube.LiveChatBan, parts []string) error
	DeleteChatMessageFunc     func(ctx context.Context, messageID string) error
}

func (m *mockFullClient) Ping(context.Context) error { return nil }
func (m *mockFullClient) GetBroadcasts(context.Context, BroadcastType, []string, []string, string) (*youtube.LiveBroadcastListResponse, error) {
	return nil, fmt.Errorf("not mocked")
}
func (m *mockFullClient) UpdateBroadcast(context.Context, *youtube.LiveBroadcast, []string) error {
	return fmt.Errorf("not mocked")
}
func (m *mockFullClient) InsertBroadcast(context.Context, *youtube.LiveBroadcast, []string) (*youtube.LiveBroadcast, error) {
	return nil, fmt.Errorf("not mocked")
}
func (m *mockFullClient) DeleteBroadcast(context.Context, string) error {
	return fmt.Errorf("not mocked")
}
func (m *mockFullClient) GetStreams(context.Context, []string) (*youtube.LiveStreamListResponse, error) {
	return nil, fmt.Errorf("not mocked")
}
func (m *mockFullClient) GetVideos(context.Context, []string, []string) (*youtube.VideoListResponse, error) {
	return nil, fmt.Errorf("not mocked")
}
func (m *mockFullClient) UpdateVideo(context.Context, *youtube.Video, []string) error {
	return fmt.Errorf("not mocked")
}
func (m *mockFullClient) InsertCuepoint(context.Context, *youtube.Cuepoint) error {
	return fmt.Errorf("not mocked")
}
func (m *mockFullClient) GetPlaylists(context.Context, []string) (*youtube.PlaylistListResponse, error) {
	return nil, fmt.Errorf("not mocked")
}
func (m *mockFullClient) GetPlaylistItems(context.Context, string, string, []string) (*youtube.PlaylistItemListResponse, error) {
	return nil, fmt.Errorf("not mocked")
}
func (m *mockFullClient) InsertPlaylistItem(context.Context, *youtube.PlaylistItem, []string) error {
	return fmt.Errorf("not mocked")
}
func (m *mockFullClient) SetThumbnail(context.Context, string, io.Reader) error {
	return fmt.Errorf("not mocked")
}
func (m *mockFullClient) InsertCommentThread(context.Context, *youtube.CommentThread, []string) error {
	return fmt.Errorf("not mocked")
}
func (m *mockFullClient) ListChatMessages(context.Context, string, []string) (*youtube.LiveChatMessageListResponse, error) {
	return nil, fmt.Errorf("not mocked")
}
func (m *mockFullClient) GetLiveChatMessages(context.Context, string, string, []string) (*youtube.LiveChatMessageListResponse, error) {
	return nil, fmt.Errorf("not mocked")
}
func (m *mockFullClient) Search(context.Context, string, EventType, []string) (*youtube.SearchListResponse, error) {
	return nil, fmt.Errorf("not mocked")
}

func (m *mockFullClient) DeleteChatMessage(ctx context.Context, messageID string) error {
	return m.DeleteChatMessageFunc(ctx, messageID)
}

func (m *mockFullClient) InsertLiveChatMessage(ctx context.Context, msg *youtube.LiveChatMessage, parts []string) error {
	return m.InsertLiveChatMessageFunc(ctx, msg, parts)
}

func (m *mockFullClient) InsertLiveChatBan(ctx context.Context, ban *youtube.LiveChatBan, parts []string) error {
	return m.InsertLiveChatBanFunc(ctx, ban, parts)
}

func newTestYouTube(mock *mockFullClient, broadcasts []*youtube.LiveBroadcast) *YouTube {
	return &YouTube{
		YouTubeClient: &ClientCalcPoints{
			Client: mock,
		},
		currentLiveBroadcasts: broadcasts,
	}
}

func testBroadcasts() []*youtube.LiveBroadcast {
	return []*youtube.LiveBroadcast{
		{
			Id: "broadcast-1",
			Snippet: &youtube.LiveBroadcastSnippet{
				LiveChatId: "chat-abc",
			},
		},
	}
}

func TestSendChatMessageCallsInsertLiveChatMessage(t *testing.T) {
	ctx := context.Background()
	var capturedMsg *youtube.LiveChatMessage

	mock := &mockFullClient{
		InsertLiveChatMessageFunc: func(ctx context.Context, msg *youtube.LiveChatMessage, parts []string) error {
			capturedMsg = msg
			return nil
		},
	}
	yt := newTestYouTube(mock, testBroadcasts())

	err := yt.SendChatMessage(ctx, "hello world")
	require.NoError(t, err)

	require.NotNil(t, capturedMsg)
	assert.Equal(t, "chat-abc", capturedMsg.Snippet.LiveChatId)
	assert.Equal(t, "textMessageEvent", capturedMsg.Snippet.Type)
	assert.Equal(t, "hello world", capturedMsg.Snippet.TextMessageDetails.MessageText)
}

func TestRemoveChatMessageCallsDeleteWithRealID(t *testing.T) {
	ctx := context.Background()
	var capturedID string

	mock := &mockFullClient{
		DeleteChatMessageFunc: func(ctx context.Context, messageID string) error {
			capturedID = messageID
			return nil
		},
	}
	yt := newTestYouTube(mock, testBroadcasts())

	err := yt.RemoveChatMessage(ctx, streamcontrol.EventID("LCC.CJmp9e...real-yt-id"))
	require.NoError(t, err)
	assert.Equal(t, "LCC.CJmp9e...real-yt-id", capturedID,
		"must pass the actual YouTube message ID, not a composite string")
}

func TestBanUserPermanent(t *testing.T) {
	ctx := context.Background()
	var capturedBan *youtube.LiveChatBan

	mock := &mockFullClient{
		InsertLiveChatBanFunc: func(ctx context.Context, ban *youtube.LiveChatBan, parts []string) error {
			capturedBan = ban
			return nil
		},
	}
	yt := newTestYouTube(mock, testBroadcasts())

	err := yt.BanUser(ctx, streamcontrol.UserID("UC999"), "spam", time.Time{})
	require.NoError(t, err)

	require.NotNil(t, capturedBan)
	assert.Equal(t, "chat-abc", capturedBan.Snippet.LiveChatId)
	assert.Equal(t, "UC999", capturedBan.Snippet.BannedUserDetails.ChannelId)
	assert.Equal(t, "permanent", capturedBan.Snippet.Type)
	assert.Equal(t, uint64(0), capturedBan.Snippet.BanDurationSeconds)
}

func TestBanUserTemporary(t *testing.T) {
	ctx := context.Background()
	var capturedBan *youtube.LiveChatBan

	mock := &mockFullClient{
		InsertLiveChatBanFunc: func(ctx context.Context, ban *youtube.LiveChatBan, parts []string) error {
			capturedBan = ban
			return nil
		},
	}
	yt := newTestYouTube(mock, testBroadcasts())

	deadline := time.Now().Add(5 * time.Minute)
	err := yt.BanUser(ctx, streamcontrol.UserID("UC999"), "timeout", deadline)
	require.NoError(t, err)

	require.NotNil(t, capturedBan)
	assert.Equal(t, "temporary", capturedBan.Snippet.Type)
	assert.InDelta(t, 300, float64(capturedBan.Snippet.BanDurationSeconds), 5,
		"5-minute ban should have ~300 second duration")
}

func TestBanType(t *testing.T) {
	assert.Equal(t, "permanent", banType(time.Time{}))
	assert.Equal(t, "temporary", banType(time.Now().Add(time.Hour)))
}


func TestParsePurchaseAmountText(t *testing.T) {
	for _, tc := range []struct {
		input            string
		expectedCurrency streamcontrol.Currency
		expectedAmount   float64
	}{
		{"$2.00", streamcontrol.CurrencyUSD, 2.0},
		{"$5.00", streamcontrol.CurrencyUSD, 5.0},
		{"€10.50", streamcontrol.CurrencyEUR, 10.5},
		{"£100.00", streamcontrol.CurrencyGBP, 100.0},
		{"¥500", streamcontrol.CurrencyJPY, 500.0},
		{"$1,000.00", streamcontrol.CurrencyUSD, 1000.0},
		{"R$50.00", streamcontrol.CurrencyOther, 50.0},
		{"", streamcontrol.CurrencyOther, 0},
	} {
		t.Run(tc.input, func(t *testing.T) {
			currency, amount := parsePurchaseAmountText(tc.input)
			assert.Equal(t, tc.expectedCurrency, currency)
			assert.InDelta(t, tc.expectedAmount, amount, 0.001)
		})
	}
}

// TestListenLoopErrorRetryFloor proves consecutive error retries respect the
// minChatPollInterval floor regardless of the API returning rapid-fire errors.
// Pre-fix the loop spun at HTTP-rate (~36/s observed in production logs).
func TestListenLoopErrorRetryFloor(t *testing.T) {
	orig := minChatPollInterval
	minChatPollInterval = 50 * time.Millisecond
	defer func() { minChatPollInterval = orig }()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	const wantCalls = 4
	var mu sync.Mutex
	callTimes := make([]time.Time, 0, wantCalls)
	done := make(chan struct{})

	mock := &mockChatClient{
		GetLiveChatMessagesFunc: func(ctx context.Context, chatID string, pageToken string, parts []string) (*youtube.LiveChatMessageListResponse, error) {
			mu.Lock()
			callTimes = append(callTimes, time.Now())
			n := len(callTimes)
			mu.Unlock()
			if n >= wantCalls {
				select {
				case <-done:
				default:
					close(done)
					cancel()
				}
			}
			return nil, fmt.Errorf("simulated API error")
		},
	}

	listener, err := NewChatListener(ctx, mock, "video-err", "chat-err")
	require.NoError(t, err)

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("listener did not produce expected error retries in time")
	}
	for range listener.MessagesChan() {
	}

	mu.Lock()
	defer mu.Unlock()
	require.GreaterOrEqual(t, len(callTimes), 2)
	for i := 1; i < len(callTimes); i++ {
		gap := callTimes[i].Sub(callTimes[i-1])
		assert.GreaterOrEqualf(t, gap, minChatPollInterval-5*time.Millisecond,
			"retry %d→%d gap %v must be ≥%v", i-1, i, gap, minChatPollInterval)
	}
}

// TestListenLoopSuccessPollFloor proves that even when the API returns
// PollingIntervalMillis=0 the listener still floors at minChatPollInterval.
func TestListenLoopSuccessPollFloor(t *testing.T) {
	orig := minChatPollInterval
	minChatPollInterval = 50 * time.Millisecond
	defer func() { minChatPollInterval = orig }()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	const wantCalls = 4
	var mu sync.Mutex
	callTimes := make([]time.Time, 0, wantCalls)
	done := make(chan struct{})

	mock := &mockChatClient{
		GetLiveChatMessagesFunc: func(ctx context.Context, chatID string, pageToken string, parts []string) (*youtube.LiveChatMessageListResponse, error) {
			mu.Lock()
			callTimes = append(callTimes, time.Now())
			n := len(callTimes)
			mu.Unlock()
			if n >= wantCalls {
				select {
				case <-done:
				default:
					close(done)
					cancel()
				}
			}
			return &youtube.LiveChatMessageListResponse{
				Items:                 nil,
				PollingIntervalMillis: 0,
			}, nil
		},
	}

	listener, err := NewChatListener(ctx, mock, "video-poll", "chat-poll")
	require.NoError(t, err)

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("listener did not produce expected polls in time")
	}
	for range listener.MessagesChan() {
	}

	mu.Lock()
	defer mu.Unlock()
	require.GreaterOrEqual(t, len(callTimes), 2)
	for i := 1; i < len(callTimes); i++ {
		gap := callTimes[i].Sub(callTimes[i-1])
		assert.GreaterOrEqualf(t, gap, minChatPollInterval-5*time.Millisecond,
			"poll %d→%d gap %v must be ≥%v", i-1, i, gap, minChatPollInterval)
	}
}
