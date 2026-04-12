package youtube

import (
	"context"
	"errors"
	"fmt"
	"html"
	"math/rand"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	ytchat "github.com/abhinavxd/youtube-live-chat-downloader/v2"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
)

const youtubeWatchURLString = `https://www.youtube.com/watch`

func chatCustomCookies() []*http.Cookie {
	// borrowed from: https://github.com/abhinavxd/youtube-live-chat-downloader/blob/main/example/main.go
	return []*http.Cookie{
		{Name: "PREF",
			Value:  "tz=Europe.Rome",
			MaxAge: 300},
		{Name: "CONSENT",
			Value:  fmt.Sprintf("YES+yt.432048971.it+FX+%d", 100+rand.Intn(999-100+1)),
			MaxAge: 300},
	}
}

var youtubeWatchURL *url.URL

func init() {
	var err error
	youtubeWatchURL, err = url.Parse(youtubeWatchURLString)
	if err != nil {
		panic(err)
	}

	ytchat.AddCookies(chatCustomCookies())
}

func ytWatchURL(videoID string) *url.URL {
	result := ptr(*youtubeWatchURL)
	query := result.Query()
	query.Add("v", videoID)
	result.RawQuery = query.Encode()
	return result
}

// TODO: delete this handler after explaining to YouTube the application and
// getting a quota for normal ChatListener.
type ChatListenerOBSOLETE struct {
	videoID          string
	continuationCode string
	clientConfig     ytchat.YtCfg
	wg               sync.WaitGroup
	cancelFunc       context.CancelFunc
	messagesOutChan  chan streamcontrol.Event
}

func NewChatListenerOBSOLETE(
	ctx context.Context,
	videoID string,
	onClose func(context.Context, *ChatListenerOBSOLETE),
) (*ChatListenerOBSOLETE, error) {
	if videoID == "" {
		return nil, fmt.Errorf("video ID is empty")
	}

	watchURL := ytWatchURL(videoID)

	continuationCode, cfg, err := ytchat.ParseInitialData(watchURL.String())
	if err != nil {
		return nil, fmt.Errorf("unable to fetch the initial data for chat messages retrieval (URL: %s): %w", watchURL, err)
	}

	ctx, cancelFunc := context.WithCancel(ctx)
	l := &ChatListenerOBSOLETE{
		videoID:          videoID,
		continuationCode: continuationCode,
		clientConfig:     cfg,
		cancelFunc:       cancelFunc,
		messagesOutChan:  make(chan streamcontrol.Event, 100),
	}
	l.wg.Add(1)
	observability.Go(ctx, func(ctx context.Context) {
		defer l.wg.Done()
		if onClose != nil {
			defer onClose(ctx, l)
		}
		defer func() {
			logger.Debugf(ctx, "the listener loop is finished")
			close(l.messagesOutChan)
		}()
		err := l.listenLoop(ctx)
		if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, ytchat.ErrLiveStreamOver) {
			logger.Errorf(ctx, "the listener loop returned an error: %v", err)
		}
	})
	return l, nil
}

const chatFetchRetryInterval = time.Second

