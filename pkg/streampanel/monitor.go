package streampanel

import (
	"context"
	"fmt"
	"runtime"
	"sort"
	"strconv"
	"sync"

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
	"github.com/xaionaro-go/obs-grpc-proxy/pkg/obsgrpcproxy"
	"github.com/xaionaro-go/obs-grpc-proxy/protobuf/go/obs_grpc"
	"github.com/xaionaro-go/streamctl/pkg/observability"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/obs"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/twitch"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/youtube"
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

	type elementType struct {
		ElementName string
		streamdconfig.OBSSource
		NewImage *canvas.Image
	}

	elements := make([]elementType, 0, len(monitorCfg.Elements))
	for elName, el := range monitorCfg.Elements {
		elements = append(elements, elementType{
			ElementName: elName,
			OBSSource:   el,
		})
	}
	sort.Slice(elements, func(i, j int) bool {
		return elements[i].ZIndex < elements[j].ZIndex
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
				logger.Tracef(ctx, "updating the image '%s': %v %#+v %#+v", el.ElementName, changed, lastWinSize, winSize)
				img = imgFillTo(ctx, img, image.Point{X: int(winSize.Width), Y: int(winSize.Height)}, alignStart, alignEnd)
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
			logger.Error(ctx, err)
			return
		}
		if !changed && lastWinSize == winSize && lastOrientation == orientation {
			return
		}
		logger.Tracef(ctx, "updating the screenshot image: %v %#+v %#+v", changed, lastWinSize, winSize)
		img = imgFillTo(ctx, img, image.Point{X: int(winSize.Width), Y: int(winSize.Height)}, alignStart, alignStart)
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
					func(ctx context.Context, elementName string, newElement streamdconfig.OBSSource) error {
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
					fmt.Sprintf("Are you sure you want to delete the element '%s' from the Monitor page?", name),
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
				streamdconfig.OBSSource{},
				func(ctx context.Context, elementName string, newElement streamdconfig.OBSSource) error {
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
	cfg streamdconfig.OBSSource,
	saveFunc func(ctx context.Context, elementName string, cfg streamdconfig.OBSSource) error,
) {
	logger.Debugf(ctx, "editMonitorElementWindow")
	defer logger.Debugf(ctx, "/editMonitorElementWindow")
	w := p.app.NewWindow("Monitor element settings")
	resizeWindow(w, fyne.NewSize(1500, 1000))

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
		cfg.SourceName = s
	})
	chatSourceSelect.SetSelected(cfg.SourceName)

	sourceWidth := xfyne.NewNumericalEntry()
	sourceWidth.SetText(fmt.Sprintf("%v", cfg.SourceWidth))
	sourceWidth.OnChanged = func(s string) {
		if s == "" || s == "-" {
			s = "0"
		}
		v, err := strconv.ParseFloat(s, 64)
		if err != nil {
			p.DisplayError(fmt.Errorf("unable to parse '%s' as a float: %w", s, err))
			return
		}
		cfg.SourceWidth = v
	}
	sourceHeight := xfyne.NewNumericalEntry()
	sourceHeight.SetText(fmt.Sprintf("%v", cfg.SourceHeight))
	sourceHeight.OnChanged = func(s string) {
		if s == "" || s == "-" {
			s = "0"
		}
		v, err := strconv.ParseFloat(s, 64)
		if err != nil {
			p.DisplayError(fmt.Errorf("unable to parse '%s' as a float: %w", s, err))
			return
		}
		cfg.SourceHeight = v
	}

	if cfg.ImageFormat == streamdconfig.ImageFormatUndefined {
		cfg.ImageFormat = streamdconfig.ImageFormatJPEG
	}

	imageFormatSelect := widget.NewSelect([]string{
		string(streamdconfig.ImageFormatPNG),
		string(streamdconfig.ImageFormatJPEG),
		string(streamdconfig.ImageFormatWebP),
	}, func(s string) {
		cfg.ImageFormat = streamdconfig.ImageFormat(s)
	})
	imageFormatSelect.SetSelected(string(cfg.ImageFormat))

	if cfg.ImageQuality == 0 {
		cfg.ImageQuality = 2
	}

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

	if cfg.UpdateInterval == 0 {
		cfg.UpdateInterval = 200 * time.Millisecond
	}

	updateInterval := xfyne.NewNumericalEntry()
	updateInterval.SetText(fmt.Sprintf("%v", cfg.UpdateInterval.Seconds()))
	updateInterval.OnChanged = func(s string) {
		if s == "" || s == "-" {
			s = "0.2"
		}
		v, err := strconv.ParseFloat(s, 64)
		if err != nil {
			p.DisplayError(fmt.Errorf("unable to parse '%s' as a float: %w", s, err))
			return
		}
		cfg.UpdateInterval = time.Duration(float64(time.Second) * v)
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

	if cfg.DisplayWidth == 0 {
		cfg.DisplayWidth = 100
	}
	if cfg.DisplayHeight == 0 {
		cfg.DisplayHeight = 100
	}

	displayWidth := xfyne.NewNumericalEntry()
	displayWidth.SetText(fmt.Sprintf("%v", cfg.DisplayWidth))
	displayWidth.OnChanged = func(s string) {
		if s == "" || s == "-" {
			s = "0"
		}
		v, err := strconv.ParseFloat(s, 64)
		if err != nil {
			p.DisplayError(fmt.Errorf("unable to parse '%s' as a float: %w", s, err))
			return
		}
		cfg.DisplayWidth = v
	}
	displayHeight := xfyne.NewNumericalEntry()
	displayHeight.SetText(fmt.Sprintf("%v", cfg.DisplayHeight))
	displayHeight.OnChanged = func(s string) {
		if s == "" || s == "-" {
			s = "0"
		}
		v, err := strconv.ParseFloat(s, 64)
		if err != nil {
			p.DisplayError(fmt.Errorf("unable to parse '%s' as a float: %w", s, err))
			return
		}
		cfg.DisplayHeight = v
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

	saveButton := widget.NewButtonWithIcon("Save", theme.DocumentSaveIcon(), func() {
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
			widget.NewLabel("Source:"),
			chatSourceSelect,
			widget.NewLabel("Source image size (use '0' for preserving the original size or ratio):"),
			container.NewHBox(widget.NewLabel("X:"), sourceWidth, widget.NewLabel(`px`), widget.NewSeparator(), widget.NewLabel("Y:"), sourceHeight, widget.NewLabel(`px`)),
			widget.NewLabel("Format:"),
			imageFormatSelect,
			widget.NewLabel("Quality:"),
			imageQuality,
			widget.NewLabel("Update interval:"),
			container.NewHBox(updateInterval, widget.NewLabel("seconds")),
			widget.NewLabel("Z-Index / layer:"),
			zIndex,
			widget.NewLabel("Display size:"),
			container.NewHBox(widget.NewLabel("X:"), displayWidth, widget.NewLabel(`%`), widget.NewSeparator(), widget.NewLabel("Y:"), displayHeight, widget.NewLabel(`%`)),
			widget.NewLabel("Align:"),
			container.NewHBox(widget.NewLabel("X:"), alignX, widget.NewSeparator(), widget.NewLabel("Y:"), alignY),
			widget.NewLabel("Offset:"),
			container.NewHBox(widget.NewLabel("X:"), offsetX, widget.NewLabel(`%`), widget.NewSeparator(), widget.NewLabel("Y:"), offsetY, widget.NewLabel(`%`)),
		),
	)
	w.SetContent(content)
	w.Show()
}
