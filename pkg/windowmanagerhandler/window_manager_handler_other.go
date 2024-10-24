//go:build !linux
// +build !linux

package windowmanagerhandler

import (
	"context"
)

type PlatformSpecificWindowManagerHandler struct{}
type WindowID uint64
type PID uint64
type UID uint64

func (wmh *WindowManagerHandler) init() error {
	return nil //fmt.Errorf("the support of window manager handler for this platform is not implemented, yet")
}

func (PlatformSpecificWindowManagerHandler) Close() error {
	return nil
}

func (PlatformSpecificWindowManagerHandler) WindowFocusChangeChan(ctx context.Context) <-chan WindowFocusChange {
	return nil
}
