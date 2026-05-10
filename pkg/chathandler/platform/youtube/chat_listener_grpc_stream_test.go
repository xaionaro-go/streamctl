package youtube

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/youtubeapiproxy/grpc/ytgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// recordingStreamListServer captures every PageToken it receives and signals
// when the second StreamList call has been observed. The server's first call
// returns one response with NextPageToken="cursor-1" then EOF; the second
// call returns nil immediately (EOF on first Recv) so the client loop can
// observe whether the cursor was threaded into PageToken.
type recordingStreamListServer struct {
	ytgrpc.UnimplementedV3DataLiveChatMessageServiceServer

	mu         sync.Mutex
	pageTokens []string
	done       chan struct{} // closed once the second call's PageToken is captured
}

func (s *recordingStreamListServer) StreamList(
	req *ytgrpc.LiveChatMessageListRequest,
	stream grpc.ServerStreamingServer[ytgrpc.LiveChatMessageListResponse],
) error {
	s.mu.Lock()
	s.pageTokens = append(s.pageTokens, req.GetPageToken())
	idx := len(s.pageTokens)
	s.mu.Unlock()

	switch idx {
	case 1:
		// First call: emit one response carrying the cursor, then EOF.
		return stream.Send(&ytgrpc.LiveChatMessageListResponse{
			NextPageToken: "cursor-1",
		})
	default:
		// Second (and any subsequent) call: signal observed and EOF
		// immediately so the test goroutine can cancel deterministically.
		select {
		case <-s.done:
		default:
			close(s.done)
		}
		return nil
	}
}

func (s *recordingStreamListServer) capturedPageTokens() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.pageTokens))
	copy(out, s.pageTokens)
	return out
}

// TestGRPCStreamListener_ThreadsNextPageToken proves that
// streamChat threads LiveChatMessageListResponse.NextPageToken into the
// PageToken of the next StreamList call across an EOF-reconnect within a
// single streamChat invocation. Without the thread, the second call's
// PageToken stays empty and the listener re-fetches the recent backlog —
// the dup-spam observed in /tmp/l before the iter-1 dedup gate.
func TestGRPCStreamListener_ThreadsNextPageToken(t *testing.T) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	srv := grpc.NewServer()
	defer srv.Stop()

	rec := &recordingStreamListServer{done: make(chan struct{})}
	ytgrpc.RegisterV3DataLiveChatMessageServiceServer(srv, rec)
	go func() { _ = srv.Serve(lis) }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := grpc.NewClient(lis.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	defer conn.Close()

	chatClient := ytgrpc.NewV3DataLiveChatMessageServiceClient(conn)

	listener := &GRPCStreamListener{}
	ch := make(chan streamcontrol.Event, 16)

	streamChatDone := make(chan struct{})
	go func() {
		listener.streamChat(ctx, chatClient, "test-chat", ch)
		close(streamChatDone)
	}()

	// Wait for the second-call PageToken to be captured, then cancel
	// streamChat so it returns. No time.Sleep used for ordering — the
	// channel close is deterministic.
	select {
	case <-rec.done:
	case <-ctx.Done():
		t.Fatal("timed out waiting for second StreamList call")
	}
	cancel()

	select {
	case <-streamChatDone:
	case <-time.After(2 * time.Second):
		t.Fatal("streamChat did not return after cancel")
	}

	tokens := rec.capturedPageTokens()
	require.GreaterOrEqual(t, len(tokens), 2,
		"expected at least 2 StreamList calls, got %d", len(tokens))
	assert.Equal(t, []string{"", "cursor-1"}, tokens[:2],
		"first call must use empty PageToken; second must thread NextPageToken from the first response")
}
