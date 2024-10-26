package streampanel

import (
	"context"
	"fmt"
	"image/color"
	"runtime"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"

	"image"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/anthonynsimon/bild/adjust"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/lusingander/colorpicker"
	"github.com/xaionaro-go/obs-grpc-proxy/protobuf/go/obs_grpc"
	"github.com/xaionaro-go/streamctl/pkg/colorx"
	"github.com/xaionaro-go/streamctl/pkg/observability"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/kick"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/obs"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/twitch"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/youtube"
	"github.com/xaionaro-go/streamctl/pkg/streamd/client"
	streamdconfig "github.com/xaionaro-go/streamctl/pkg/streamd/config"
	streamdconsts "github.com/xaionaro-go/streamctl/pkg/streamd/consts"
	"github.com/xaionaro-go/streamctl/pkg/streampanel/consts"
	"github.com/xaionaro-go/streamctl/pkg/xfyne"
	"github.com/xaionaro-go/streamctl/pkg/xsync"
)

func (p *Panel) focusDashboardWindow(
	ctx context.Context,
) {
	err := p.openDashboardWindow(ctx)
	if err != nil {
		p.DisplayError(err)
	}
}

func (p *Panel) openDashboardWindow(
	ctx context.Context,
) (_err error) {
	logger.Debugf(ctx, "newDashboardWindow")
	defer func() { logger.Debugf(ctx, "/newDashboardWindow: %v", _err) }()
	return xsync.DoA1R1(ctx, &p.dashboardLocker, p.openDashboardWindowNoLock, ctx)
}

type dashboardWindow struct {
	fyne.Window
	*Panel
	locker              xsync.Mutex
	stopUpdatingFunc    context.CancelFunc
	lastWinSize         fyne.Size
	lastOrientation     fyne.DeviceOrientation
	screenshotContainer *fyne.Container
	layersContainer     *fyne.Container
}

func (p *Panel) newDashboardWindow(
	context.Context,
) *dashboardWindow {
	w := &dashboardWindow{
		Window: p.app.NewWindow("Dashboard"),
		Panel:  p,
	}

	bg := image.NewGray(image.Rect(0, 0, 1, 1))
	bgFyne := canvas.NewImageFromImage(bg)
	bgFyne.FillMode = canvas.ImageFillStretch

	w.screenshotContainer = container.NewStack()
	p.appStatus = widget.NewLabel("")
	obsLabel := widget.NewLabel("OBS:")
	obsLabel.Importance = widget.HighImportance
	p.streamStatus[obs.ID] = widget.NewLabel("")
	twLabel := widget.NewLabel("TW:")
	twLabel.Importance = widget.HighImportance
	p.streamStatus[twitch.ID] = widget.NewLabel("")
	kcLabel := widget.NewLabel("Kc:")
	kcLabel.Importance = widget.HighImportance
	p.streamStatus[kick.ID] = widget.NewLabel("")
	ytLabel := widget.NewLabel("YT:")
	ytLabel.Importance = widget.HighImportance
	p.streamStatus[youtube.ID] = widget.NewLabel("")
	streamInfoItems := container.NewVBox()
	if _, ok := p.StreamD.(*client.Client); ok {
		appLabel := widget.NewLabel("App:")
		appLabel.Importance = widget.HighImportance
		streamInfoItems.Add(container.NewHBox(layout.NewSpacer(), appLabel, p.appStatus))
	}
	streamInfoItems.Add(container.NewHBox(layout.NewSpacer(), obsLabel, p.streamStatus[obs.ID]))
	streamInfoItems.Add(container.NewHBox(layout.NewSpacer(), twLabel, p.streamStatus[twitch.ID]))
	streamInfoItems.Add(container.NewHBox(layout.NewSpacer(), kcLabel, p.streamStatus[kick.ID]))
	streamInfoItems.Add(container.NewHBox(layout.NewSpacer(), ytLabel, p.streamStatus[youtube.ID]))
	streamInfoContainer := container.NewBorder(
		nil,
		nil,
		nil,
		streamInfoItems,
	)
	w.layersContainer = container.NewStack()

	w.Window.SetContent(container.NewStack(
		bgFyne,
		w.screenshotContainer,
		w.layersContainer,
		streamInfoContainer,
	))
	w.Window.Show()
	return w
}

func (w *dashboardWindow) startUpdating(
	ctx context.Context,
) {
	logger.Debugf(ctx, "startUpdating")
	defer logger.Debugf(ctx, "/startUpdating")
	xsync.DoA1(ctx, &w.locker, w.startUpdatingNoLock, ctx)
}

