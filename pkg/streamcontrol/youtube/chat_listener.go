package youtube

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/youtube/v3"
)

type ChatListener struct {
	videoID    string
	liveChatID string
	client     YouTubeChatClient

	wg              sync.WaitGroup
	cancelFunc      context.CancelFunc
	messagesOutChan chan streamcontrol.ChatMessage
}

type YouTubeChatClient interface {
	GetLiveChatMessages(ctx context.Context, chatID string, pageToken string, parts []string) (*youtube.LiveChatMessageListResponse, error)
}

func NewChatListener(
	ctx context.Context,
	ytClient YouTubeChatClient,
	videoID string,
	liveChatID string,
) (*ChatListener, error) {
	ctx, cancelFunc := context.WithCancel(ctx)
	l := &ChatListener{
		videoID:         videoID,
		liveChatID:      liveChatID,
		client:          ytClient,
		cancelFunc:      cancelFunc,
		messagesOutChan: make(chan streamcontrol.ChatMessage, 100),
	}
	l.wg.Add(1)
	observability.Go(ctx, func(ctx context.Context) {
		defer l.wg.Done()
		defer func() {
			logger.Debugf(ctx, "the listener loop is finished")
			close(l.messagesOutChan)
		}()
		err := l.listenLoop(ctx)
		if err != nil && !errors.Is(err, context.Canceled) && !errors.As(err, &ErrChatEnded{}) {
			logger.Errorf(ctx, "the listener loop returned an error: %v", err)
		}
	})

	return l, nil
}

func (l *ChatListener) listenLoop(ctx context.Context) (_err error) {
	logger.Debugf(ctx, "listenLoop")
	defer func() { logger.Debugf(ctx, "/listenLoop: %v", _err) }()

	var pageToken string
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		response, err := l.client.GetLiveChatMessages(
			ctx,
			l.liveChatID,
			pageToken,
			[]string{"snippet", "authorDetails"},
		)
		if err != nil {
			gErr := &googleapi.Error{}
			if !errors.As(err, &gErr) {
				logger.Warnf(ctx, "unable to get chat messages: %v", err)
				continue
			}
			for _, e := range gErr.Errors {
				switch e.Reason {
				case "liveChatEnded":
					return ErrChatEnded{ChatID: l.liveChatID}
				case "liveChatDisabled":
					return ErrChatDisabled{ChatID: l.liveChatID}
				case "liveChatNotFound":
					return ErrChatNotFound{ChatID: l.liveChatID}
				}
			}
			b, _ := json.Marshal(err)
			logger.Warnf(ctx, "unable to get chat messages: %v (%s)", err, b)
			continue
		}

		for _, item := range response.Items {
			publishedAt, err := ParseTimestamp(item.Snippet.PublishedAt)
			if err != nil {
				logger.Errorf(ctx, "unable to parse the timestamp '%s': %v", item.Snippet.PublishedAt, err)
			}
			msg := streamcontrol.ChatMessage{
				CreatedAt: publishedAt,
				EventType: streamcontrol.EventTypeChatMessage,
				UserID:    streamcontrol.ChatUserID(item.AuthorDetails.ChannelId),
				Username:  item.AuthorDetails.DisplayName,
				MessageID: streamcontrol.ChatMessageID(item.Id),
				Message:   item.Snippet.DisplayMessage,
			}
			if item.Snippet.SuperChatDetails != nil {
				msg.Paid.Currency = streamcontrol.CurrencyOther
				msg.Paid.Amount = float64(item.Snippet.SuperChatDetails.AmountMicros) / 1000000
			}
			select {
			case l.messagesOutChan <- msg:
			default:
				logger.Errorf(ctx, "the queue is full, have to drop %#+v", msg)
			}
		}
		pageToken = response.NextPageToken

		time.Sleep(time.Millisecond * time.Duration(response.PollingIntervalMillis))
	}
}

func (h *ChatListener) Close(ctx context.Context) error {
	h.cancelFunc()
	return nil
}

func (h *ChatListener) MessagesChan() <-chan streamcontrol.ChatMessage {
	return h.messagesOutChan
}

func (h *ChatListener) GetVideoID() string {
	return h.videoID
}
