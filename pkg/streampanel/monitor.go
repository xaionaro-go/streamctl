package streampanel

import (
	"context"
	"fmt"
	"runtime"
	"sort"
	"sync"

	"image"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/anthonynsimon/bild/adjust"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/obs-grpc-proxy/pkg/obsgrpcproxy"
	"github.com/xaionaro-go/obs-grpc-proxy/protobuf/go/obs_grpc"
	"github.com/xaionaro-go/streamctl/pkg/observability"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/obs"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/twitch"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/youtube"
	streamdconsts "github.com/xaionaro-go/streamctl/pkg/streamd/consts"
	"github.com/xaionaro-go/streamctl/pkg/streampanel/consts"
)

func (p *Panel) startMonitorPage(
	ctx context.Context,
) {
	logger.Debugf(ctx, "startMonitorPage")
	defer logger.Debugf(ctx, "/startMonitorPage")

	observability.Go(ctx, func() {
		p.updateMonitorPageImages(ctx)
		p.updateMonitorPageStreamStatus(ctx)

		observability.Go(ctx, func() {
			t := time.NewTicker(200 * time.Millisecond)
			for {
				select {
				case <-ctx.Done():
					return
				case <-t.C:
				}

				p.updateMonitorPageImages(ctx)
			}
		})

		observability.Go(ctx, func() {
			t := time.NewTicker(2 * time.Second)
			for {
				select {
				case <-ctx.Done():
					return
				case <-t.C:
				}

				p.updateMonitorPageStreamStatus(ctx)
			}
		})
	})
}

func (p *Panel) updateMonitorPageImages(
	ctx context.Context,
) {
	logger.Tracef(ctx, "updateMonitorPageImages")
	defer logger.Tracef(ctx, "/updateMonitorPageImages")

	p.monitorLocker.Lock()
	defer p.monitorLocker.Unlock()

	var winSize fyne.Size
	var orientation fyne.DeviceOrientation
	switch runtime.GOOS {
	default:
		winSize = p.monitorPage.Size()
		orientation = p.app.Driver().Device().Orientation()
	}
	lastWinSize := p.monitorLastWinSize
	lastOrientation := p.monitorLastOrientation
	p.monitorLastWinSize = winSize
	p.monitorLastOrientation = orientation

	if lastWinSize != winSize {
		logger.Debugf(ctx, "window size changed %#+v -> %#+v", lastWinSize, winSize)
	}

	var wg sync.WaitGroup

	wg.Add(1)
	observability.Go(ctx, func() {
		defer wg.Done()
		img, changed, err := p.getImage(ctx, consts.ImageScreenshot)
		if err != nil {
			logger.Error(ctx, err)
			return
		}
		if !changed && lastWinSize == winSize && lastOrientation == orientation {
			return
		}
		logger.Tracef(ctx, "updating the screenshot image: %v %#+v %#+v", changed, lastWinSize, winSize)
		//img = imgFitTo(img, image.Point{X: int(winSize.Width), Y: int(winSize.Height)})
		img = imgFillTo(ctx, img, image.Point{X: int(winSize.Width), Y: int(winSize.Height)}, alignStart, alignStart)
		img = adjust.Brightness(img, -0.5)
		imgFyne := canvas.NewImageFromImage(img)
		imgFyne.FillMode = canvas.ImageFillContain
		logger.Tracef(ctx, "screenshot image size: %#+v", img.Bounds().Size())

		p.screenshotContainer.Objects = p.screenshotContainer.Objects[:0]
		p.screenshotContainer.Objects = append(p.screenshotContainer.Objects, imgFyne)
		p.screenshotContainer.Refresh()
	})

	wg.Add(1)
	observability.Go(ctx, func() {
		defer wg.Done()
		img, changed, err := p.getImage(ctx, streamdconsts.ImageChat)
		if err != nil {
			logger.Error(ctx, err)
			return
		}
		if !changed && lastWinSize == winSize && lastOrientation == orientation {
			return
		}
		logger.Tracef(ctx, "updating the chat image: %v %#+v %#+v", changed, lastWinSize, winSize)
		//img = imgFitTo(img, image.Point{X: int(winSize.Width), Y: int(winSize.Height)})
		img = imgFillTo(ctx, img, image.Point{X: int(winSize.Width), Y: int(winSize.Height)}, alignStart, alignEnd)
		imgFyne := canvas.NewImageFromImage(img)
		imgFyne.FillMode = canvas.ImageFillContain
		logger.Tracef(ctx, "chat image size: %#+v", img.Bounds().Size())

		p.chatContainer.Objects = p.chatContainer.Objects[:0]
		p.chatContainer.Objects = append(p.chatContainer.Objects, imgFyne)
		p.chatContainer.Refresh()
	})

	wg.Add(1)
	observability.Go(ctx, func() {
		defer wg.Done()
		img, changed, err := p.getImage(ctx, streamdconsts.ImageMonitorScreenHighQualityForeground)
		if err != nil {
			logger.Error(ctx, err)
			return
		}
		if !changed && lastWinSize == winSize && lastOrientation == orientation {
			return
		}
		logger.Tracef(ctx, "updating the screen fg HQ image: %v %#+v %#+v", changed, lastWinSize, winSize)
		img = imgRotateFillTo(ctx, img, image.Point{X: int(winSize.Width), Y: int(winSize.Height)}, alignStart, alignStart)
		imgFyne := canvas.NewImageFromImage(img)
		imgFyne.FillMode = canvas.ImageFillContain
		logger.Tracef(ctx, "screen fg HQ image size: %#+v", img.Bounds().Size())

		p.monitorFgHQContainer.Objects = p.monitorFgHQContainer.Objects[:0]
		p.monitorFgHQContainer.Objects = append(p.monitorFgHQContainer.Objects, imgFyne)
		p.monitorFgHQContainer.Refresh()
	})
}

