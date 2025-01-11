package streampanel

import (
	"context"
	"fmt"
	"image/color"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/obs-grpc-proxy/protobuf/go/obs_grpc"
	"github.com/xaionaro-go/observability"
	gconsts "github.com/xaionaro-go/streamctl/pkg/consts"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/kick"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/obs"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/twitch"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/youtube"
	"github.com/xaionaro-go/streamctl/pkg/xcontext"
	"github.com/xaionaro-go/streamctl/pkg/xruntime"
)

type streamStartedWindowStreamStatus struct {
	*fyne.Container
	LastUpdatedAt time.Time
}

type streamStartedWindow struct {
	*Panel
	streamStatus map[streamcontrol.PlatformName]*streamStartedWindowStreamStatus
}

func (p *Panel) openStreamStartedWindow(
	ctx context.Context,
) {
	w := &streamStartedWindow{
		Panel:        p,
		streamStatus: make(map[streamcontrol.PlatformName]*streamStartedWindowStreamStatus),
	}
	for _, platID := range []streamcontrol.PlatformName{
		obs.ID,
		twitch.ID,
		kick.ID,
		youtube.ID,
	} {
		dst := &streamStartedWindowStreamStatus{
			Container: container.NewStack(),
		}
		w.streamStatus[platID] = dst
		w.setStreamStatusColorAndText(ctx, dst, bgColorGray, string(platID))
	}
	w.open(ctx)
}

var (
	bgColorRed    = color.NRGBA{R: 128, G: 0, B: 0, A: 255}
	bgColorYellow = color.NRGBA{R: 128, G: 128, B: 0, A: 255}
	bgColorGreen  = color.NRGBA{R: 0, G: 128, B: 0, A: 255}
	bgColorBlack  = color.NRGBA{R: 0, G: 0, B: 0, A: 255}
	bgColorGray   = color.NRGBA{R: 128, G: 128, B: 128, A: 255}
)

func (w *streamStartedWindow) setStreamStatus(
	ctx context.Context,
	platID streamcontrol.PlatformName,
	src *streamStatus,
) {
	logger.Debugf(ctx, "setStreamStatus(ctx, '%s', src)", platID)
	defer logger.Debugf(ctx, "/setStreamStatus(ctx, '%s', src)", platID)

	if src == nil {
		logger.Debugf(ctx, "no stream status provided for '%v'", platID)
		return
	}
	dst := w.streamStatus[platID]
	if dst == nil {
		logger.Warnf(ctx, "there is no widget to display the stream status of '%v'", platID)
		return
	}

	if !dst.LastUpdatedAt.Before(src.LastChangedAt) {
		logger.Debugf(ctx, "%v before %v", dst, src.LastChangedAt)
		return
	}

	if !src.BackendIsEnabled {
		w.setStreamStatusColorAndText(ctx, dst, bgColorBlack, string(platID))
		return
	}

	if src.BackendError != nil {
		w.setStreamStatusColorAndText(ctx, dst, bgColorRed, string(platID))
		return
	}

	if !src.IsActive {
		w.setStreamStatusColorAndText(ctx, dst, bgColorYellow, string(platID))
		return
	}

	w.setStreamStatusColorAndText(ctx, dst, bgColorGreen, string(platID))
}

func (w *streamStartedWindow) setStreamStatusColorAndText(
	ctx context.Context,
	dst *streamStartedWindowStreamStatus,
	bgColor color.Color,
	text string,
) {
	logger.Debugf(ctx, "setStreamStatusColorAndText(dst, %v, '%s')", bgColor, text)
	dst.LastUpdatedAt = time.Now()

	background := canvas.NewRectangle(bgColor)
	textWidget := canvas.NewText(text, color.White)
	textWidget.TextSize = 16

	dst.Container.RemoveAll()
	dst.Container.Add(background)
	dst.Container.Add(textWidget)
	dst.Refresh()
}