func (l *ChatListenerOBSOLETE) listenLoop(ctx context.Context) (_err error) {
	logger.Debugf(ctx, "listenLoop")
	defer func() { logger.Debugf(ctx, "/listenLoop: %v", _err) }()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		msgs, newContinuation, _, err := ytchat.FetchContinuationChat(l.continuationCode, l.clientConfig)
		switch err {
		case nil:
		case ytchat.ErrLiveStreamOver:
			return err
		default:
			logger.Errorf(
				ctx,
				"unable to get a continuation for %v: %v; retrying in %v",
				l.videoID,
				err,
				chatFetchRetryInterval,
			)
			time.Sleep(chatFetchRetryInterval)
			continue
		}
		l.continuationCode = newContinuation

		for _, msg := range msgs {
			text, format := l.normalizeMessage(ctx, msg.Message)
			channelID := streamcontrol.UserID(sanitizeAuthorID(msg.AuthorID))
			ev := streamcontrol.Event{
				ID:        streamcontrol.EventID(msg.ID),
				CreatedAt: msg.Timestamp,
				User: streamcontrol.User{
					ID:   channelID,
					Slug: string(channelID),
					Name: sanitizeAuthorName(msg.AuthorName),
				},
				Message: &streamcontrol.Message{
					Content: text,
					Format:  format,
				},
			}

			switch msg.Type {
			case ytchat.ChatMessageTypeViewerEngagement:
				ev.Type = streamcontrol.EventTypeGreeting
			case ytchat.ChatMessageTypePaidMessage, ytchat.ChatMessageTypePaidSticker:
				ev.Type = streamcontrol.EventTypeCheer
				currency, amount := parsePurchaseAmountText(msg.PurchaseAmount)
				ev.Paid = &streamcontrol.Money{
					Currency: currency,
					Amount:   amount,
				}
			default:
				ev.Type = streamcontrol.EventTypeChatMessage
			}

			l.messagesOutChan <- ev
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func sanitizeAuthorID(authorID string) string {
	return authorID
}

func sanitizeAuthorName(authorName string) string {
	r, _ := strings.CutPrefix(authorName, "@")
	return r
}

// parsePurchaseAmountText parses YouTube SuperChat amount strings like "$2.00", "€5.00", "¥500"
// into a currency and numeric amount.
func parsePurchaseAmountText(s string) (streamcontrol.Currency, float64) {
	s = strings.TrimSpace(s)
	if len(s) == 0 {
		return streamcontrol.CurrencyOther, 0
	}

	// Map leading currency symbols to Currency values.
	// YouTube uses symbols like $, €, £, ¥ in purchaseAmountText.
	// Multi-char prefixes (e.g. "R$") must be checked before single-char ones.
	type prefix struct {
		symbol   string
		currency streamcontrol.Currency
	}
	prefixes := []prefix{
		{"R$", streamcontrol.CurrencyOther},
		{"$", streamcontrol.CurrencyUSD},
		{"€", streamcontrol.CurrencyEUR},
		{"£", streamcontrol.CurrencyGBP},
		{"¥", streamcontrol.CurrencyJPY},
	}

	currency := streamcontrol.CurrencyOther
	amountStr := s
	for _, p := range prefixes {
		if strings.HasPrefix(s, p.symbol) {
			currency = p.currency
			amountStr = strings.TrimPrefix(s, p.symbol)
			break
		}
	}

	// Remove thousands separators (commas) and spaces.
	amountStr = strings.ReplaceAll(amountStr, ",", "")
	amountStr = strings.TrimSpace(amountStr)

	amount, err := strconv.ParseFloat(amountStr, 64)
	if err != nil {
		return currency, 0
	}
	return currency, amount
}

func (h *ChatListenerOBSOLETE) normalizeMessage(
	ctx context.Context,
	msg string,
) (_ret0 string, _ret1 streamcontrol.TextFormatType) {
	logger.Tracef(ctx, "normalizeMessage(ctx, '%v')", msg)
	defer func() { logger.Tracef(ctx, "/normalizeMessage(ctx, '%v'): %v %v", msg, _ret0, _ret1) }()

	switch {
	case strings.Contains(msg, "https://yt3.ggpht.com/"):
		return messageAsHTML(msg), streamcontrol.TextFormatTypeHTML
	default:
		return msg, streamcontrol.TextFormatTypePlain
	}
}

func messageAsHTML(msg string) string {
	msg = html.EscapeString(msg)
	re := regexp.MustCompile(`https://yt3\.ggpht\.com/[^\s]+`)
	return re.ReplaceAllStringFunc(msg, func(link string) string {
		link = html.EscapeString(link)
		return fmt.Sprintf(`<img src="%s">`, link)
	})
}

func (h *ChatListenerOBSOLETE) Close(ctx context.Context) (_err error) {
	logger.Debugf(ctx, "Close(ctx)")
	defer func() { logger.Debugf(ctx, "/Close(ctx): %v", _err) }()
	h.cancelFunc()
	return nil
}

func (h *ChatListenerOBSOLETE) MessagesChan() <-chan streamcontrol.Event {
	return h.messagesOutChan
}

func (h *ChatListenerOBSOLETE) GetVideoID() string {
	return h.videoID
}
