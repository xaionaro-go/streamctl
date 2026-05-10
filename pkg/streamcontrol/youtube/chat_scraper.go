package youtube

import (
	"time"

	ytchat "github.com/abhinavxd/youtube-live-chat-downloader/v2"
)

// chatScraper isolates the upstream youtube-live-chat-downloader calls
// that ChatListenerOBSOLETE relies on, so listenLoop and
// refreshContinuation can be exercised without hitting youtube.com.
// The production implementation is ytChatScraper; tests supply their
// own.
type chatScraper interface {
	FetchContinuationChat(
		continuation string,
		cfg ytchat.YtCfg,
	) ([]ytchat.ChatMessage, string, time.Duration, error)
	ParseInitialData(watchURL string) (string, ytchat.YtCfg, error)
}

// ytChatScraper is the production chatScraper: it forwards every method
// to the abhinavxd/youtube-live-chat-downloader package.
type ytChatScraper struct{}

func (ytChatScraper) FetchContinuationChat(
	continuation string,
	cfg ytchat.YtCfg,
) ([]ytchat.ChatMessage, string, time.Duration, error) {
	return ytchat.FetchContinuationChat(continuation, cfg)
}

func (ytChatScraper) ParseInitialData(watchURL string) (string, ytchat.YtCfg, error) {
	return ytchat.ParseInitialData(watchURL)
}
