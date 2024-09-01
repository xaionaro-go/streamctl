package types

import (
	"context"
	"time"
)

type Player interface {
	ProcessTitle(ctx context.Context) (string, error)
	OpenURL(ctx context.Context, link string) error
	GetLink(ctx context.Context) (string, error)
	EndChan(ctx context.Context) (<-chan struct{}, error)
	IsEnded(ctx context.Context) (bool, error)
	GetPosition(ctx context.Context) (time.Duration, error)
	GetLength(ctx context.Context) (time.Duration, error)
	SetSpeed(ctx context.Context, speed float64) error
	SetPause(ctx context.Context, pause bool) error
	Stop(ctx context.Context) error
	Close(ctx context.Context) error
	SetupForStreaming(ctx context.Context) error
}

type PlayerCommon struct {
	Title string
}

func (p PlayerCommon) ProcessTitle(
	ctx context.Context,
) (string, error) {
	return p.Title, nil
}
