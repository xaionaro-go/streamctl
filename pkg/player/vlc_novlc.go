//go:build !with_libvlc
// +build !with_libvlc

package player

import (
	"fmt"
)

const SupportedVLC = false

type VLC struct{}

func NewVLC(title string) (*VLC, error) {
	return nil, fmt.Errorf("compiled without VLC")
}

func (*Manager) NewVLC(title string) (*VLC, error) {
	return NewVLC(title)
}

func (*VLC) OpenURL(link string) error {
	panic("compiled without VLC support")
}

func (*VLC) EndChan() <-chan struct{} {
	panic("compiled without VLC support")
}

func (*VLC) IsEnded() bool {
	panic("compiled without VLC support")
}

func (*VLC) SetSpeed(speed float64) error {
	panic("compiled without VLC support")
}

func (*VLC) SetPause(pause bool) error {
	panic("compiled without VLC support")
}

func (*VLC) Stop() error {
	panic("compiled without VLC support")
}

func (*VLC) Close() error {
	panic("compiled without VLC support")
}
