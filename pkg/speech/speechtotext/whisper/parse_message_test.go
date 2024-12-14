package whisper

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/streamctl/pkg/speech"
)

func TestParseMessage(t *testing.T) {
	ctx := context.Background()
	message := `1592 1912 Well,
2172 3752 you work, I hope so
12028 14168 Yes, it seems to work, but
14168 15388 the translation suffers a little`
	_, words, err := parseMessage(ctx, message)
	require.NoError(t, err)
	require.Equal(t,
		[]speech.TranscriptWord{
			{
				StartTime:  1592 * time.Millisecond,
				EndTime:    1912 * time.Millisecond,
				Text:       "Well,",
				Confidence: 0.5,
			},
			{
				StartTime:  2172 * time.Millisecond,
				EndTime:    3752 * time.Millisecond,
				Text:       "you work, I hope so",
				Confidence: 0.5,
			},
			{
				StartTime:  12028 * time.Millisecond,
				EndTime:    14168 * time.Millisecond,
				Text:       "Yes, it seems to work, but",
				Confidence: 0.5,
			},
			{
				StartTime:  14168 * time.Millisecond,
				EndTime:    15388 * time.Millisecond,
				Text:       "the translation suffers a little",
				Confidence: 0.5,
			},
		}, words)
}
