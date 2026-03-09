package youtube

import (
	"context"
	"fmt"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/youtube/grpc/ytgrpc"
	"golang.org/x/oauth2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type testQuotaTracker struct {
	consumed atomic.Uint64
}

func (t *testQuotaTracker) ReportQuotaConsumption(_ context.Context, _ string, points uint) {
	t.consumed.Add(uint64(points))
}

func (t *testQuotaTracker) UsedQuotaPoints() uint64 {
	return t.consumed.Load()
}

type mockTokenSource struct{}

func (m *mockTokenSource) Token() (*oauth2.Token, error) {
	return &oauth2.Token{
		AccessToken: "test-token",
		TokenType:   "Bearer",
		Expiry:      time.Now().Add(time.Hour),
	}, nil
}

type mockStreamListServer struct {
	ytgrpc.UnimplementedV3DataLiveChatMessageServiceServer
	messages []*ytgrpc.LiveChatMessage
	sent     bool
}

func (m *mockStreamListServer) StreamList(
	req *ytgrpc.LiveChatMessageListRequest,
	stream grpc.ServerStreamingServer[ytgrpc.LiveChatMessageListResponse],
) error {
	if m.sent {
		return nil
	}
	m.sent = true
	return stream.Send(&ytgrpc.LiveChatMessageListResponse{
		Items:         m.messages,
		NextPageToken: "next-token",
	})
}

func TestGRPCServicePath(t *testing.T) {
	expected := "/youtube.api.v3.V3DataLiveChatMessageService/StreamList"
	actual := ytgrpc.V3DataLiveChatMessageService_StreamList_FullMethodName
	assert.Equal(t, expected, actual)
}

func TestChatListenerConvertTextMessage(t *testing.T) {
	l := &ChatListener{}
	ctx := context.Background()

	item := &ytgrpc.LiveChatMessage{
		Id: "msg-123",
		Snippet: &ytgrpc.LiveChatMessageSnippet{
			Type:           ytgrpc.LiveChatMessageSnippet_TEXT_MESSAGE_EVENT,
			PublishedAt:    "2026-01-01T00:00:00.000Z",
			DisplayMessage: "Hello world",
		},
		AuthorDetails: &ytgrpc.LiveChatMessageAuthorDetails{
			ChannelId:   "UC12345",
			DisplayName: "TestUser",
		},
	}

	event := l.convertMessage(ctx, item)

	assert.Equal(t, streamcontrol.EventID("msg-123"), event.ID)
	assert.Equal(t, streamcontrol.EventTypeChatMessage, event.Type)
	assert.Equal(t, streamcontrol.UserID("UC12345"), event.User.ID)
	assert.Equal(t, "TestUser", event.User.Name)
	require.NotNil(t, event.Message)
	assert.Equal(t, "Hello world", event.Message.Content)
	assert.Equal(t, streamcontrol.TextFormatTypePlain, event.Message.Format)
	assert.Nil(t, event.Paid)
	assert.Nil(t, event.Tier)
	assert.Nil(t, event.TargetUser)
}

func TestChatListenerConvertSuperChat(t *testing.T) {
	l := &ChatListener{}
	ctx := context.Background()

	item := &ytgrpc.LiveChatMessage{
		Id: "msg-456",
		Snippet: &ytgrpc.LiveChatMessageSnippet{
			Type:           ytgrpc.LiveChatMessageSnippet_SUPER_CHAT_EVENT,
			PublishedAt:    "2026-01-01T00:00:00.000Z",
			DisplayMessage: "Super chat!",
			DisplayedContent: &ytgrpc.LiveChatMessageSnippet_SuperChatDetails{
				SuperChatDetails: &ytgrpc.LiveChatSuperChatDetails{
					AmountMicros: 5000000,
					Currency:     "USD",
					UserComment:  "Great stream!",
				},
			},
		},
		AuthorDetails: &ytgrpc.LiveChatMessageAuthorDetails{
			ChannelId:   "UC67890",
			DisplayName: "DonorUser",
		},
	}

	event := l.convertMessage(ctx, item)

	assert.Equal(t, streamcontrol.EventTypeCheer, event.Type)
	require.NotNil(t, event.Paid)
	assert.Equal(t, 5.0, event.Paid.Amount)
	assert.Equal(t, streamcontrol.CurrencyUSD, event.Paid.Currency)
	assert.Equal(t, "Super chat!", event.Message.Content)
}

