package youtube

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/youtube/grpc/ytgrpc"
	"golang.org/x/oauth2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

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

func TestChatListenerStreamConvertMessage(t *testing.T) {
	l := &ChatListenerStream{}
	ctx := context.Background()

	item := &ytgrpc.LiveChatMessage{
		Id: "msg-123",
		Snippet: &ytgrpc.LiveChatMessageSnippet{
			PublishedAt:    "2026-01-01T00:00:00.000Z",
			DisplayMessage: "Hello world",
		},
		AuthorDetails: &ytgrpc.LiveChatMessageAuthorDetails{
			ChannelId:   "UC12345",
			DisplayName: "TestUser",
		},
	}

	event := l.convertMessage(ctx, item)

	if event.ID != "msg-123" {
		t.Errorf("expected ID 'msg-123', got '%s'", event.ID)
	}
	if event.Type != streamcontrol.EventTypeChatMessage {
		t.Errorf("expected EventTypeChatMessage, got %v", event.Type)
	}
	if event.User.ID != "UC12345" {
		t.Errorf("expected user ID 'UC12345', got '%s'", event.User.ID)
	}
	if event.User.Name != "TestUser" {
		t.Errorf("expected user name 'TestUser', got '%s'", event.User.Name)
	}
	if event.Message == nil {
		t.Fatal("expected message, got nil")
	}
	if event.Message.Content != "Hello world" {
		t.Errorf("expected message 'Hello world', got '%s'", event.Message.Content)
	}
	if event.Message.Format != streamcontrol.TextFormatTypePlain {
		t.Errorf("expected plain text format, got %v", event.Message.Format)
	}
	if event.Paid != nil {
		t.Errorf("expected no paid info, got %v", event.Paid)
	}
}

func TestChatListenerStreamConvertSuperChat(t *testing.T) {
	l := &ChatListenerStream{}
	ctx := context.Background()

	item := &ytgrpc.LiveChatMessage{
		Id: "msg-456",
		Snippet: &ytgrpc.LiveChatMessageSnippet{
			PublishedAt:    "2026-01-01T00:00:00.000Z",
			DisplayMessage: "Super chat!",
			SuperChatDetails: &ytgrpc.LiveChatSuperChatDetails{
				AmountMicros: 5000000,
				Currency:     "USD",
			},
		},
		AuthorDetails: &ytgrpc.LiveChatMessageAuthorDetails{
			ChannelId:   "UC67890",
			DisplayName: "DonorUser",
		},
	}

	event := l.convertMessage(ctx, item)

	if event.Paid == nil {
		t.Fatal("expected paid info for SuperChat, got nil")
	}
	if event.Paid.Amount != 5.0 {
		t.Errorf("expected amount 5.0, got %f", event.Paid.Amount)
	}
}

func TestChatListenerStreamConvertNilSnippet(t *testing.T) {
	l := &ChatListenerStream{}
	ctx := context.Background()

	item := &ytgrpc.LiveChatMessage{
		Id: "msg-789",
	}

	event := l.convertMessage(ctx, item)

	if event.ID != "msg-789" {
		t.Errorf("expected ID 'msg-789', got '%s'", event.ID)
	}
	if event.Message == nil {
		t.Fatal("expected message, got nil")
	}
	if event.Message.Content != "" {
		t.Errorf("expected empty message, got '%s'", event.Message.Content)
	}
}

func TestChatListenerStreamGRPCIntegration(t *testing.T) {
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

	// Override the gRPC host for testing by creating a direct connection
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

	if len(resp.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(resp.Items))
	}
	if resp.Items[0].Id != "integration-msg-1" {
		t.Errorf("expected ID 'integration-msg-1', got '%s'", resp.Items[0].Id)
	}
	if resp.NextPageToken != "next-token" {
		t.Errorf("expected next-token, got '%s'", resp.NextPageToken)
	}
}
