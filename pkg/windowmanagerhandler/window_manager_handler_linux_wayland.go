//go:build linux && !android
// +build linux,!android

package windowmanagerhandler

import (
	"fmt"
)

func (wmh *WindowManagerHandler) initUsingWayland() error {
	return fmt.Errorf("support of Wayland is not implemented, yet")
}
