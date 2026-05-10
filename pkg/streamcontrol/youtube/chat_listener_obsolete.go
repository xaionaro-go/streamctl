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
	watchURL         *url.URL
	continuationCode string
	clientConfig     ytchat.YtCfg
	wg               sync.WaitGroup
	cancelFunc       context.CancelFunc
	messagesOutChan  chan streamcontrol.Event

	// scraper is the upstream-call seam that listenLoop and
	// refreshContinuation drive. NewChatListenerOBSOLETE wires
	// ytChatScraper{}; tests inject fakes.
	scraper chatScraper
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

	ctx, cancelFunc := context.WithCancel(ctx)
	l := &ChatListenerOBSOLETE{
		videoID:         videoID,
		watchURL:        watchURL,
		cancelFunc:      cancelFunc,
		messagesOutChan: make(chan streamcontrol.Event, 100),
		scraper:         ytChatScraper{},
	}
	if err := l.refreshContinuation(ctx); err != nil {
		cancelFunc()
		return nil, fmt.Errorf("unable to fetch the initial data for chat messages retrieval (URL: %s): %w", watchURL, err)
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

const (
	chatFetchRetryInterval = time.Second

	// maxConsecutiveFetchFailures bounds the in-loop retry budget. A
	// stale or server-rejected continuation that ParseInitialData keeps
	// "successfully" returning verbatim cannot be recovered here — the
	// listener exits so the upstream discoverAndScrapeLoop can
	// re-discover the broadcast (and may pick a different videoID).
	// Without this cap the loop hammers YouTube at 1 req/s indefinitely
	// — observed on areion 2026-05-08 (3h, 6452 errors on a stale
	// videoID).
	maxConsecutiveFetchFailures = 5
)

// refreshContinuation re-fetches the watch page to obtain a fresh continuation
// token and ytcfg. Used both at startup and after a fetch error: a stale or
// server-rejected continuation (e.g. HTTP 400 INVALID_ARGUMENT) cannot be
// recovered by retrying the same payload.
func (l *ChatListenerOBSOLETE) refreshContinuation(ctx context.Context) error {
	logger.Debugf(ctx, "refreshing continuation for %s", l.videoID)
	continuationCode, cfg, err := l.scraper.ParseInitialData(l.watchURL.String())
	if err != nil {
		return err
	}
	l.continuationCode = continuationCode
	l.clientConfig = cfg
	return nil
}

func (l *ChatListenerOBSOLETE) listenLoop(ctx context.Context) (_err error) {
	logger.Debugf(ctx, "listenLoop")
	defer func() { logger.Debugf(ctx, "/listenLoop: %v", _err) }()
	consecutiveFailures := 0
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		msgs, newContinuation, _, err := l.scraper.FetchContinuationChat(l.continuationCode, l.clientConfig)
		switch {
		case err == nil:
			consecutiveFailures = 0
		case errors.Is(err, ytchat.ErrLiveStreamOver):
			return err
		default:
			consecutiveFailures++
			if consecutiveFailures >= maxConsecutiveFetchFailures {
				return fmt.Errorf(
					"giving up after %d consecutive fetch failures for %v; last error: %w",
					consecutiveFailures, l.videoID, err,
				)
			}
			logger.Errorf(
				ctx,
				"unable to get a continuation for %v (failure %d/%d): %v; refreshing and retrying in %v",
				l.videoID,
				consecutiveFailures,
				maxConsecutiveFetchFailures,
				err,
				chatFetchRetryInterval,
			)
			time.Sleep(chatFetchRetryInterval)
			if refreshErr := l.refreshContinuation(ctx); refreshErr != nil {
				if errors.Is(refreshErr, ytchat.ErrStreamNotLive) {
					return refreshErr
				}
				logger.Warnf(ctx, "unable to refresh continuation for %v (URL: %s): %v", l.videoID, l.watchURL, refreshErr)
			}
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