func (w *dashboardWindow) startUpdatingNoLock(
	ctx context.Context,
) {
	if w.stopUpdatingFunc != nil {
		logger.Errorf(ctx, "updating is already started")
		return
	}

	ctx, cancelFunc := context.WithCancel(ctx)
	w.stopUpdatingFunc = cancelFunc

	cfg, err := w.GetStreamDConfig(ctx)
	if err != nil {
		w.DisplayError(fmt.Errorf("unable to get the current config: %w", err))
		return
	}

	observability.Go(ctx, func() {
		w.updateImages(ctx, cfg.Dashboard)
		w.updateStreamStatus(ctx)

		observability.Go(ctx, func() {
			t := time.NewTicker(200 * time.Millisecond)
			for {
				select {
				case <-ctx.Done():
					return
				case <-t.C:
				}

				w.updateImages(ctx, cfg.Dashboard)
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

				w.updateStreamStatus(ctx)
			}
		})
	})
}

func (w *dashboardWindow) stopUpdating(
	ctx context.Context,
) {
	w.locker.Do(ctx, func() {
		if w.stopUpdatingFunc == nil {
			logger.Errorf(ctx, "the updating is already stopped (or was not started)")
			return
		}
		w.stopUpdatingFunc()
		w.stopUpdatingFunc = nil
	})
}

func (p *Panel) openDashboardWindowNoLock(
	ctx context.Context,
) error {
	p.dashboardShowHideButton.SetText("Hide")
	p.dashboardShowHideButton.SetIcon(theme.WindowCloseIcon())
	if p.dashboardWindow != nil {
		p.dashboardWindow.RequestFocus()
	} else {
		p.dashboardWindow = p.newDashboardWindow(ctx)
	}
	w := p.dashboardWindow
	w.startUpdating(ctx)
	w.Window.SetOnClosed(func() {
		p.dashboardLocker.Do(ctx, func() {
			w.stopUpdating(ctx)
			p.dashboardShowHideButton.SetText("Open")
			p.dashboardShowHideButton.SetIcon(theme.ComputerIcon())
			p.dashboardWindow = nil
		})
	})
	return nil
}

func (w *dashboardWindow) updateImages(
	ctx context.Context,
	dashboardCfg streamdconfig.DashboardConfig,
) {
	logger.Tracef(ctx, "updateImages")
	defer logger.Tracef(ctx, "/updateImages")

	w.dashboardLocker.Do(ctx, func() {
		w.updateImagesNoLock(ctx, dashboardCfg)
	})
}

