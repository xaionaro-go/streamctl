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
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/anthonynsimon/bild/adjust"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/lusingander/colorpicker"
	"github.com/xaionaro-go/obs-grpc-proxy/protobuf/go/obs_grpc"
	"github.com/xaionaro-go/streamctl/pkg/colorx"
	"github.com/xaionaro-go/streamctl/pkg/observability"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/obs"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/twitch"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/youtube"
	"github.com/xaionaro-go/streamctl/pkg/streamd/client"
	streamdconfig "github.com/xaionaro-go/streamctl/pkg/streamd/config"
	streamdconsts "github.com/xaionaro-go/streamctl/pkg/streamd/consts"
	"github.com/xaionaro-go/streamctl/pkg/streampanel/consts"
	"github.com/xaionaro-go/streamctl/pkg/xfyne"
)

func (p *Panel) startMonitorPage(
	ctx context.Context,
) {
	logger.Debugf(ctx, "startMonitorPage")
	defer logger.Debugf(ctx, "/startMonitorPage")

	cfg, err := p.StreamD.GetConfig(ctx)
	if err != nil {
		p.DisplayError(fmt.Errorf("unable to get the current config: %w", err))
		return
	}

	observability.Go(ctx, func() {
		p.updateMonitorPageImages(ctx, cfg.Monitor)
		p.updateMonitorPageStreamStatus(ctx)

		observability.Go(ctx, func() {
			t := time.NewTicker(200 * time.Millisecond)
			for {
				select {
				case <-ctx.Done():
					return
				case <-t.C:
				}

				p.updateMonitorPageImages(ctx, cfg.Monitor)
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
	monitorCfg streamdconfig.MonitorConfig,
) {
	logger.Tracef(ctx, "updateMonitorPageImages")
	defer logger.Tracef(ctx, "/updateMonitorPageImages")

	p.monitorLocker.Do(ctx, func() {
		p.updateMonitorPageImagesNoLock(ctx, monitorCfg)
	})
}

func (p *Panel) updateMonitorPageImagesNoLock(
	ctx context.Context,
	monitorCfg streamdconfig.MonitorConfig,
) {
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

	type elementType struct {
		ElementName string
		streamdconfig.MonitorElementConfig
		NewImage *canvas.Image
	}

	elements := make([]elementType, 0, len(monitorCfg.Elements))
	for elName, el := range monitorCfg.Elements {
		elements = append(elements, elementType{
			ElementName:          elName,
			MonitorElementConfig: el,
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
			observability.Go(ctx, func() {
				defer wg.Done()
				img, changed, err := p.getImage(ctx, streamdconsts.ImageID(el.ElementName))
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
				img = imgFillTo(
					ctx,
					img,
					image.Point{X: int(winSize.Width), Y: int(winSize.Height)},
					imgSize,
					offset,
					el.AlignX,
					el.AlignY,
				)
				imgFyne := canvas.NewImageFromImage(img)
				imgFyne.FillMode = canvas.ImageFillContain
				logger.Tracef(ctx, "image '%s' size: %#+v", el.ElementName, img.Bounds().Size())
				el.NewImage = imgFyne
			})
		}
	}

	wg.Add(1)
	observability.Go(ctx, func() {
		defer wg.Done()
		img, changed, err := p.getImage(ctx, consts.ImageScreenshot)
		if err != nil {
			// we use local config, which is invalid, but we don't want to make a request
			// to another instance just to know if this should be an error or a trace message
			if p.Config.Screenshot.Enabled != nil && *p.Config.Screenshot.Enabled {
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

		p.screenshotContainer.Objects = p.screenshotContainer.Objects[:0]
		p.screenshotContainer.Objects = append(p.screenshotContainer.Objects, imgFyne)
		p.screenshotContainer.Refresh()
	})
	wg.Wait()

	if len(p.monitorLayersContainer.Objects) != len(elements) {
		p.monitorLayersContainer.Objects = p.monitorLayersContainer.Objects[:0]
		img := image.NewRGBA(image.Rectangle{
			Max: image.Point{
				X: 1,
				Y: 1,
			},
		})
		for len(p.monitorLayersContainer.Objects) < len(elements) {
			p.monitorLayersContainer.Objects = append(
				p.monitorLayersContainer.Objects,
				canvas.NewImageFromImage(img),
			)
		}
	}
	for idx, el := range elements {
		if el.NewImage == nil {
			continue
		}
		p.monitorLayersContainer.Objects[idx] = el.NewImage
	}
	p.monitorLayersContainer.Refresh()
}

func (p *Panel) updateMonitorPageStreamStatus(
	ctx context.Context,
) {
	logger.Tracef(ctx, "updateMonitorPageStreamStatus")
	defer logger.Tracef(ctx, "/updateMonitorPageStreamStatus")

	if streamDClient, ok := p.StreamD.(*client.Client); ok {
		now := time.Now()
		appBytesIn := atomic.LoadUint64(&streamDClient.Stats.BytesIn)
		appBytesOut := atomic.LoadUint64(&streamDClient.Stats.BytesOut)
		if !p.appStatusData.prevUpdateTS.IsZero() {
			tsDiff := now.Sub(p.appStatusData.prevUpdateTS)
			bytesInDiff := appBytesIn - p.appStatusData.prevBytesIn
			bytesOutDiff := appBytesOut - p.appStatusData.prevBytesOut
			bwIn := float64(bytesInDiff) * 8 / tsDiff.Seconds() / 1000
			bwOut := float64(bytesOutDiff) * 8 / tsDiff.Seconds() / 1000
			p.appStatus.SetText(fmt.Sprintf("%4.0fKb/s | %4.0fKb/s", bwIn, bwOut))
		}
		p.appStatusData.prevUpdateTS = now
		p.appStatusData.prevBytesIn = appBytesIn
		p.appStatusData.prevBytesOut = appBytesOut
	}

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
	logger.Debugf(ctx, "newMonitorSettingsWindow")
	defer logger.Debugf(ctx, "/newMonitorSettingsWindow")
	w := p.app.NewWindow("Monitor settings")
	resizeWindow(w, fyne.NewSize(1500, 1000))

	content := container.NewVBox()

	var refreshContent func()
	refreshContent = func() {
		content.RemoveAll()

		cfg, err := p.StreamD.GetConfig(ctx)
		if err != nil {
			p.DisplayError(fmt.Errorf("unable to get the current config: %w", err))
		}

		saveCfg := func(ctx context.Context) error {
			err := p.StreamD.SetConfig(ctx, cfg)
			if err != nil {
				return fmt.Errorf("unable to set the config: %w", err)
			}

			err = p.StreamD.SaveConfig(ctx)
			if err != nil {
				return fmt.Errorf("unable to save the config: %w", err)
			}

			return nil
		}

		content.Add(widget.NewRichTextFromMarkdown("## Elements"))
		for name, el := range cfg.Monitor.Elements {
			editButton := widget.NewButtonWithIcon("", theme.SettingsIcon(), func() {
				p.editMonitorElementWindow(
					ctx,
					name,
					el,
					func(ctx context.Context, elementName string, newElement streamdconfig.MonitorElementConfig) error {
						if _, ok := cfg.Monitor.Elements[elementName]; !ok {
							return fmt.Errorf("element with name '%s' does not exist", elementName)
						}
						cfg.Monitor.Elements[elementName] = newElement
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
					fmt.Sprintf("Delete monitor element '%s'?", name),
					fmt.Sprintf(
						"Are you sure you want to delete the element '%s' from the Monitor page?",
						name,
					),
					func(b bool) {
						if !b {
							return
						}

						delete(cfg.Monitor.Elements, name)
						err := saveCfg(ctx)
						if err != nil {
							p.DisplayError(err)
							return
						}
						refreshContent()
					},
					w,
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
			p.editMonitorElementWindow(
				ctx,
				"",
				streamdconfig.MonitorElementConfig{},
				func(ctx context.Context, elementName string, newElement streamdconfig.MonitorElementConfig) error {
					if _, ok := cfg.Monitor.Elements[elementName]; ok {
						return fmt.Errorf("element with name '%s' already exists", elementName)
					}
					cfg.Monitor.Elements[elementName] = newElement
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
	w.SetContent(content)
	w.Show()
}

func (p *Panel) editMonitorElementWindow(
	ctx context.Context,
	_elementNameValue string,
	cfg streamdconfig.MonitorElementConfig,
	saveFunc func(ctx context.Context, elementName string, cfg streamdconfig.MonitorElementConfig) error,
) {
	logger.Debugf(ctx, "editMonitorElementWindow")
	defer logger.Debugf(ctx, "/editMonitorElementWindow")
	w := p.app.NewWindow("Monitor element settings")
	resizeWindow(w, fyne.NewSize(1500, 1000))

	obsVideoSource := &streamdconfig.MonitorSourceOBSVideo{
		UpdateInterval: streamdconfig.Duration(200 * time.Millisecond),
	}
	obsVolumeSource := &streamdconfig.MonitorSourceOBSVolume{
		UpdateInterval: streamdconfig.Duration(200 * time.Millisecond),
		ColorActive:    "00FF00FF",
		ColorPassive:   "00000000",
	}
	dummy := &streamdconfig.MonitorSourceDummy{}
	switch source := cfg.Source.(type) {
	case *streamdconfig.MonitorSourceOBSVideo:
		obsVideoSource = source
	case *streamdconfig.MonitorSourceOBSVolume:
		obsVolumeSource = source
	case *streamdconfig.MonitorSourceDummy:
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
		resp, err := obsServer.GetSceneItemList(ctx, &obs_grpc.GetSceneItemListRequest{
			SceneName: scene.SceneName,
		})
		if err != nil {
			p.DisplayError(
				fmt.Errorf("unable to get the list of items of scene '%s': %w", scene.SceneName, err),
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
		string(streamdconfig.MonitorSourceTypeOBSVideo),
		string(streamdconfig.MonitorSourceTypeOBSVolume),
		string(streamdconfig.MonitorSourceTypeDummy),
	}, func(s string) {
		switch streamdconfig.MonitorSourceType(s) {
		case streamdconfig.MonitorSourceTypeOBSVideo:
			sourceOBSVolumeConfig.Hide()
			sourceOBSVideoConfig.Show()
		case streamdconfig.MonitorSourceTypeOBSVolume:
			sourceOBSVideoConfig.Hide()
			sourceOBSVolumeConfig.Show()
		case streamdconfig.MonitorSourceTypeDummy:
			sourceOBSVideoConfig.Hide()
			sourceOBSVolumeConfig.Hide()
		}
	})
	sourceTypeSelect.SetSelected(string(streamdconfig.MonitorSourceTypeOBSVideo))

	saveButton := widget.NewButtonWithIcon("Save", theme.DocumentSaveIcon(), func() {
		if elementName.Text == "" {
			p.DisplayError(fmt.Errorf("element name is not set"))
			return
		}
		switch streamdconfig.MonitorSourceType(sourceTypeSelect.Selected) {
		case streamdconfig.MonitorSourceTypeOBSVideo:
			cfg.Source = obsVideoSource
		case streamdconfig.MonitorSourceTypeOBSVolume:
			cfg.Source = obsVolumeSource
		case streamdconfig.MonitorSourceTypeDummy:
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
			p.DisplayError(fmt.Errorf("unable to save the monitor element: %w", err))
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
			widget.NewLabel("Monitor element name:"),
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