func TestChatListenerConvertSuperChatFallbackComment(t *testing.T) {
	l := &ChatListener{}
	ctx := context.Background()

	item := &ytgrpc.LiveChatMessage{
		Id: "msg-457",
		Snippet: &ytgrpc.LiveChatMessageSnippet{
			Type: ytgrpc.LiveChatMessageSnippet_SUPER_CHAT_EVENT,
			DisplayedContent: &ytgrpc.LiveChatMessageSnippet_SuperChatDetails{
				SuperChatDetails: &ytgrpc.LiveChatSuperChatDetails{
					AmountMicros: 2000000,
					Currency:     "EUR",
					UserComment:  "Fallback comment",
				},
			},
		},
	}

	event := l.convertMessage(ctx, item)

	assert.Equal(t, streamcontrol.EventTypeCheer, event.Type)
	assert.Equal(t, "Fallback comment", event.Message.Content)
	assert.Equal(t, streamcontrol.CurrencyEUR, event.Paid.Currency)
}

func TestChatListenerConvertSuperSticker(t *testing.T) {
	l := &ChatListener{}
	ctx := context.Background()

	item := &ytgrpc.LiveChatMessage{
		Id: "msg-sticker",
		Snippet: &ytgrpc.LiveChatMessageSnippet{
			Type: ytgrpc.LiveChatMessageSnippet_SUPER_STICKER_EVENT,
			DisplayedContent: &ytgrpc.LiveChatMessageSnippet_SuperStickerDetails{
				SuperStickerDetails: &ytgrpc.LiveChatSuperStickerDetails{
					AmountMicros: 1000000,
					Currency:     "GBP",
				},
			},
		},
	}

	event := l.convertMessage(ctx, item)

	assert.Equal(t, streamcontrol.EventTypeCheer, event.Type)
	require.NotNil(t, event.Paid)
	assert.Equal(t, 1.0, event.Paid.Amount)
	assert.Equal(t, streamcontrol.CurrencyGBP, event.Paid.Currency)
}

func TestChatListenerConvertNewSponsor(t *testing.T) {
	l := &ChatListener{}
	ctx := context.Background()

	item := &ytgrpc.LiveChatMessage{
		Id: "msg-sponsor",
		Snippet: &ytgrpc.LiveChatMessageSnippet{
			Type: ytgrpc.LiveChatMessageSnippet_NEW_SPONSOR_EVENT,
			DisplayedContent: &ytgrpc.LiveChatMessageSnippet_NewSponsorDetails{
				NewSponsorDetails: &ytgrpc.LiveChatNewSponsorDetails{
					MemberLevelName: "Gold",
				},
			},
		},
		AuthorDetails: &ytgrpc.LiveChatMessageAuthorDetails{
			ChannelId:   "UCnewmember",
			DisplayName: "NewMember",
		},
	}

	event := l.convertMessage(ctx, item)

	assert.Equal(t, streamcontrol.EventTypeSubscriptionNew, event.Type)
	require.NotNil(t, event.Tier)
	assert.Equal(t, streamcontrol.Tier("Gold"), *event.Tier)
	assert.Equal(t, "NewMember", event.User.Name)
}

func TestChatListenerConvertMemberMilestone(t *testing.T) {
	l := &ChatListener{}
	ctx := context.Background()

	item := &ytgrpc.LiveChatMessage{
		Id: "msg-milestone",
		Snippet: &ytgrpc.LiveChatMessageSnippet{
			Type: ytgrpc.LiveChatMessageSnippet_MEMBER_MILESTONE_CHAT_EVENT,
			DisplayedContent: &ytgrpc.LiveChatMessageSnippet_MemberMilestoneChatDetails{
				MemberMilestoneChatDetails: &ytgrpc.LiveChatMemberMilestoneChatDetails{
					MemberLevelName: "Silver",
					MemberMonth:     12,
					UserComment:     "1 year!",
				},
			},
		},
	}

	event := l.convertMessage(ctx, item)

	assert.Equal(t, streamcontrol.EventTypeSubscriptionRenewed, event.Type)
	require.NotNil(t, event.Tier)
	assert.Equal(t, streamcontrol.Tier("Silver"), *event.Tier)
	assert.Equal(t, "1 year!", event.Message.Content)
}