func (w *dashboardWindow) updateImagesNoLock(
	ctx context.Context,
	dashboardCfg streamdconfig.DashboardConfig,
) {
	var winSize fyne.Size
	var orientation fyne.DeviceOrientation
	switch runtime.GOOS {
	default:
		winSize = w.Canvas().Size()
		orientation = w.app.Driver().Device().Orientation()
	}
	lastWinSize := w.lastWinSize
	lastOrientation := w.lastOrientation
	w.lastWinSize = winSize
	w.lastOrientation = orientation

	if lastWinSize != winSize {
		logger.Debugf(ctx, "window size changed %#+v -> %#+v", lastWinSize, winSize)
	}

	type elementType struct {
		ElementName string
		streamdconfig.DashboardElementConfig
		NewImage *canvas.Image
	}

	elements := make([]elementType, 0, len(dashboardCfg.Elements))
	for elName, el := range dashboardCfg.Elements {
		elements = append(elements, elementType{
			ElementName:            elName,
			DashboardElementConfig: el,
		})
	}
	sort.Slice(elements, func(i, j int) bool {
		if elements[i].ZIndex != elements[j].ZIndex {
			return elements[i].ZIndex < elements[j].ZIndex
		}
		return elements[i].ElementName < elements[j].ElementName
	})

	var wg sync.WaitGroup

	for idx := range elements {
		wg.Add(1)
		{
			el := &elements[idx]
			var oldObj fyne.CanvasObject
			if len(w.layersContainer.Objects) > idx {
				oldObj = w.layersContainer.Objects[idx]
			}
			observability.Go(ctx, func() {
				defer wg.Done()
				img, changed, err := w.getImage(ctx, streamdconsts.ImageID(el.ElementName))
				if err != nil {
					logger.Error(ctx, err)
					return
				}
				if !changed && lastWinSize == winSize && lastOrientation == orientation {
					return
				}
				logger.Tracef(
					ctx,
					"updating the image '%s': %v %#+v %#+v",
					el.ElementName,
					changed,
					lastWinSize,
					winSize,
				)
				imgSize := image.Point{
					X: int(winSize.Width * float32(el.Width) / 100),
					Y: int(winSize.Height * float32(el.Height) / 100),
				}
				offset := image.Point{
					X: int(winSize.Width * float32(el.OffsetX) / 100),
					Y: int(winSize.Height * float32(el.OffsetY) / 100),
				}
				var oldImg image.Image
				if oldCanvasImg, ok := oldObj.(*canvas.Image); ok {
					oldImg = oldCanvasImg.Image
				}
				img = imgFillTo(
					ctx,
					oldImg,
					img,
					image.Point{X: int(winSize.Width), Y: int(winSize.Height)},
					imgSize,
					offset,
					el.AlignX,
					el.AlignY,
				)
				imgFyne := canvas.NewImageFromImage(img)
				imgFyne.FillMode = canvas.ImageFillContain
				imgFyne.SetMinSize(fyne.NewSize(1, 1))
				logger.Tracef(ctx, "image '%s' size: %#+v", el.ElementName, img.Bounds().Size())
				el.NewImage = imgFyne
			})
		}
	}

	wg.Add(1)
	observability.Go(ctx, func() {
		defer wg.Done()
		img, changed, err := w.getImage(ctx, consts.ImageScreenshot)
		if err != nil {
			// we use local config, which is invalid, but we don't want to make a request
			// to another instance just to know if this should be an error or a trace message
			if w.Config.Screenshot.Enabled != nil && *w.Config.Screenshot.Enabled {
				logger.Errorf(ctx, "unable to get the screenshot: %v", err)
			} else {
				logger.Tracef(ctx, "unable to get the screenshot: %v", err)
			}
			return
		}
		if !changed && lastWinSize == winSize && lastOrientation == orientation {
			return
		}
		logger.Tracef(
			ctx,
			"updating the screenshot image: %v %#+v %#+v",
			changed,
			lastWinSize,
			winSize,
		)
		winSize := image.Point{X: int(winSize.Width), Y: int(winSize.Height)}
		img = imgFillTo(
			ctx,
			nil,
			img,
			winSize,
			winSize,
			image.Point{X: 0, Y: 0},
			streamdconsts.AlignXLeft,
			streamdconsts.AlignYTop,
		)
		img = adjust.Brightness(img, -0.5)
		imgFyne := canvas.NewImageFromImage(img)
		imgFyne.FillMode = canvas.ImageFillContain
		logger.Tracef(ctx, "screenshot image size: %#+v", img.Bounds().Size())

		w.screenshotContainer.Objects = w.screenshotContainer.Objects[:0]
		w.screenshotContainer.Objects = append(w.screenshotContainer.Objects, imgFyne)
		w.screenshotContainer.Refresh()
	})
	wg.Wait()

	if len(w.layersContainer.Objects) != len(elements) {
		w.layersContainer.Objects = w.layersContainer.Objects[:0]
		img := image.NewRGBA(image.Rectangle{
			Max: image.Point{
				X: 1,
				Y: 1,
			},
		})
		for len(w.layersContainer.Objects) < len(elements) {
			w.layersContainer.Objects = append(
				w.layersContainer.Objects,
				canvas.NewImageFromImage(img),
			)
		}
	}
	for idx, el := range elements {
		if el.NewImage == nil {
			continue
		}
		w.layersContainer.Objects[idx] = el.NewImage
	}
	w.layersContainer.Refresh()
}

func (w *dashboardWindow) updateStreamStatus(
	ctx context.Context,
) {
	logger.Tracef(ctx, "updateStreamStatus")
	defer logger.Tracef(ctx, "/updateStreamStatus")

	if streamDClient, ok := w.StreamD.(*client.Client); ok {
		now := time.Now()
		appBytesIn := atomic.LoadUint64(&streamDClient.Stats.BytesIn)
		appBytesOut := atomic.LoadUint64(&streamDClient.Stats.BytesOut)
		if !w.appStatusData.prevUpdateTS.IsZero() {
			tsDiff := now.Sub(w.appStatusData.prevUpdateTS)
			bytesInDiff := appBytesIn - w.appStatusData.prevBytesIn
			bytesOutDiff := appBytesOut - w.appStatusData.prevBytesOut
			bwIn := float64(bytesInDiff) * 8 / tsDiff.Seconds() / 1000
			bwOut := float64(bytesOutDiff) * 8 / tsDiff.Seconds() / 1000
			w.appStatus.SetText(fmt.Sprintf("%4.0fKb/s | %4.0fKb/s", bwIn, bwOut))
		}
		w.appStatusData.prevUpdateTS = now
		w.appStatusData.prevBytesIn = appBytesIn
		w.appStatusData.prevBytesOut = appBytesOut
	}

	var wg sync.WaitGroup
	for _, platID := range []streamcontrol.PlatformName{
		obs.ID,
		youtube.ID,
		twitch.ID,
		kick.ID,
	} {
		wg.Add(1)
		observability.Go(ctx, func() {
			defer wg.Done()

			dst := w.streamStatus[platID]

			ok, err := w.StreamD.IsBackendEnabled(ctx, platID)
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

			streamStatus, err := w.StreamD.GetStreamStatus(ctx, platID)
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
			if streamStatus.StartedAt == nil {
				dst.SetText("started")
				return
			}
			duration := time.Since(*streamStatus.StartedAt)

			viewerCountString := ""
			if streamStatus.ViewersCount != nil {
				viewerCountString = fmt.Sprintf(" (%d)", *streamStatus.ViewersCount)
			}

			dst.SetText(fmt.Sprintf("%s%s", duration.Truncate(time.Second).String(), viewerCountString))
		})
	}

	wg.Wait()
}

