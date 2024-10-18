//go:build !linux
// +build !linux

package windowmanagerhandler

import (
	"context"
	"fmt"
)

type PlatformSpecificWindowManagerHandler struct{}
type WindowID struct{}
type PID struct{}
type UID struct{}

func (wmh *WindowManagerHandler) init(context.Context) error {
	return fmt.Errorf("the support of window manager handler for this platform is not implemented, yet")
}
