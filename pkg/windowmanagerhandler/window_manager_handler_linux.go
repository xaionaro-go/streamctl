//go:build linux
// +build linux

package windowmanagerhandler

import (
	"context"
	"os"
)

type WindowID uint64
type PID int // using the same underlying type as `os` does
type UID int // using the same underlying type as `os` does

type XWMOrWaylandWM interface {
	WindowFocusChangeChan(ctx context.Context) <-chan WindowFocusChange
}

type PlatformSpecificWindowManagerHandler struct {
	XWMOrWaylandWM
}

func (wmh *WindowManagerHandler) init() error {
	if os.Getenv("DISPLAY") != "" {
		return wmh.initUsingXServer()
	} else {
		return wmh.initUsingWayland()
	}
}