func (p *Panel) newDashboardSettingsWindow(ctx context.Context) {
	logger.Debugf(ctx, "newDashboardSettingsWindow")
	defer logger.Debugf(ctx, "/newDashboardSettingsWindow")
	settingsWindow := p.app.NewWindow("Dashboard settings")
	resizeWindow(settingsWindow, fyne.NewSize(1500, 1000))

	content := container.NewVBox()

	var refreshContent func()
	refreshContent = func() {
		content.RemoveAll()

		cfg, err := p.GetStreamDConfig(ctx)
		if err != nil {
			p.DisplayError(fmt.Errorf("unable to get the current config: %w", err))
		}

		saveCfg := func(ctx context.Context) error {
			err := p.SetStreamDConfig(ctx, cfg)
			if err != nil {
				return fmt.Errorf("unable to set the config: %w", err)
			}

			err = p.StreamD.SaveConfig(ctx)
			if err != nil {
				return fmt.Errorf("unable to save the config: %w", err)
			}

			p.dashboardLocker.Do(ctx, func() {
				if p.dashboardWindow != nil {
					p.dashboardWindow.Window.Close()
					observability.Go(ctx, func() { p.focusDashboardWindow(ctx) })
				}
			})

			return nil
		}

		content.Add(widget.NewRichTextFromMarkdown("## Elements"))
		for name, el := range cfg.Dashboard.Elements {
			editButton := widget.NewButtonWithIcon("", theme.SettingsIcon(), func() {
				p.editDashboardElementWindow(
					ctx,
					name,
					el,
					func(ctx context.Context, elementName string, newElement streamdconfig.DashboardElementConfig) error {
						if _, ok := cfg.Dashboard.Elements[elementName]; !ok {
							return fmt.Errorf("element with name '%s' does not exist", elementName)
						}
						cfg.Dashboard.Elements[elementName] = newElement
						err := saveCfg(ctx)
						if err != nil {
							return err
						}
						refreshContent()
						return nil
					},
				)
			})
			deleteButton := widget.NewButtonWithIcon("", theme.DeleteIcon(), func() {
				w := dialog.NewConfirm(
					fmt.Sprintf("Delete dashboard element '%s'?", name),
					fmt.Sprintf(
						"Are you sure you want to delete the element '%s' from the Dashboard?",
						name,
					),
					func(b bool) {
						if !b {
							return
						}

						delete(cfg.Dashboard.Elements, name)
						err := saveCfg(ctx)
						if err != nil {
							p.DisplayError(err)
							return
						}
						refreshContent()
					},
					settingsWindow,
				)
				w.Show()
			})
			content.Add(container.NewHBox(
				editButton,
				deleteButton,
				widget.NewLabel(name),
			))
		}

		addButton := widget.NewButtonWithIcon("Add", theme.ContentAddIcon(), func() {
			p.editDashboardElementWindow(
				ctx,
				"",
				streamdconfig.DashboardElementConfig{},
				func(ctx context.Context, elementName string, newElement streamdconfig.DashboardElementConfig) error {
					if _, ok := cfg.Dashboard.Elements[elementName]; ok {
						return fmt.Errorf("element with name '%s' already exists", elementName)
					}
					cfg.Dashboard.Elements[elementName] = newElement
					err := saveCfg(ctx)
					if err != nil {
						return err
					}
					refreshContent()
					return nil
				},
			)
		})
		content.Add(addButton)
	}

	refreshContent()
	settingsWindow.SetContent(content)
	settingsWindow.Show()
}

