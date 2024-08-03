//go:build !with_libvlc
// +build !with_libvlc

package player

import (
	"context"
	"fmt"
	"time"
)

const SupportedLibVLC = false

type LibVLC struct{}

func NewLibVLC(ctx context.Context, title string) (*LibVLC, error) {
	return nil, fmt.Errorf("compiled without LibVLC")
}

func (*Manager) NewLibVLC(ctx context.Context, title string) (*LibVLC, error) {
	return NewLibVLC(ctx, title)
}

func (*LibVLC) OpenURL(
	ctx context.Context,
	link string,
) error {
	panic("compiled without LibVLC support")
}

func (*LibVLC) EndChan(
	ctx context.Context,
) (<-chan struct{}, error) {
	panic("compiled without LibVLC support")
}

func (*LibVLC) IsEnded(
	ctx context.Context,
) (bool, error) {
	panic("compiled without LibVLC support")
}

func (p *LibVLC) GetPosition(
	ctx context.Context,
) (time.Duration, error) {
	panic("compiled without LibVLC support")
}

func (p *LibVLC) GetLength(
	ctx context.Context,
) (time.Duration, error) {
	panic("compiled without LibVLC support")
}

func (p *LibVLC) ProcessTitle(
	ctx context.Context,
) (string, error) {
	panic("compiled without LibVLC support")
}

func (p *LibVLC) GetLink(
	ctx context.Context,
) (string, error) {
	panic("compiled without LibVLC support")
}

func (*LibVLC) SetSpeed(
	ctx context.Context,
	speed float64,
) error {
	panic("compiled without LibVLC support")
}

func (*LibVLC) SetPause(
	ctx context.Context,
	pause bool,
) error {
	panic("compiled without LibVLC support")
}

func (*LibVLC) Stop(
	ctx context.Context,
) error {
	panic("compiled without LibVLC support")
}

func (*LibVLC) Close(ctx context.Context) error {
	panic("compiled without LibVLC support")
}
