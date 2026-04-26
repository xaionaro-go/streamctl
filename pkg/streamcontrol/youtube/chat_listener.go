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

// minChatPollInterval is the lower bound between liveChatMessages.list calls.
// Sub-second polling burns daily quota with no signal value: chat is paginated
// via NextPageToken, not push-driven.
//
// Declared as a var (rather than const) so tests can shrink it without paying
// real wall-clock seconds per assertion.
var minChatPollInterval = time.Second

type ChatListener struct {
	videoID    string
	liveChatID string
	client     ChatClient

	wg              sync.WaitGroup
	cancelFunc      context.CancelFunc
	messagesOutChan chan streamcontrol.Event
}

type ChatClient interface {
	GetLiveChatMessages(ctx context.Context, chatID string, pageToken string, parts []string) (*youtube.LiveChatMessageListResponse, error)
}

func NewChatListener(
	ctx context.Context,
	ytClient ChatClient,
	videoID string,
	liveChatID string,
) (*ChatListener, error) {
	ctx, cancelFunc := context.WithCancel(ctx)
	l := &ChatListener{
		videoID:         videoID,
		liveChatID:      liveChatID,
		client:          ytClient,
		cancelFunc:      cancelFunc,
		messagesOutChan: make(chan streamcontrol.Event, 100),
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
			switch {
			case errors.As(err, &gErr):
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
			default:
				logger.Warnf(ctx, "unable to get chat messages: %v", err)
			}
			if !sleepCtx(ctx, minChatPollInterval) {
				return ctx.Err()
			}
			continue
		}

		for _, item := range response.Items {
			publishedAt, err := ParseTimestamp(item.Snippet.PublishedAt)
			if err != nil {
				logger.Errorf(ctx, "unable to parse the timestamp '%s': %v", item.Snippet.PublishedAt, err)
			}
			var msg streamcontrol.Event
			switch item.Snippet.Type {
			case "messageDeletedEvent":
				if item.Snippet.MessageDeletedDetails == nil {
					logger.Warnf(ctx, "messageDeletedEvent without MessageDeletedDetails, skipping")
					continue
				}
				deletedID := item.Snippet.MessageDeletedDetails.DeletedMessageId
				// "delete-" prefix prevents dedup collision with the original
				// chat message that still lives in the 5-minute Event ID cache.
				msg = streamcontrol.Event{
					ID:                streamcontrol.EventID("delete-" + deletedID),
					CreatedAt:         publishedAt,
					Type:              streamcontrol.EventTypeChatMessageDeleted,
					ReferredMessageID: &deletedID,
				}
			default:
				msg = streamcontrol.Event{
					ID:        streamcontrol.EventID(item.Id),
					CreatedAt: publishedAt,
					Type:      streamcontrol.EventTypeChatMessage,
					User: streamcontrol.User{
						ID:   streamcontrol.UserID(item.AuthorDetails.ChannelId),
						Slug: item.AuthorDetails.ChannelId,
						Name: item.AuthorDetails.DisplayName,
					},
					Message: &streamcontrol.Message{
						Content: item.Snippet.DisplayMessage,
						Format:  streamcontrol.TextFormatTypePlain,
					},
				}
				if item.Snippet.SuperChatDetails != nil {
					msg.Paid = &streamcontrol.Money{
						Currency: parseCurrencyString(item.Snippet.SuperChatDetails.Currency),
						Amount:   float64(item.Snippet.SuperChatDetails.AmountMicros) / 1000000,
					}
				}
			}
			select {
			case l.messagesOutChan <- msg:
			default:
				logger.Errorf(ctx, "the queue is full, have to drop %#+v", msg)
			}
		}
		pageToken = response.NextPageToken

		interval := max(time.Duration(response.PollingIntervalMillis)*time.Millisecond, minChatPollInterval)
		if !sleepCtx(ctx, interval) {
			return ctx.Err()
		}
	}
}

// sleepCtx blocks for d, returning early if ctx is cancelled. Returns true if
// the full duration elapsed, false on cancellation.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (h *ChatListener) Close(ctx context.Context) error {
	h.cancelFunc()
	return nil
}

func (h *ChatListener) MessagesChan() <-chan streamcontrol.Event {
	return h.messagesOutChan
}

func (h *ChatListener) GetVideoID() string {
	return h.videoID
}

func parseCurrencyString(s string) streamcontrol.Currency {
	switch s {
	case "USD":
		return streamcontrol.CurrencyUSD
	case "EUR":
		return streamcontrol.CurrencyEUR
	case "GBP":
		return streamcontrol.CurrencyGBP
	case "JPY":
		return streamcontrol.CurrencyJPY
	default:
		return streamcontrol.CurrencyOther
	}
}