func (p *Panel) editDashboardElementWindow(
	ctx context.Context,
	_elementNameValue string,
	cfg streamdconfig.DashboardElementConfig,
	saveFunc func(ctx context.Context, elementName string, cfg streamdconfig.DashboardElementConfig) error,
) {
	logger.Debugf(ctx, "editDashboardElementWindow")
	defer logger.Debugf(ctx, "/editDashboardElementWindow")
	w := p.app.NewWindow("Dashboard element settings")
	resizeWindow(w, fyne.NewSize(1500, 1000))

	obsVideoSource := &streamdconfig.DashboardSourceImageOBSScreenshot{
		UpdateInterval: streamdconfig.Duration(200 * time.Millisecond),
	}
	obsVolumeSource := &streamdconfig.DashboardSourceImageOBSVolume{
		UpdateInterval: streamdconfig.Duration(200 * time.Millisecond),
		ColorActive:    "00FF00FF",
		ColorPassive:   "00000000",
	}
	dummy := &streamdconfig.DashboardSourceImageDummy{}
	switch source := cfg.Source.(type) {
	case *streamdconfig.DashboardSourceImageOBSScreenshot:
		obsVideoSource = source
	case *streamdconfig.DashboardSourceImageOBSVolume:
		obsVolumeSource = source
	case *streamdconfig.DashboardSourceImageDummy:
		dummy = source
	}

	elementName := widget.NewEntry()
	if _elementNameValue != "" {
		elementName.SetText(_elementNameValue)
		elementName.Disable()
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

	var videoSourceNames []string
	var audioSourceNames []string
	videoSourceNameIsSet := map[string]struct{}{}
	audioSourceNameIsSet := map[string]struct{}{}
	for _, scene := range resp.Scenes {
		videoSourceNames = append(videoSourceNames, *scene.SceneName)
		resp, err := obsServer.GetSceneItemList(ctx, &obs_grpc.GetSceneItemListRequest{
			SceneName: scene.SceneName,
		})
		if err != nil {
			p.DisplayError(
				fmt.Errorf("unable to get the list of items of scene '%v': %w", scene.SceneName, err),
			)
			return
		}
		for _, item := range resp.SceneItems {
			logger.Debugf(ctx, "source info: %#+v", item)
			func() {
				if _, ok := videoSourceNameIsSet[item.SourceName]; ok {
					return
				}
				sceneItemTransform := item.SceneItemTransform
				sourceWidth := sceneItemTransform.SourceWidth
				if sourceWidth == 0 {
					return
				}
				videoSourceNameIsSet[item.SourceName] = struct{}{}
				videoSourceNames = append(videoSourceNames, item.SourceName)
			}()
			func() {
				if _, ok := audioSourceNameIsSet[item.SourceName]; ok {
					return
				}
				// TODO: filter only audio sources
				audioSourceNameIsSet[item.SourceName] = struct{}{}
				audioSourceNames = append(audioSourceNames, item.SourceName)
			}()
		}
	}
	sort.Strings(videoSourceNames)

	sourceOBSVideoSelect := widget.NewSelect(videoSourceNames, func(s string) {
		obsVideoSource.Name = s
	})
	sourceOBSVideoSelect.SetSelected(obsVideoSource.Name)

	sourceWidth := xfyne.NewNumericalEntry()
	sourceWidth.SetText(fmt.Sprintf("%v", obsVideoSource.Width))
	sourceWidth.OnChanged = func(s string) {
		if s == "" || s == "-" {
			s = "0"
		}
		v, err := strconv.ParseFloat(s, 64)
		if err != nil {
			p.DisplayError(fmt.Errorf("unable to parse '%s' as a float: %w", s, err))
			return
		}
		obsVideoSource.Width = v
	}
	sourceHeight := xfyne.NewNumericalEntry()
	sourceHeight.SetText(fmt.Sprintf("%v", obsVideoSource.Height))
	sourceHeight.OnChanged = func(s string) {
		if s == "" || s == "-" {
			s = "0"
		}
		v, err := strconv.ParseFloat(s, 64)
		if err != nil {
			p.DisplayError(fmt.Errorf("unable to parse '%s' as a float: %w", s, err))
			return
		}
		obsVideoSource.Height = v
	}

	if obsVideoSource.ImageFormat == streamdconfig.ImageFormatUndefined {
		obsVideoSource.ImageFormat = streamdconfig.ImageFormatJPEG
	}

	imageFormatSelect := widget.NewSelect([]string{
		string(streamdconfig.ImageFormatPNG),
		string(streamdconfig.ImageFormatJPEG),
		string(streamdconfig.ImageFormatWebP),
	}, func(s string) {
		obsVideoSource.ImageFormat = streamdconfig.ImageFormat(s)
	})
	imageFormatSelect.SetSelected(string(obsVideoSource.ImageFormat))

	imageQuality := xfyne.NewNumericalEntry()
	imageQuality.SetText(fmt.Sprintf("%v", cfg.ImageQuality))
	imageQuality.OnChanged = func(s string) {
		if s == "" || s == "-" {
			s = "0"
		}
		v, err := strconv.ParseFloat(s, 64)
		if err != nil {
			p.DisplayError(fmt.Errorf("unable to parse '%s' as a float: %w", s, err))
			return
		}
		cfg.ImageQuality = v
	}

	// we cannot use imageQuality twice, unfortunately, so duplicating it:
	imageCompressionValue := xfyne.NewNumericalEntry()
	imageCompressionValue.SetText(fmt.Sprintf("%v", cfg.ImageQuality))
	imageCompressionValue.OnChanged = func(s string) {
		if s == "" || s == "-" {
			s = "0"
		}
		v, err := strconv.ParseFloat(s, 64)
		if err != nil {
			p.DisplayError(fmt.Errorf("unable to parse '%s' as a float: %w", s, err))
			return
		}
		cfg.ImageQuality = v
	}

	imageCompression := container.NewVBox(
		widget.NewLabel("Compression:"),
		imageCompressionValue,
	)

	isLossless := widget.NewCheck("Is lossless:", func(b bool) {
		cfg.ImageLossless = b
		if b {
			imageQuality.Hide()
			imageCompression.Show()
		} else {
			imageCompression.Hide()
			imageQuality.Show()
		}
	})
	isLossless.SetChecked(cfg.ImageLossless)
	isLossless.OnChanged(cfg.ImageLossless)

	brightnessValue := float64(0)
	brightness := xfyne.NewNumericalEntry()
	brightness.OnChanged = func(s string) {
		if s == "" || s == "-" {
			s = "0"
		}
		v, err := strconv.ParseFloat(s, 64)
		if err != nil {
			p.DisplayError(fmt.Errorf("unable to parse '%s' as a float: %w", s, err))
			return
		}
		brightnessValue = v
	}

	opacityValue := float64(0)
	opacity := xfyne.NewNumericalEntry()
	opacity.OnChanged = func(s string) {
		if s == "" || s == "-" {
			s = "0"
		}
		v, err := strconv.ParseFloat(s, 64)
		if err != nil {
			p.DisplayError(fmt.Errorf("unable to parse '%s' as a float: %w", s, err))
			return
		}
		opacityValue = v
	}

	for _, filter := range cfg.Filters {
		switch filter := filter.(type) {
		case *streamdconfig.FilterColor:
			brightnessValue += filter.Brightness
			opacityValue += filter.Opacity
		}
	}
	brightness.SetText(fmt.Sprintf("%f", brightnessValue))

	obsVideoUpdateInterval := xfyne.NewNumericalEntry()
	obsVideoUpdateInterval.SetText(
		fmt.Sprintf("%v", time.Duration(obsVideoSource.UpdateInterval).Seconds()),
	)
	obsVideoUpdateInterval.OnChanged = func(s string) {
		if s == "" || s == "-" {
			s = "0.2"
		}
		v, err := strconv.ParseFloat(s, 64)
		if err != nil {
			p.DisplayError(fmt.Errorf("unable to parse '%s' as a float: %w", s, err))
			return
		}
		obsVideoSource.UpdateInterval = streamdconfig.Duration(float64(time.Second) * v)
	}

	zIndex := xfyne.NewNumericalEntry()
	zIndex.SetText(fmt.Sprintf("%v", cfg.ZIndex))
	zIndex.OnChanged = func(s string) {
		if s == "" || s == "-" {
			s = "0"
		}
		v, err := strconv.ParseFloat(s, 64)
		if err != nil {
			p.DisplayError(fmt.Errorf("unable to parse '%s' as a float: %w", s, err))
			return
		}
		cfg.ZIndex = v
	}

	if cfg.Width == 0 {
		cfg.Width = 100
	}
	if cfg.Height == 0 {
		cfg.Height = 100
	}

	displayWidth := xfyne.NewNumericalEntry()
	displayWidth.SetText(fmt.Sprintf("%v", cfg.Width))
	displayWidth.OnChanged = func(s string) {
		if s == "" || s == "-" {
			s = "0"
		}
		v, err := strconv.ParseFloat(s, 64)
		if err != nil {
			p.DisplayError(fmt.Errorf("unable to parse '%s' as a float: %w", s, err))
			return
		}
		cfg.Width = v
	}
	displayHeight := xfyne.NewNumericalEntry()
	displayHeight.SetText(fmt.Sprintf("%v", cfg.Height))
	displayHeight.OnChanged = func(s string) {
		if s == "" || s == "-" {
			s = "0"
		}
		v, err := strconv.ParseFloat(s, 64)
		if err != nil {
			p.DisplayError(fmt.Errorf("unable to parse '%s' as a float: %w", s, err))
			return
		}
		cfg.Height = v
	}

	if cfg.AlignX == "" {
		cfg.AlignX = streamdconsts.AlignXMiddle
	}

	if cfg.AlignY == "" {
		cfg.AlignY = streamdconsts.AlignYMiddle
	}

	alignX := widget.NewSelect([]string{
		string(streamdconsts.AlignXLeft),
		string(streamdconsts.AlignXMiddle),
		string(streamdconsts.AlignXRight),
	}, func(s string) {
		cfg.AlignX = streamdconsts.AlignX(s)
	})
	alignX.SetSelected(string(cfg.AlignX))

	alignY := widget.NewSelect([]string{
		string(streamdconsts.AlignYTop),
		string(streamdconsts.AlignYMiddle),
		string(streamdconsts.AlignYBottom),
	}, func(s string) {
		cfg.AlignY = streamdconsts.AlignY(s)
	})
	alignY.SetSelected(string(cfg.AlignY))

	offsetX := xfyne.NewNumericalEntry()
	offsetX.SetText(fmt.Sprintf("%v", cfg.OffsetX))
	offsetX.OnChanged = func(s string) {
		if s == "" || s == "-" {
			s = "0"
		}
		v, err := strconv.ParseFloat(s, 64)
		if err != nil {
			p.DisplayError(fmt.Errorf("unable to parse '%s' as a float: %w", s, err))
			return
		}
		cfg.OffsetX = v
	}
	offsetY := xfyne.NewNumericalEntry()
	offsetY.SetText(fmt.Sprintf("%v", cfg.OffsetY))
	offsetY.OnChanged = func(s string) {
		if s == "" || s == "-" {
			s = "0"
		}
		v, err := strconv.ParseFloat(s, 64)
		if err != nil {
			p.DisplayError(fmt.Errorf("unable to parse '%s' as a float: %w", s, err))
			return
		}
		cfg.OffsetY = v
	}

	sourceOBSVideoConfig := container.NewVBox(
		widget.NewLabel("Source:"),
		sourceOBSVideoSelect,
		widget.NewLabel("Source image size (use '0' for preserving the original size or ratio):"),
		container.NewHBox(
			widget.NewLabel("X:"),
			sourceWidth,
			widget.NewLabel(`px`),
			widget.NewSeparator(),
			widget.NewLabel("Y:"),
			sourceHeight,
			widget.NewLabel(`px`),
		),
		widget.NewLabel("Format:"),
		imageFormatSelect,
		widget.NewLabel("Update interval:"),
		container.NewHBox(obsVideoUpdateInterval, widget.NewLabel("seconds")),
	)

	sourceOBSVolumeSelect := widget.NewSelect(audioSourceNames, func(s string) {
		obsVolumeSource.Name = s
	})

	obsVolumeUpdateInterval := xfyne.NewNumericalEntry()
	obsVolumeUpdateInterval.SetText(
		fmt.Sprintf("%v", time.Duration(obsVideoSource.UpdateInterval).Seconds()),
	)
	obsVolumeUpdateInterval.OnChanged = func(s string) {
		if s == "" || s == "-" {
			s = "0.2"
		}
		v, err := strconv.ParseFloat(s, 64)
		if err != nil {
			p.DisplayError(fmt.Errorf("unable to parse '%s' as a float: %w", s, err))
			return
		}
		obsVolumeSource.UpdateInterval = streamdconfig.Duration(float64(time.Second) * v)
	}

	var volumeColorActiveParsed color.Color
	if volumeColorActiveParsed, err = colorx.Parse(obsVolumeSource.ColorActive); err != nil {
		volumeColorActiveParsed = color.RGBA{R: 0, G: 255, B: 0, A: 255}
	}
	volumeColorActive := colorpicker.NewColorSelectModalRect(
		w,
		fyne.NewSize(30, 20),
		volumeColorActiveParsed,
	)
	volumeColorActive.SetOnChange(func(c color.Color) {
		r32, g32, b32, a32 := c.RGBA()
		r8, g8, b8, a8 := uint8(r32>>8), uint8(g32>>8), uint8(b32>>8), uint8(a32>>8)
		obsVolumeSource.ColorActive = fmt.Sprintf("%.2X%.2X%.2X%.2X", r8, g8, b8, a8)
	})

	var volumeColorPassiveParsed color.Color
	if volumeColorPassiveParsed, err = colorx.Parse(obsVolumeSource.ColorPassive); err != nil {
		volumeColorPassiveParsed = color.RGBA{R: 0, G: 0, B: 0, A: 0}
	}
	volumeColorPassive := colorpicker.NewColorSelectModalRect(
		w,
		fyne.NewSize(30, 20),
		volumeColorPassiveParsed,
	)
	volumeColorPassive.SetOnChange(func(c color.Color) {
		r32, g32, b32, a32 := c.RGBA()
		r8, g8, b8, a8 := uint8(r32>>8), uint8(g32>>8), uint8(b32>>8), uint8(a32>>8)
		obsVolumeSource.ColorPassive = fmt.Sprintf("%.2X%.2X%.2X%.2X", r8, g8, b8, a8)
	})

	sourceOBSVideoSelect.SetSelected(obsVideoSource.Name)
	sourceOBSVolumeConfig := container.NewVBox(
		widget.NewLabel("Source:"),
		sourceOBSVolumeSelect,
		widget.NewLabel("Color active:"),
		volumeColorActive,
		widget.NewLabel("Color passive:"),
		volumeColorPassive,
		widget.NewLabel("Update interval:"),
		container.NewHBox(obsVolumeUpdateInterval, widget.NewLabel("seconds")),
	)

	sourceTypeSelect := widget.NewSelect([]string{
		string(streamdconfig.DashboardSourceImageTypeOBSVideo),
		string(streamdconfig.DashboardSourceImageTypeOBSVolume),
		string(streamdconfig.DashboardSourceImageTypeDummy),
	}, func(s string) {
		switch streamdconfig.DashboardSourceImageType(s) {
		case streamdconfig.DashboardSourceImageTypeOBSVideo:
			sourceOBSVolumeConfig.Hide()
			sourceOBSVideoConfig.Show()
		case streamdconfig.DashboardSourceImageTypeOBSVolume:
			sourceOBSVideoConfig.Hide()
			sourceOBSVolumeConfig.Show()
		case streamdconfig.DashboardSourceImageTypeDummy:
			sourceOBSVideoConfig.Hide()
			sourceOBSVolumeConfig.Hide()
		}
	})
	sourceTypeSelect.SetSelected(string(streamdconfig.DashboardSourceImageTypeOBSVideo))

	saveButton := widget.NewButtonWithIcon("Save", theme.DocumentSaveIcon(), func() {
		if elementName.Text == "" {
			p.DisplayError(fmt.Errorf("element name is not set"))
			return
		}
		switch streamdconfig.DashboardSourceImageType(sourceTypeSelect.Selected) {
		case streamdconfig.DashboardSourceImageTypeOBSVideo:
			cfg.Source = obsVideoSource
		case streamdconfig.DashboardSourceImageTypeOBSVolume:
			cfg.Source = obsVolumeSource
		case streamdconfig.DashboardSourceImageTypeDummy:
			cfg.Source = dummy
		}
		cfg.Filters = cfg.Filters[:0]
		if brightnessValue != 0 || opacityValue != 0 {
			cfg.Filters = append(cfg.Filters, &streamdconfig.FilterColor{
				Brightness: brightnessValue,
				Opacity:    opacityValue,
			})
		}
		err := saveFunc(ctx, elementName.Text, cfg)
		if err != nil {
			p.DisplayError(fmt.Errorf("unable to save the dashboard element: %w", err))
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
			widget.NewLabel("Dashboard element name:"),
			elementName,
			widget.NewLabel("Z-Index / layer:"),
			zIndex,
			widget.NewLabel("Display size:"),
			container.NewHBox(
				widget.NewLabel("X:"),
				displayWidth,
				widget.NewLabel(`%`),
				widget.NewSeparator(),
				widget.NewLabel("Y:"),
				displayHeight,
				widget.NewLabel(`%`),
			),
			widget.NewLabel("Align:"),
			container.NewHBox(
				widget.NewLabel("X:"),
				alignX,
				widget.NewSeparator(),
				widget.NewLabel("Y:"),
				alignY,
			),
			widget.NewLabel("Offset:"),
			container.NewHBox(
				widget.NewLabel("X:"),
				offsetX,
				widget.NewLabel(`%`),
				widget.NewSeparator(),
				widget.NewLabel("Y:"),
				offsetY,
				widget.NewLabel(`%`),
			),
			widget.NewLabel("Quality:"),
			isLossless,
			imageQuality,
			imageCompression,
			widget.NewLabel("Brightness adjustment (-1.0 .. 1.0):"),
			brightness,
			widget.NewLabel("Opacity multiplier:"),
			opacity,
			widget.NewLabel("Source type:"),
			sourceTypeSelect,
			sourceOBSVideoConfig,
			sourceOBSVolumeConfig,
		),
	)
	w.SetContent(content)
	w.Show()
}