func (p *Panel) updateMonitorPageStreamStatus(
	ctx context.Context,
) {
	logger.Tracef(ctx, "updateMonitorPageStreamStatus")
	defer logger.Tracef(ctx, "/updateMonitorPageStreamStatus")

	var wg sync.WaitGroup
	for _, platID := range []streamcontrol.PlatformName{
		obs.ID,
		youtube.ID,
		twitch.ID,
	} {
		wg.Add(1)
		observability.Go(ctx, func() {
			defer wg.Done()

			dst := p.streamStatus[platID]

			ok, err := p.StreamD.IsBackendEnabled(ctx, platID)
			if err != nil {
				logger.Error(ctx, err)
				dst.Importance = widget.LowImportance
				dst.SetText("error")
				return
			}

			if !ok {
				dst.SetText("disabled")
				return
			}

			streamStatus, err := p.StreamD.GetStreamStatus(ctx, platID)
			if err != nil {
				logger.Error(ctx, err)
				dst.SetText("error")
				return
			}

			if !streamStatus.IsActive {
				dst.Importance = widget.DangerImportance
				dst.SetText("stopped")
				return
			}
			dst.Importance = widget.SuccessImportance
			if streamStatus.StartedAt != nil {
				duration := time.Since(*streamStatus.StartedAt)
				dst.SetText(duration.Truncate(time.Second).String())
			} else {
				dst.SetText("started")
			}
		})
	}

	wg.Wait()
}

func (p *Panel) newMonitorSettingsWindow(ctx context.Context) {
	w := p.app.NewWindow("Monitor settings")
	resizeWindow(w, fyne.NewSize(1500, 1000))

	cfg, err := p.StreamD.GetConfig(ctx)
	if err != nil {
		p.DisplayError(fmt.Errorf("unable to get the current config: %w", err))
	}

	obsServer, obsServerClose, err := p.StreamD.OBS(ctx)
	if err != nil {
		p.DisplayError(fmt.Errorf("unable to init a connection to OBS: %w", err))
		return
	}
	if obsServerClose != nil {
		defer obsServerClose()
	}

	resp, err := obsServer.GetSceneList(ctx, &obs_grpc.GetSceneListRequest{})
	if err != nil {
		p.DisplayError(fmt.Errorf("unable to get the list of available scenes in OBS: %w", err))
		return
	}

	var sourceNames []string
	sourceNameIsSet := map[string]struct{}{}
	for _, _scene := range resp.Scenes {
		scene, err := obsgrpcproxy.FromAbstractObject[map[string]any](_scene)
		if err != nil {
			p.DisplayError(fmt.Errorf("unable to convert scene info: %w", err))
			return
		}
		logger.Debugf(ctx, "scene info: %#+v", scene)
		sceneName, _ := scene["sceneName"].(string)
		resp, err := obsServer.GetSceneItemList(ctx, &obs_grpc.GetSceneItemListRequest{
			SceneName: &sceneName,
		})
		if err != nil {
			p.DisplayError(fmt.Errorf("unable to get the list of items of scene '%s': %w", sceneName, err))
			return
		}
		for _, item := range resp.SceneItems {
			source, err := obsgrpcproxy.FromAbstractObject[map[string]any](item)
			if err != nil {
				p.DisplayError(fmt.Errorf("unable to convert source info: %w", err))
				return
			}
			logger.Debugf(ctx, "source info: %#+v", source)
			sourceName, _ := source["sourceName"].(string)
			if _, ok := sourceNameIsSet[sourceName]; ok {
				continue
			}
			sceneItemTransform := source["sceneItemTransform"].(map[string]any)
			sourceWidth, _ := sceneItemTransform["sourceWidth"].(float64)
			if sourceWidth == 0 {
				continue
			}
			sourceNameIsSet[sourceName] = struct{}{}
			sourceNames = append(sourceNames, sourceName)
		}
	}
	sort.Strings(sourceNames)

	chatSourceSelect := widget.NewSelect(sourceNames, func(s string) {
		cfg.Monitor.ChatOBSSourceName = s
	})
	chatSourceSelect.SetSelected(cfg.Monitor.ChatOBSSourceName)

	countersSourceSelect := widget.NewSelect(sourceNames, func(s string) {
		cfg.Monitor.CounterOBSSourceName = s
	})
	countersSourceSelect.SetSelected(cfg.Monitor.CounterOBSSourceName)

	saveButton := widget.NewButtonWithIcon("Save", theme.DocumentSaveIcon(), func() {
		err := p.StreamD.SetConfig(ctx, cfg)
		if err != nil {
			p.DisplayError(fmt.Errorf("unable to set the config: %w", err))
			return
		}

		err = p.StreamD.SaveConfig(ctx)
		if err != nil {
			p.DisplayError(fmt.Errorf("unable to save the config: %w", err))
			return
		}

		w.Close()
	})

	content := container.NewBorder(
		nil,
		saveButton,
		nil,
		nil,
		container.NewVBox(
			widget.NewLabel("Chat source:"),
			chatSourceSelect,
			widget.NewSeparator(),
			widget.NewLabel("Counters source:"),
			countersSourceSelect,
			widget.NewSeparator(),
		),
	)
	w.SetContent(content)
	w.Show()
}
