package streamplayer

import (
	"context"
	"time"

	"github.com/facebookincubator/go-belt/tool/logger"
	player "github.com/xaionaro-go/player/pkg/player/types"
)

const (
	playerTries = 2
)

type RetryablePlayer struct {
	Player player.Player
}

func wrapPlayer(p player.Player) *RetryablePlayer {
	return &RetryablePlayer{
		Player: p,
	}
}

func retryA1R2[A0 any, R0 any](
	ctx context.Context,
	f func(A0) (R0, error),
	a0 A0,
) (R0, error) {
	var lastErr error
	for i := range playerTries {
		res, err := f(a0)
		if err == nil {
			return res, nil
		}
		logger.Errorf(ctx, "player: attempt %d failed: %v", i+1, err)
		lastErr = err
	}
	var zero R0
	return zero, lastErr
}

func retryA2R1[A0 any, A1 any](
	ctx context.Context,
	f func(A0, A1) error,
	a0 A0,
	a1 A1,
) error {
	var lastErr error
	for i := range playerTries {
		err := f(a0, a1)
		if err == nil {
			return nil
		}
		logger.Errorf(ctx, "player: attempt %d failed: %v", i+1, err)
		lastErr = err
	}
	return lastErr
}

func retryA4R1[A0 any, A1 any, A2 any, A3 any](
	ctx context.Context,
	f func(A0, A1, A2, A3) error,
	a0 A0,
	a1 A1,
	a2 A2,
	a3 A3,
) error {
	var lastErr error
	for i := range playerTries {
		err := f(a0, a1, a2, a3)
		if err == nil {
			return nil
		}
		logger.Errorf(ctx, "player: attempt %d failed: %v", i+1, err)
		lastErr = err
	}
	return lastErr
}

func retryA1R1[A0 any](
	ctx context.Context,
	f func(A0) error,
	a0 A0,
) error {
	var lastErr error
	for i := range playerTries {
		err := f(a0)
		if err == nil {
			return nil
		}
		logger.Errorf(ctx, "player: attempt %d failed: %v", i+1, err)
		lastErr = err
	}
	return lastErr
}

func (p *RetryablePlayer) ProcessTitle(ctx context.Context) (string, error) {
	return retryA1R2(ctx, p.Player.ProcessTitle, ctx)
}

func (p *RetryablePlayer) OpenURL(ctx context.Context, link string) error {
	return retryA2R1(ctx, p.Player.OpenURL, ctx, link)
}

func (p *RetryablePlayer) GetLink(ctx context.Context) (string, error) {
	return retryA1R2(ctx, p.Player.GetLink, ctx)
}

func (p *RetryablePlayer) EndChan(ctx context.Context) (<-chan struct{}, error) {
	return retryA1R2(ctx, p.Player.EndChan, ctx)
}

func (p *RetryablePlayer) IsEnded(ctx context.Context) (bool, error) {
	return retryA1R2(ctx, p.Player.IsEnded, ctx)
}

func (p *RetryablePlayer) GetPosition(ctx context.Context) (time.Duration, error) {
	return retryA1R2(ctx, p.Player.GetPosition, ctx)
}
func (p *RetryablePlayer) GetAudioPosition(ctx context.Context) (time.Duration, error) {
	return retryA1R2(ctx, p.Player.GetAudioPosition, ctx)
}

func (p *RetryablePlayer) GetLength(ctx context.Context) (time.Duration, error) {
	return retryA1R2(ctx, p.Player.GetLength, ctx)
}

func (p *RetryablePlayer) GetSpeed(ctx context.Context) (float64, error) {
	return retryA1R2(ctx, p.Player.GetSpeed, ctx)
}

func (p *RetryablePlayer) SetSpeed(ctx context.Context, speed float64) error {
	return retryA2R1(ctx, p.Player.SetSpeed, ctx, speed)
}

func (p *RetryablePlayer) GetPause(ctx context.Context) (bool, error) {
	return retryA1R2(ctx, p.Player.GetPause, ctx)
}

func (p *RetryablePlayer) SetPause(ctx context.Context, pause bool) error {
	return retryA2R1(ctx, p.Player.SetPause, ctx, pause)
}

func (p *RetryablePlayer) Seek(ctx context.Context, pos time.Duration, isRelative bool, quick bool) error {
	return retryA4R1(ctx, p.Player.Seek, ctx, pos, isRelative, quick)
}

func (p *RetryablePlayer) GetVideoTracks(ctx context.Context) (player.VideoTracks, error) {
	return retryA1R2(ctx, p.Player.GetVideoTracks, ctx)
}

func (p *RetryablePlayer) GetAudioTracks(ctx context.Context) (player.AudioTracks, error) {
	return retryA1R2(ctx, p.Player.GetAudioTracks, ctx)
}

func (p *RetryablePlayer) GetSubtitlesTracks(ctx context.Context) (player.SubtitlesTracks, error) {
	return retryA1R2(ctx, p.Player.GetSubtitlesTracks, ctx)
}

func (p *RetryablePlayer) SetVideoTrack(ctx context.Context, vid int64) error {
	return retryA2R1(ctx, p.Player.SetVideoTrack, ctx, vid)
}

func (p *RetryablePlayer) SetAudioTrack(ctx context.Context, aid int64) error {
	return retryA2R1(ctx, p.Player.SetAudioTrack, ctx, aid)
}

func (p *RetryablePlayer) SetSubtitlesTrack(ctx context.Context, sid int64) error {
	return retryA2R1(ctx, p.Player.SetSubtitlesTrack, ctx, sid)
}

func (p *RetryablePlayer) Stop(ctx context.Context) error {
	return retryA1R1(ctx, p.Player.Stop, ctx)
}

func (p *RetryablePlayer) Close(ctx context.Context) error {
	return retryA1R1(ctx, p.Player.Close, ctx)
}

func (p *RetryablePlayer) SetupForStreaming(ctx context.Context) error {
	return retryA1R1(ctx, p.Player.SetupForStreaming, ctx)
}

func (p *RetryablePlayer) GetCachedDuration(ctx context.Context) (time.Duration, error) {
	player, ok := p.Player.(interface {
		GetCachedDuration(ctx context.Context) (time.Duration, error)
	})
	if !ok {
		var zero time.Duration
		return zero, ErrNotImplemented{}
	}
	return retryA1R2(ctx, player.GetCachedDuration, ctx)
}

type ErrNotImplemented struct{}

func (e ErrNotImplemented) Error() string {
	return "not implemented"
}
