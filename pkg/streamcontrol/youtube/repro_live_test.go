package youtube

import (
	"net"
	"os"
	"testing"
	"time"

	ytchat "github.com/abhinavxd/youtube-live-chat-downloader/v2"
	"github.com/stretchr/testify/require"
)

// TestRepro_LiveYouTubeWedgeChain hits real youtube.com to verify the
// theorised wedge-trigger mechanism end-to-end:
//
//  1. ParseInitialData on the ended-broadcast watch page returns a
//     continuation token without error (i.e. refreshContinuation
//     "succeeds" in production).
//  2. FetchContinuationChat with that continuation returns HTTP 400
//     INVALID_ARGUMENT (the very response areion saw 6452 times).
//
// Together (1)+(2) explain why ChatListenerOBSOLETE.listenLoop wedged
// before the bail-out: refresh kept handing back a token that the chat
// API kept rejecting, neither path produced ErrLiveStreamOver or
// ErrStreamNotLive that would let the loop exit.
//
// This test makes outbound HTTPS to youtube.com; CI sets STREAMD_NO_NET=1
// to skip. Re-run manually for diagnosis.
func TestRepro_LiveYouTubeWedgeChain(t *testing.T) {
	if os.Getenv("STREAMD_NO_NET") != "" {
		t.Skip("STREAMD_NO_NET set; skipping live-network repro")
	}
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	if _, err := dialer.Dial("tcp", "www.youtube.com:443"); err != nil {
		t.Skipf("youtube.com unreachable: %v", err)
	}

	const videoID = "sYx8sCf_PiU"
	watchURL := "https://www.youtube.com/watch?v=" + videoID

	continuation, cfg, err := ytchat.ParseInitialData(watchURL)
	t.Logf("ParseInitialData err=%v continuation_len=%d", err, len(continuation))
	require.NoError(t, err,
		"ParseInitialData must succeed for the wedge-trigger to occur; got %v", err)
	require.NotEmpty(t, continuation,
		"refresh handing back an empty continuation would short-circuit the loop")

	_, _, _, fetchErr := ytchat.FetchContinuationChat(continuation, cfg)
	t.Logf("FetchContinuationChat err=%v", fetchErr)
	require.Error(t, fetchErr,
		"the wedge requires the chat API to reject the refreshed continuation")
	require.Contains(t, fetchErr.Error(), "400",
		"expected HTTP 400 (matches areion log); got %v", fetchErr)

	// Sanity check that the wrapped body matches the production shape, so
	// the in-tree httptest fixture stays anchored to reality.
	require.Contains(t, fetchErr.Error(), "INVALID_ARGUMENT",
		"expected INVALID_ARGUMENT in the body; got %v", fetchErr)
}
