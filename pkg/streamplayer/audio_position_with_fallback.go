package streamplayer

import (
	"context"
	"fmt"
	"time"

	"github.com/facebookincubator/go-belt/tool/logger"
	player "github.com/xaionaro-go/player/pkg/player/types"
)

// Source labels returned by audioPositionWithFallback so callers can log which
// clock backed a particular reading. Returning the source from the helper (vs.
// duplicating the audio-pts/time-pos query at the call site) keeps a single
// source of truth for fallback semantics.
const (
	audioPositionSourceAudioPTS = "audio-pts"
	audioPositionSourceTimePos  = "time-pos"
)

// audio-pts advances even when video stalls but is unavailable when the audio
// decoder hasn't reached STATUS_PLAYING (e.g. no audio track, slow decoder
// warmup); time-pos is the master playback clock used as fallback so a healthy
// stream is not mistaken for a dead player.
func audioPositionWithFallback(
	ctx context.Context,
	player player.Player,
) (_pos time.Duration, _source string, _err error) {
	pos, err := player.GetAudioPosition(ctx)
	if err == nil {
		return pos, audioPositionSourceAudioPTS, nil
	}
	logger.Tracef(ctx, "audioPositionWithFallback: audio-pts unavailable (%v), falling back to time-pos", err)
	pos, err = player.GetPosition(ctx)
	if err != nil {
		return 0, "", fmt.Errorf("unable to get any position (audio-pts and time-pos both failed): %w", err)
	}
	return pos, audioPositionSourceTimePos, nil
}
