//go:build linux
// +build linux

package windowmanagerhandler

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/BurntSushi/xgb/xproto"
	"github.com/BurntSushi/xgbutil"
	"github.com/BurntSushi/xgbutil/ewmh"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/observability"
)

type XWindowManagerHandler struct {
	*xgbutil.XUtil
}

func (wmh *WindowManagerHandler) initUsingXServer() error {
	x, err := xgbutil.NewConn()
	if err != nil {
		return fmt.Errorf("unable to connect to X-server using DISPLAY '%s': %w", os.Getenv("DISPLAY"), err)
	}
	wmh.XWMOrWaylandWM = &XWindowManagerHandler{
		XUtil: x,
	}
	return nil
}

func (wmh *XWindowManagerHandler) WindowFocusChangeChan(ctx context.Context) <-chan WindowFocusChange {
	logger.Debugf(ctx, "WindowFocusChangeChan")
	ch := make(chan WindowFocusChange)

	observability.Go(ctx, func() {
		defer logger.Debugf(ctx, "/WindowFocusChangeChan")
		defer func() {
			close(ch)
		}()

		prevClientID := xproto.Window(0)
		t := time.NewTicker(200 * time.Millisecond)
		defer t.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}

			clientID, err := ewmh.ActiveWindowGet(wmh.XUtil)
			if err != nil {
				logger.Errorf(ctx, "unable to get active window: %w", err)
				continue
			}

			if clientID == prevClientID {
				continue
			}
			prevClientID = clientID

			name, err := ewmh.WmNameGet(wmh.XUtil, clientID)
			if err != nil {
				logger.Errorf(ctx, "unable to get the name of the active window (%d): %w", clientID, err)
				continue
			}

			ch <- WindowFocusChange{
				WindowID:    WindowID(clientID),
				WindowTitle: name,
			}
		}
	})

	return ch
}
