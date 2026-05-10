package youtube

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	ytchat "github.com/abhinavxd/youtube-live-chat-downloader/v2"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
)

// alwaysRejectingChatScraper makes every FetchContinuationChat return a
// 400-style error and lets ParseInitialData "succeed" returning an
// empty continuation. Mirrors the wedge observed on areion 2026-05-08:
// YouTube kept rejecting the continuation while ParseInitialData kept
// handing it back unchanged.
type alwaysRejectingChatScraper struct {
	fetchCalls   atomic.Int32
	refreshCalls atomic.Int32
}

func (s *alwaysRejectingChatScraper) FetchContinuationChat(
	string,
	ytchat.YtCfg,
) ([]ytchat.ChatMessage, string, time.Duration, error) {
	s.fetchCalls.Add(1)
	return nil, "", 0, errors.New("status code: 400: invalid argument")
}

func (s *alwaysRejectingChatScraper) ParseInitialData(string) (string, ytchat.YtCfg, error) {
	s.refreshCalls.Add(1)
	return "", ytchat.YtCfg{}, nil
}

// everyFourthOKChatScraper succeeds on every 4th FetchContinuationChat
// call. Used to verify the failure counter resets on success so
// transient blips never reach the bail threshold.
type everyFourthOKChatScraper struct {
	calls atomic.Int32
}

func (s *everyFourthOKChatScraper) FetchContinuationChat(
	string,
	ytchat.YtCfg,
) ([]ytchat.ChatMessage, string, time.Duration, error) {
	n := s.calls.Add(1)
	if n%4 == 0 {
		return nil, "newContinuation", 0, nil
	}
	return nil, "", 0, errors.New("status code: 400")
}

func (s *everyFourthOKChatScraper) ParseInitialData(string) (string, ytchat.YtCfg, error) {
	return "", ytchat.YtCfg{}, nil
}