func TestChatListenerConvertMembershipGifting(t *testing.T) {
	l := &ChatListener{}
	ctx := context.Background()

	item := &ytgrpc.LiveChatMessage{
		Id: "msg-gift",
		Snippet: &ytgrpc.LiveChatMessageSnippet{
			Type: ytgrpc.LiveChatMessageSnippet_MEMBERSHIP_GIFTING_EVENT,
			DisplayedContent: &ytgrpc.LiveChatMessageSnippet_MembershipGiftingDetails{
				MembershipGiftingDetails: &ytgrpc.LiveChatMembershipGiftingDetails{
					GiftMembershipsCount:     5,
					GiftMembershipsLevelName: "Platinum",
				},
			},
		},
		AuthorDetails: &ytgrpc.LiveChatMessageAuthorDetails{
			ChannelId:   "UCgifter",
			DisplayName: "GenerousUser",
		},
	}

	event := l.convertMessage(ctx, item)

	assert.Equal(t, streamcontrol.EventTypeGiftedSubscription, event.Type)
	require.NotNil(t, event.Tier)
	assert.Equal(t, streamcontrol.Tier("Platinum"), *event.Tier)
	assert.Equal(t, "5 memberships gifted", event.Message.Content)
	assert.Equal(t, "GenerousUser", event.User.Name)
}

func TestChatListenerConvertGiftMembershipReceived(t *testing.T) {
	l := &ChatListener{}
	ctx := context.Background()

	item := &ytgrpc.LiveChatMessage{
		Id: "msg-gift-recv",
		Snippet: &ytgrpc.LiveChatMessageSnippet{
			Type: ytgrpc.LiveChatMessageSnippet_GIFT_MEMBERSHIP_RECEIVED_EVENT,
			DisplayedContent: &ytgrpc.LiveChatMessageSnippet_GiftMembershipReceivedDetails{
				GiftMembershipReceivedDetails: &ytgrpc.LiveChatGiftMembershipReceivedDetails{
					MemberLevelName:                      "Gold",
					GifterChannelId:                      "UCgifter",
					AssociatedMembershipGiftingMessageId: "msg-gift",
				},
			},
		},
	}

	event := l.convertMessage(ctx, item)

	assert.Equal(t, streamcontrol.EventTypeGiftedSubscription, event.Type)
	require.NotNil(t, event.Tier)
	assert.Equal(t, streamcontrol.Tier("Gold"), *event.Tier)
}

func TestChatListenerConvertUserBanned(t *testing.T) {
	l := &ChatListener{}
	ctx := context.Background()

	item := &ytgrpc.LiveChatMessage{
		Id: "msg-ban",
		Snippet: &ytgrpc.LiveChatMessageSnippet{
			Type: ytgrpc.LiveChatMessageSnippet_USER_BANNED_EVENT,
			DisplayedContent: &ytgrpc.LiveChatMessageSnippet_UserBannedDetails{
				UserBannedDetails: &ytgrpc.LiveChatUserBannedMessageDetails{
					BannedUserDetails: &ytgrpc.ChannelProfileDetails{
						ChannelId:   "UCbanned",
						DisplayName: "BannedUser",
					},
					BanType:            ytgrpc.LiveChatUserBannedMessageDetails_TEMPORARY,
					BanDurationSeconds: 300,
				},
			},
		},
		AuthorDetails: &ytgrpc.LiveChatMessageAuthorDetails{
			ChannelId:   "UCmod",
			DisplayName: "Moderator",
		},
	}

	event := l.convertMessage(ctx, item)

	assert.Equal(t, streamcontrol.EventTypeBan, event.Type)
	assert.Equal(t, "Moderator", event.User.Name)
	require.NotNil(t, event.TargetUser)
	assert.Equal(t, streamcontrol.UserID("UCbanned"), event.TargetUser.ID)
	assert.Equal(t, "BannedUser", event.TargetUser.Name)
	assert.Equal(t, "temporary ban (300s)", event.Message.Content)
}

