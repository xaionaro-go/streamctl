package streamplayer

import (
	"context"
	"fmt"
	"time"

	"github.com/facebookincubator/go-belt/tool/logger"
	player "github.com/xaionaro-go/player/pkg/player/types"
)

// audio-pts advances even when video stalls but is unavailable when the audio
// decoder hasn't reached STATUS_PLAYING (e.g. no audio track, slow decoder
// warmup); time-pos is the master playback clock used as fallback so a healthy
// stream is not mistaken for a dead player.
func audioPositionWithFallback(ctx context.Context, player player.Player) (time.Duration, error) {
	pos, err := player.GetAudioPosition(ctx)
	if err == nil {
		return pos, nil
	}
	logger.Tracef(ctx, "audioPositionWithFallback: audio-pts unavailable (%v), falling back to time-pos", err)
	pos, err = player.GetPosition(ctx)
	if err != nil {
		return 0, fmt.Errorf("unable to get any position (audio-pts and time-pos both failed): %w", err)
	}
	return pos, nil
}