// TestChatListenerOBSOLETE_BailsAfterConsecutiveFetchFailures pins the
// behavior that fixes the wedge: 6452 errors / 8009 attempts in 3h on a
// stale videoID because listenLoop had no cap on consecutive fetch
// failures when refresh also "succeeded". The bail lets the upstream
// discoverAndScrapeLoop re-discover the broadcast.
//
// Falsification: revert the bail-out branch and the listener loops
// forever; the test then exits only via the test ctx timeout and
// fetchCalls grows past maxConsecutiveFetchFailures.
func TestChatListenerOBSOLETE_BailsAfterConsecutiveFetchFailures(t *testing.T) {
	scraper := &alwaysRejectingChatScraper{}
	l := &ChatListenerOBSOLETE{
		videoID:         "deadVideo",
		watchURL:        ytWatchURL("deadVideo"),
		messagesOutChan: make(chan streamcontrol.Event, 1),
		scraper:         scraper,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	start := time.Now()
	err := l.listenLoop(ctx)
	elapsed := time.Since(start)

	require.Error(t, err, "listenLoop must return when fetch keeps failing")
	require.NotErrorIs(t, err, context.DeadlineExceeded,
		"listenLoop must bail on its own, not via test ctx (fetchCalls=%d, elapsed=%v)",
		scraper.fetchCalls.Load(), elapsed)
	require.Equal(t, int32(maxConsecutiveFetchFailures), scraper.fetchCalls.Load(),
		"expected exactly %d fetch attempts before bail, got %d",
		maxConsecutiveFetchFailures, scraper.fetchCalls.Load())
	require.Equal(t, int32(maxConsecutiveFetchFailures-1), scraper.refreshCalls.Load(),
		"refresh runs only on non-final failures; got %d", scraper.refreshCalls.Load())
	require.Less(t, elapsed, time.Duration(maxConsecutiveFetchFailures+1)*chatFetchRetryInterval,
		"bail should happen within ~%d sleep intervals, took %v", maxConsecutiveFetchFailures, elapsed)
}

// staleContinuationScraper drives FetchContinuationChat through the real
// upstream library (so the test exercises the actual HTTP/JSON path
// against the live_chat endpoint) but stubs ParseInitialData to return
// an unchanged continuation token. Mirrors the production wedge: the
// page parses fine, hands back the same dead token, YouTube keeps
// rejecting it.
type staleContinuationScraper struct {
	ytChatScraper // production FetchContinuationChat: real HTTP POST.
}

func (staleContinuationScraper) ParseInitialData(string) (string, ytchat.YtCfg, error) {
	return "stale-continuation-token", ytchat.YtCfg{}, nil
}

// youtube400ResponseBody is the verbatim body shape areion logged on
// 2026-05-08 when YouTube rejected continuation requests for the ended
// broadcast sYx8sCf_PiU.
const youtube400ResponseBody = `{
  "error": {
    "code": 400,
    "message": "Request contains an invalid argument.",
    "errors": [
      {
        "message": "Request contains an invalid argument.",
        "domain": "global",
        "reason": "badRequest"
      }
    ],
    "status": "INVALID_ARGUMENT"
  }
}`

// TestChatListenerOBSOLETE_BailsOnRealYouTube400Response is a
// protocol-level repro of the production wedge. It stands up an httptest
// server that returns the exact 400 INVALID_ARGUMENT body YouTube
// returned to areion on 2026-05-08, points the upstream library at it,
// and runs listenLoop end-to-end through real HTTP/JSON
// serialization. Without the bail-out branch the loop hangs (test
// runner kills it at the ctx timeout); with the bail it returns within
// ~chatFetchRetryInterval × maxConsecutiveFetchFailures.
//
// Falsification: comment out the bail-out branch in listenLoop and the
// test exits via context.DeadlineExceeded rather than the wrapped 400
// error, with fetchCount > maxConsecutiveFetchFailures.
func TestChatListenerOBSOLETE_BailsOnRealYouTube400Response(t *testing.T) {
	var fetchCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fetchCount.Add(1)
		w.Header().Set("Content-Type", "application/json; charset=UTF-8")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(youtube400ResponseBody))
	}))
	t.Cleanup(server.Close)

	origURL := ytchat.LIVE_CHAT_URL
	ytchat.LIVE_CHAT_URL = server.URL
	t.Cleanup(func() { ytchat.LIVE_CHAT_URL = origURL })

	l := &ChatListenerOBSOLETE{
		videoID:          "sYx8sCf_PiU",
		watchURL:         ytWatchURL("sYx8sCf_PiU"),
		continuationCode: "stale-continuation-token",
		clientConfig:     ytchat.YtCfg{},
		messagesOutChan:  make(chan streamcontrol.Event, 1),
		scraper:          staleContinuationScraper{},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	start := time.Now()
	err := l.listenLoop(ctx)
	elapsed := time.Since(start)

	require.Error(t, err, "listenLoop must return an error")
	require.NotErrorIs(t, err, context.DeadlineExceeded,
		"listenLoop must bail on its own, not via test ctx (fetchCount=%d, elapsed=%v)",
		fetchCount.Load(), elapsed)
	require.ErrorContains(t, err, "consecutive fetch failures",
		"bail message should reference the consecutive-failure counter")
	require.ErrorContains(t, err, "status code: 400",
		"wrapped error should preserve the upstream 400")
	require.ErrorContains(t, err, "INVALID_ARGUMENT",
		"wrapped error should preserve the YouTube error status")
	require.Equal(t, int32(maxConsecutiveFetchFailures), fetchCount.Load(),
		"server should have received exactly %d POSTs before bail",
		maxConsecutiveFetchFailures)
	require.Less(t, elapsed, time.Duration(maxConsecutiveFetchFailures+1)*chatFetchRetryInterval,
		"bail should land within ~%d sleep intervals; took %v",
		maxConsecutiveFetchFailures, elapsed)
}

// TestChatListenerOBSOLETE_ResetsCounterOnFetchSuccess ensures transient
// failures do not accumulate across successful fetches.
//
// Falsification: skip the consecutiveFailures=0 reset on success and the
// listener bails on a fail-fail-fail-success pattern, surfacing as a
// non-ctx error before the test ctx fires.
func TestChatListenerOBSOLETE_ResetsCounterOnFetchSuccess(t *testing.T) {
	scraper := &everyFourthOKChatScraper{}
	l := &ChatListenerOBSOLETE{
		videoID:         "test",
		watchURL:        ytWatchURL("test"),
		messagesOutChan: make(chan streamcontrol.Event, 16),
		scraper:         scraper,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := l.listenLoop(ctx)
	require.ErrorIs(t, err, context.DeadlineExceeded,
		"loop should exit via ctx timeout (counter reset on success), got: %v (fetchCalls=%d)",
		err, scraper.calls.Load())
}