func TestChatListenerConvertPermanentBan(t *testing.T) {
	l := &ChatListener{}
	ctx := context.Background()

	item := &ytgrpc.LiveChatMessage{
		Id: "msg-permban",
		Snippet: &ytgrpc.LiveChatMessageSnippet{
			Type: ytgrpc.LiveChatMessageSnippet_USER_BANNED_EVENT,
			DisplayedContent: &ytgrpc.LiveChatMessageSnippet_UserBannedDetails{
				UserBannedDetails: &ytgrpc.LiveChatUserBannedMessageDetails{
					BannedUserDetails: &ytgrpc.ChannelProfileDetails{
						ChannelId:   "UCbanned2",
						DisplayName: "PermanentlyBanned",
					},
					BanType: ytgrpc.LiveChatUserBannedMessageDetails_PERMANENT,
				},
			},
		},
	}

	event := l.convertMessage(ctx, item)

	assert.Equal(t, streamcontrol.EventTypeBan, event.Type)
	assert.Equal(t, "permanent ban", event.Message.Content)
}

func TestChatListenerConvertMessageDeleted(t *testing.T) {
	l := &ChatListener{}
	ctx := context.Background()

	item := &ytgrpc.LiveChatMessage{
		Id: "msg-del",
		Snippet: &ytgrpc.LiveChatMessageSnippet{
			Type: ytgrpc.LiveChatMessageSnippet_MESSAGE_DELETED_EVENT,
			DisplayedContent: &ytgrpc.LiveChatMessageSnippet_MessageDeletedDetails{
				MessageDeletedDetails: &ytgrpc.LiveChatMessageDeletedDetails{
					DeletedMessageId: "msg-original",
				},
			},
		},
	}

	event := l.convertMessage(ctx, item)

	assert.Equal(t, streamcontrol.EventTypeOther, event.Type)
	require.NotNil(t, event.Message.InReplyTo)
	assert.Equal(t, streamcontrol.EventID("msg-original"), *event.Message.InReplyTo)
}

func TestChatListenerConvertMessageRetracted(t *testing.T) {
	l := &ChatListener{}
	ctx := context.Background()

	item := &ytgrpc.LiveChatMessage{
		Id: "msg-retract",
		Snippet: &ytgrpc.LiveChatMessageSnippet{
			Type: ytgrpc.LiveChatMessageSnippet_MESSAGE_RETRACTED_EVENT,
			DisplayedContent: &ytgrpc.LiveChatMessageSnippet_MessageRetractedDetails{
				MessageRetractedDetails: &ytgrpc.LiveChatMessageRetractedDetails{
					RetractedMessageId: "msg-to-retract",
				},
			},
		},
	}

	event := l.convertMessage(ctx, item)

	assert.Equal(t, streamcontrol.EventTypeOther, event.Type)
	require.NotNil(t, event.Message.InReplyTo)
	assert.Equal(t, streamcontrol.EventID("msg-to-retract"), *event.Message.InReplyTo)
}

func TestChatListenerConvertPoll(t *testing.T) {
	l := &ChatListener{}
	ctx := context.Background()

	item := &ytgrpc.LiveChatMessage{
		Id: "msg-poll",
		Snippet: &ytgrpc.LiveChatMessageSnippet{
			Type: ytgrpc.LiveChatMessageSnippet_POLL_EVENT,
			DisplayedContent: &ytgrpc.LiveChatMessageSnippet_PollDetails{
				PollDetails: &ytgrpc.LiveChatPollDetails{
					Metadata: &ytgrpc.LiveChatPollDetails_PollMetadata{
						QuestionText: "Best game?",
					},
					Status: ytgrpc.LiveChatPollDetails_ACTIVE,
				},
			},
		},
	}

	event := l.convertMessage(ctx, item)

	assert.Equal(t, streamcontrol.EventTypeOther, event.Type)
	assert.Equal(t, "Best game?", event.Message.Content)
}

func TestChatListenerConvertChatEnded(t *testing.T) {
	l := &ChatListener{}
	ctx := context.Background()

	item := &ytgrpc.LiveChatMessage{
		Id: "msg-ended",
		Snippet: &ytgrpc.LiveChatMessageSnippet{
			Type: ytgrpc.LiveChatMessageSnippet_CHAT_ENDED_EVENT,
		},
	}

	event := l.convertMessage(ctx, item)

	assert.Equal(t, streamcontrol.EventTypeStreamOffline, event.Type)
}

