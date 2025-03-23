package youtube

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"net/url"
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

type ChatListener struct {
	videoID          string
	continuationCode string
	clientConfig     ytchat.YtCfg
	wg               sync.WaitGroup
	cancelFunc       context.CancelFunc
	messagesOutChan  chan streamcontrol.ChatMessage
}

func NewChatListener(
	ctx context.Context,
	videoID string,
) (*ChatListener, error) {
	if videoID == "" {
		return nil, fmt.Errorf("video ID is empty")
	}

	watchURL := ytWatchURL(videoID)

	continuationCode, cfg, err := ytchat.ParseInitialData(watchURL.String())
	if err != nil {
		return nil, fmt.Errorf("unable to fetch the initial data for chat messages retrieval (URL: %s): %w", watchURL, err)
	}

	ctx, cancelFunc := context.WithCancel(ctx)
	l := &ChatListener{
		videoID:          videoID,
		continuationCode: continuationCode,
		clientConfig:     cfg,
		cancelFunc:       cancelFunc,
		messagesOutChan:  make(chan streamcontrol.ChatMessage, 100),
	}
	l.wg.Add(1)
	observability.Go(ctx, func() {
		defer l.wg.Done()
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

func (l *ChatListener) listenLoop(ctx context.Context) (_err error) {
	logger.Debugf(ctx, "listenLoop")
	defer func() { logger.Debugf(ctx, "/listenLoop: %v", _err) }()
	for {
		msgs, newContinuation, err := ytchat.FetchContinuationChat(l.continuationCode, l.clientConfig)
		switch err {
		case nil:
		case ytchat.ErrLiveStreamOver:
			return err
		default:
			logger.Errorf(
				ctx,
				"unable to get a continuation for %v: %v; retrying in %v",
				l.videoID,
				chatFetchRetryInterval,
				err,
			)
			time.Sleep(chatFetchRetryInterval)
			continue
		}
		l.continuationCode = newContinuation

		for _, msg := range msgs {
			l.messagesOutChan <- streamcontrol.ChatMessage{
				CreatedAt: msg.Timestamp,
				UserID:    streamcontrol.ChatUserID(msg.AuthorName),
				Username:  msg.AuthorName,
				// TODO: find a way to extract the message ID,
				//       in the mean while we we use a soft key for that:
				MessageID: streamcontrol.ChatMessageID(fmt.Sprintf("%s/%s", msg.AuthorName, msg.Message)),
				Message:   msg.Message,
			}
		}
	}
}

func (h *ChatListener) Close() error {
	h.cancelFunc()
	return nil
}

func (h *ChatListener) MessagesChan() <-chan streamcontrol.ChatMessage {
	return h.messagesOutChan
}
