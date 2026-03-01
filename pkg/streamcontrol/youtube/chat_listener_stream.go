package youtube

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/youtube/grpc/ytgrpc"
	"golang.org/x/oauth2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	grpcoauth "google.golang.org/grpc/credentials/oauth"
	"google.golang.org/grpc/status"
)

const (
	youtubeGRPCHost      = "youtube.googleapis.com:443"
	streamReconnectDelay = 5 * time.Second
	maxReconnectDelay    = 60 * time.Second
)

type ChatListenerStream struct {
	videoID         string
	liveChatID      string
	tokenSource     oauth2.TokenSource
	wg              sync.WaitGroup
	cancelFunc      context.CancelFunc
	messagesOutChan chan streamcontrol.Event
}

func NewChatListenerStream(
	ctx context.Context,
	videoID string,
	liveChatID string,
	tokenSource oauth2.TokenSource,
) (*ChatListenerStream, error) {
	if videoID == "" {
		return nil, fmt.Errorf("video ID is empty")
	}
	if liveChatID == "" {
		return nil, fmt.Errorf("live chat ID is empty")
	}

	ctx, cancelFunc := context.WithCancel(ctx)
	l := &ChatListenerStream{
		videoID:         videoID,
		liveChatID:      liveChatID,
		tokenSource:     tokenSource,
		cancelFunc:      cancelFunc,
		messagesOutChan: make(chan streamcontrol.Event, 100),
	}
	l.wg.Add(1)
	observability.Go(ctx, func(ctx context.Context) {
		defer l.wg.Done()
		defer func() {
			logger.Debugf(ctx, "the stream listener loop is finished")
			close(l.messagesOutChan)
		}()
		err := l.listenLoop(ctx)
		if err != nil && !errors.Is(err, context.Canceled) && !errors.As(err, &ErrChatEnded{}) {
			logger.Errorf(ctx, "the stream listener loop returned an error: %v", err)
		}
	})

	return l, nil
}

func (l *ChatListenerStream) listenLoop(ctx context.Context) (_err error) {
	logger.Debugf(ctx, "listenLoop (gRPC streaming)")
	defer func() { logger.Debugf(ctx, "/listenLoop (gRPC streaming): %v", _err) }()

	var pageToken string
	reconnectDelay := streamReconnectDelay

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		err := l.streamMessages(ctx, &pageToken)
		if err == nil {
			// Stream ended cleanly (server closed it) — reconnect with last page token.
			reconnectDelay = streamReconnectDelay
			logger.Debugf(ctx, "gRPC stream ended, reconnecting with pageToken=%q", pageToken)
			continue
		}

		st, ok := status.FromError(err)
		if ok {
			switch st.Code() {
			case codes.FailedPrecondition:
				return ErrChatEnded{ChatID: l.liveChatID}
			case codes.NotFound:
				return ErrChatNotFound{ChatID: l.liveChatID}
			case codes.PermissionDenied:
				return ErrChatDisabled{ChatID: l.liveChatID}
			case codes.ResourceExhausted:
				logger.Warnf(ctx, "rate limited by YouTube gRPC, backing off for %v", reconnectDelay)
			case codes.Unavailable:
				logger.Debugf(ctx, "gRPC stream unavailable, reconnecting in %v", reconnectDelay)
			case codes.Canceled:
				return ctx.Err()
			default:
				logger.Warnf(ctx, "gRPC stream error (code=%s): %v, reconnecting in %v", st.Code(), err, reconnectDelay)
			}
		} else if errors.Is(err, context.Canceled) {
			return err
		} else {
			logger.Warnf(ctx, "gRPC stream error: %v, reconnecting in %v", err, reconnectDelay)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(reconnectDelay):
		}

		reconnectDelay = min(reconnectDelay*2, maxReconnectDelay)
	}
}

func (l *ChatListenerStream) streamMessages(ctx context.Context, pageToken *string) error {
	conn, err := grpc.NewClient(
		youtubeGRPCHost,
		grpc.WithTransportCredentials(credentials.NewTLS(nil)),
		grpc.WithPerRPCCredentials(&grpcoauth.TokenSource{TokenSource: l.tokenSource}),
	)
	if err != nil {
		return fmt.Errorf("unable to create gRPC client: %w", err)
	}
	defer func() {
		if err := conn.Close(); err != nil {
			logger.Warnf(ctx, "unable to close gRPC connection: %v", err)
		}
	}()

	client := ytgrpc.NewV3DataLiveChatMessageServiceClient(conn)

	req := &ytgrpc.LiveChatMessageListRequest{
		LiveChatId: l.liveChatID,
		Part:       []string{"snippet", "authorDetails"},
	}
	if *pageToken != "" {
		req.PageToken = *pageToken
	}

	logger.Infof(ctx, "opening gRPC stream for liveChatId=%q (no polling quota cost)", l.liveChatID)

	stream, err := client.StreamList(ctx, req)
	if err != nil {
		return fmt.Errorf("unable to open StreamList: %w", err)
	}

	for {
		resp, err := stream.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}

		if resp.NextPageToken != "" {
			*pageToken = resp.NextPageToken
		}

		for _, item := range resp.Items {
			msg := l.convertMessage(ctx, item)
			select {
			case l.messagesOutChan <- msg:
			default:
				logger.Errorf(ctx, "the queue is full, have to drop %#+v", msg)
			}
		}
	}
}

func (l *ChatListenerStream) convertMessage(ctx context.Context, item *ytgrpc.LiveChatMessage) streamcontrol.Event {
	var publishedAt time.Time
	if item.Snippet != nil {
		var err error
		publishedAt, err = ParseTimestamp(item.Snippet.PublishedAt)
		if err != nil {
			logger.Errorf(ctx, "unable to parse the timestamp '%s': %v", item.Snippet.PublishedAt, err)
		}
	}

	var user streamcontrol.User
	if item.AuthorDetails != nil {
		user = streamcontrol.User{
			ID:   streamcontrol.UserID(item.AuthorDetails.ChannelId),
			Slug: item.AuthorDetails.ChannelId,
			Name: item.AuthorDetails.DisplayName,
		}
	}

	var displayMessage string
	if item.Snippet != nil {
		displayMessage = item.Snippet.DisplayMessage
	}

	msg := streamcontrol.Event{
		ID:        streamcontrol.EventID(item.Id),
		CreatedAt: publishedAt,
		Type:      streamcontrol.EventTypeChatMessage,
		User:      user,
		Message: &streamcontrol.Message{
			Content: displayMessage,
			Format:  streamcontrol.TextFormatTypePlain,
		},
	}

	if item.Snippet != nil && item.Snippet.SuperChatDetails != nil {
		msg.Paid = &streamcontrol.Money{
			Currency: streamcontrol.CurrencyOther,
			Amount:   float64(item.Snippet.SuperChatDetails.AmountMicros) / 1000000,
		}
	}

	return msg
}

func (l *ChatListenerStream) Close(ctx context.Context) error {
	l.cancelFunc()
	return nil
}

func (l *ChatListenerStream) MessagesChan() <-chan streamcontrol.Event {
	return l.messagesOutChan
}

func (l *ChatListenerStream) GetVideoID() string {
	return l.videoID
}