func (w *streamStartedWindow) updateStreamStatusLoop(
	ctx context.Context,
) {
	logger.Debugf(ctx, "updateStreamStatusLoop")
	defer logger.Debugf(ctx, "/updateStreamStatusLoop")

	var wg sync.WaitGroup

	wg.Add(1)
	observability.Go(ctx, func() {
		defer wg.Done()
		t := time.NewTicker(time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				w.updateStreamStatus(ctx)
			}
		}
	})

	wg.Add(1)
	observability.Go(ctx, func() {
		defer wg.Done()
		t := time.NewTicker(time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				w.renderStreamStatus(ctx)
			}
		}
	})

	wg.Wait()
}

func (w *streamStartedWindow) renderStreamStatus(
	ctx context.Context,
) {
	logger.Debugf(ctx, "renderStreamStatus")
	defer logger.Debugf(ctx, "/renderStreamStatus")
	w.Panel.streamStatusLocker.Do(ctx, func() {
		for platID, src := range w.Panel.streamStatus {
			w.setStreamStatus(ctx, platID, src)
		}
	})
}

func (w *streamStartedWindow) open(
	ctx context.Context,
) {
	obsServer, obsServerClose, err := w.StreamD.OBS(ctx)
	if obsServerClose != nil {
		defer obsServerClose()
	}
	if err != nil {
		w.ReportError(fmt.Errorf("unable to initialize a client to OBS: %w", err))
		return
	}

	sceneListResp, err := obsServer.GetSceneList(ctx, &obs_grpc.GetSceneListRequest{})
	if err != nil {
		w.ReportError(err)
		return
	}

	ctx, cancelFn := context.WithCancel(ctx)
	observability.Go(ctx, func() {
		w.updateStreamStatusLoop(ctx)
	})

	w.streamStartedLocker.Do(ctx, func() {
		if w.streamStartedWindow != nil {
			w.streamStartedWindow.Close()
			w.streamStartedWindow = nil
		}

		w.streamStartedWindow = w.app.NewWindow(gconsts.AppName + ": Stream started")
		w.streamStartedWindow.SetOnClosed(func() {
			cancelFn()
		})

		sceneButtons := container.NewGridWithColumns(3)
		for _, scene := range sceneListResp.Scenes {
			sceneButtons.Add(widget.NewButton(scene.GetSceneName(), func() {
				switchScene := func() {
					obsServer, obsServerClose, err := w.StreamD.OBS(ctx)
					if obsServerClose != nil {
						defer obsServerClose()
					}
					if err != nil {
						w.ReportError(fmt.Errorf("unable to initialize a client to OBS: %w", err))
						return
					}
					_, err = obsServer.SetCurrentProgramScene(ctx, &obs_grpc.SetCurrentProgramSceneRequest{
						SceneName: scene.SceneName,
						SceneUUID: scene.SceneUUID,
					})
					if err != nil {
						w.ReportError(fmt.Errorf("unable to switch the Scene to %#+v: %w", scene, err))
						return
					}
				}

				if !xruntime.IsSmartphone {
					switchScene()
					return
				}

				msg := fmt.Sprintf("Switch scene to %s?", scene.GetSceneName())
				dialog.NewConfirm(gconsts.AppName+": "+msg, msg, func(b bool) {
					if !b {
						return
					}
					switchScene()
				}, w.streamStartedWindow).Show()
			}))
		}
		w.streamStartedWindow.SetContent(
			container.NewBorder(
				container.NewHBox(
					w.streamStatus[obs.ID].Container,
					w.streamStatus[twitch.ID].Container,
					w.streamStatus[kick.ID].Container,
					w.streamStatus[youtube.ID].Container,
				),
				widget.NewButtonWithIcon("Open dashboard", theme.ComputerIcon(), func() {
					w.streamStartedWindow.Close()
					w.focusDashboardWindow(xcontext.DetachDone(ctx))
				}),
				nil,
				nil,
				container.NewVBox(
					widget.NewLabel("Switch OBS scene:"),
					sceneButtons,
				),
			),
		)
		w.streamStartedWindow.Show()
	})
}