func TestChatListenerConvertNilSnippet(t *testing.T) {
	l := &ChatListener{}
	ctx := context.Background()

	item := &ytgrpc.LiveChatMessage{
		Id: "msg-789",
	}

	event := l.convertMessage(ctx, item)

	assert.Equal(t, streamcontrol.EventID("msg-789"), event.ID)
	assert.Equal(t, streamcontrol.EventTypeChatMessage, event.Type)
	require.NotNil(t, event.Message)
	assert.Equal(t, "", event.Message.Content)
}

func TestParseCurrencyString(t *testing.T) {
	assert.Equal(t, streamcontrol.CurrencyUSD, parseCurrencyString("USD"))
	assert.Equal(t, streamcontrol.CurrencyEUR, parseCurrencyString("EUR"))
	assert.Equal(t, streamcontrol.CurrencyGBP, parseCurrencyString("GBP"))
	assert.Equal(t, streamcontrol.CurrencyJPY, parseCurrencyString("JPY"))
	assert.Equal(t, streamcontrol.CurrencyOther, parseCurrencyString("BRL"))
	assert.Equal(t, streamcontrol.CurrencyOther, parseCurrencyString(""))
}

func TestChatListenerConvertSponsorOnlyMode(t *testing.T) {
	l := &ChatListener{}
	ctx := context.Background()

	for _, tc := range []struct {
		name    string
		msgType ytgrpc.LiveChatMessageSnippet_Type
	}{
		{"started", ytgrpc.LiveChatMessageSnippet_SPONSOR_ONLY_MODE_STARTED_EVENT},
		{"ended", ytgrpc.LiveChatMessageSnippet_SPONSOR_ONLY_MODE_ENDED_EVENT},
	} {
		t.Run(tc.name, func(t *testing.T) {
			item := &ytgrpc.LiveChatMessage{
				Id: fmt.Sprintf("msg-sponsor-mode-%s", tc.name),
				Snippet: &ytgrpc.LiveChatMessageSnippet{
					Type: tc.msgType,
				},
			}
			event := l.convertMessage(ctx, item)
			assert.Equal(t, streamcontrol.EventTypeOther, event.Type)
		})
	}
}

func TestChatListenerGRPCIntegration(t *testing.T) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}

	srv := grpc.NewServer()
	mockServer := &mockStreamListServer{
		messages: []*ytgrpc.LiveChatMessage{
			{
				Id: "integration-msg-1",
				Snippet: &ytgrpc.LiveChatMessageSnippet{
					Type:           ytgrpc.LiveChatMessageSnippet_TEXT_MESSAGE_EVENT,
					PublishedAt:    "2026-01-01T12:00:00.000Z",
					DisplayMessage: "Integration test message",
				},
				AuthorDetails: &ytgrpc.LiveChatMessageAuthorDetails{
					ChannelId:   "UCintegration",
					DisplayName: "IntegrationUser",
				},
			},
		},
	}
	ytgrpc.RegisterV3DataLiveChatMessageServiceServer(srv, mockServer)
	go srv.Serve(lis)
	defer srv.Stop()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	conn, err := grpc.NewClient(
		lis.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("failed to dial: %v", err)
	}
	defer conn.Close()

	client := ytgrpc.NewV3DataLiveChatMessageServiceClient(conn)
	stream, err := client.StreamList(ctx, &ytgrpc.LiveChatMessageListRequest{
		LiveChatId: "test-chat-id",
		Part:       []string{"snippet", "authorDetails"},
	})
	if err != nil {
		t.Fatalf("failed to call StreamList: %v", err)
	}

	resp, err := stream.Recv()
	if err != nil {
		t.Fatalf("failed to recv: %v", err)
	}

	require.Len(t, resp.Items, 1)
	assert.Equal(t, "integration-msg-1", resp.Items[0].Id)
	assert.Equal(t, "next-token", resp.NextPageToken)
}

type multiResponseStreamListServer struct {
	ytgrpc.UnimplementedV3DataLiveChatMessageServiceServer
	responseCount int
}

