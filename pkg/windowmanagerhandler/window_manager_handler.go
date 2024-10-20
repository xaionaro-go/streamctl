package windowmanagerhandler

import (
	"context"
	"fmt"
)

type WindowManagerHandler struct {
	PlatformSpecificWindowManagerHandler
}

func New() (*WindowManagerHandler, error) {
	wmh := &WindowManagerHandler{}
	if err := wmh.init(); err != nil {
		return nil, fmt.Errorf("unable to initialize a window manager handler: %w", err)
	}
	return wmh, nil
}

func (wmh *WindowManagerHandler) WindowFocusChangeChan(ctx context.Context) <-chan WindowFocusChange {
	return wmh.PlatformSpecificWindowManagerHandler.WindowFocusChangeChan(ctx)
}

func (wmh *WindowManagerHandler) Close() error {
	return wmh.PlatformSpecificWindowManagerHandler.Close()
}

type WindowFocusChange struct {
	WindowID    *WindowID
	WindowTitle *string
	ProcessID   *PID
	UserID      *UID
	ProcessName *string
}