func (m *multiResponseStreamListServer) StreamList(
	req *ytgrpc.LiveChatMessageListRequest,
	stream grpc.ServerStreamingServer[ytgrpc.LiveChatMessageListResponse],
) error {
	for i := range m.responseCount {
		err := stream.Send(&ytgrpc.LiveChatMessageListResponse{
			Items: []*ytgrpc.LiveChatMessage{
				{
					Id: fmt.Sprintf("msg-%d", i),
					Snippet: &ytgrpc.LiveChatMessageSnippet{
						Type:           ytgrpc.LiveChatMessageSnippet_TEXT_MESSAGE_EVENT,
						PublishedAt:    "2026-01-01T12:00:00.000Z",
						DisplayMessage: fmt.Sprintf("message %d", i),
					},
					AuthorDetails: &ytgrpc.LiveChatMessageAuthorDetails{
						ChannelId:   "UCtest",
						DisplayName: "TestUser",
					},
				},
			},
			NextPageToken: fmt.Sprintf("token-%d", i),
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func TestChatListenerQuotaTracking(t *testing.T) {
	const responseCount = 5

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	srv := grpc.NewServer()
	mockServer := &multiResponseStreamListServer{responseCount: responseCount}
	ytgrpc.RegisterV3DataLiveChatMessageServiceServer(srv, mockServer)
	go srv.Serve(lis)
	defer srv.Stop()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	conn, err := grpc.NewClient(
		lis.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)
	defer conn.Close()

	client := ytgrpc.NewV3DataLiveChatMessageServiceClient(conn)

	qt := &testQuotaTracker{}
	listener := &ChatListener{
		liveChatID:      "test-chat-id",
		quotaTracker:    qt,
		messagesOutChan: make(chan streamcontrol.Event, 100),
	}

	var pageToken string
	err = listener.streamMessages(ctx, client, &pageToken)
	require.NoError(t, err)

	assert.Equal(t, uint64(5), qt.UsedQuotaPoints(),
		"StreamList quota is charged per gRPC stream open (provisional estimate: 5 units)")
	assert.Equal(t, fmt.Sprintf("token-%d", responseCount-1), pageToken)
	assert.Equal(t, responseCount, len(listener.messagesOutChan),
		"all messages should be delivered to the output channel")
}

func TestQuotaThrottleRemaining(t *testing.T) {
	t.Run("nil_tracker_returns_zero", func(t *testing.T) {
		l := &ChatListener{}
		assert.Equal(t, time.Duration(0), l.quotaThrottleRemaining(time.Now()))
	})

	t.Run("below_threshold_returns_zero", func(t *testing.T) {
		qt := &testQuotaTracker{}
		qt.consumed.Store(highQuotaUsageThreshold - 1)
		l := &ChatListener{quotaTracker: qt}
		assert.Equal(t, time.Duration(0), l.quotaThrottleRemaining(time.Now()))
	})

	t.Run("at_threshold_just_started_returns_full_delay", func(t *testing.T) {
		qt := &testQuotaTracker{}
		qt.consumed.Store(highQuotaUsageThreshold)
		l := &ChatListener{quotaTracker: qt}
		remaining := l.quotaThrottleRemaining(time.Now())
		assert.InDelta(t, highQuotaReconnectDelay.Seconds(), remaining.Seconds(), 1)
	})

	t.Run("at_threshold_partial_elapsed_returns_remainder", func(t *testing.T) {
		qt := &testQuotaTracker{}
		qt.consumed.Store(highQuotaUsageThreshold)
		l := &ChatListener{quotaTracker: qt}
		startedAt := time.Now().Add(-10 * time.Second)
		remaining := l.quotaThrottleRemaining(startedAt)
		expected := highQuotaReconnectDelay - 10*time.Second
		assert.InDelta(t, expected.Seconds(), remaining.Seconds(), 1)
	})

	t.Run("at_threshold_fully_elapsed_returns_zero", func(t *testing.T) {
		qt := &testQuotaTracker{}
		qt.consumed.Store(highQuotaUsageThreshold)
		l := &ChatListener{quotaTracker: qt}
		startedAt := time.Now().Add(-highQuotaReconnectDelay - time.Second)
		assert.Equal(t, time.Duration(0), l.quotaThrottleRemaining(startedAt))
	})
}
