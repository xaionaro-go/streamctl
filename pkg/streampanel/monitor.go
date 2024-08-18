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
	logger.Debugf(ctx, "newMonitorSettingsWindow")
	defer logger.Debugf(ctx, "/newMonitorSettingsWindow")
	w := p.app.NewWindow("Monitor settings")
	resizeWindow(w, fyne.NewSize(1500, 1000))

	content := container.NewVBox()

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
					}
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
				return nil
			},
		)
	})
	content.Add(addButton)

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

	zIndex := xfyne.NewNumericalEntry()
	zIndex.SetText(fmt.Sprintf("%v", cfg.ZIndex))
	zIndex.OnChanged = func(s string) {
		if s == "" {
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

	chatWidth := xfyne.NewNumericalEntry()
	chatWidth.SetText(fmt.Sprintf("%v", cfg.Width))
	chatWidth.OnChanged = func(s string) {
		if s == "" {
			s = "0"
		}
		v, err := strconv.ParseFloat(s, 64)
		if err != nil {
			p.DisplayError(fmt.Errorf("unable to parse '%s' as a float: %w", s, err))
			return
		}
		cfg.Width = v
	}
	chatHeight := xfyne.NewNumericalEntry()
	chatHeight.SetText(fmt.Sprintf("%v", cfg.Height))
	chatHeight.OnChanged = func(s string) {
		if s == "" {
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

	chatAlignX := widget.NewSelect([]string{
		string(streamdconsts.AlignXLeft),
		string(streamdconsts.AlignXMiddle),
		string(streamdconsts.AlignXRight),
	}, func(s string) {
		cfg.AlignX = streamdconsts.AlignX(s)
	})
	chatAlignX.SetSelected(string(cfg.AlignX))

	chatAlignY := widget.NewSelect([]string{
		string(streamdconsts.AlignYTop),
		string(streamdconsts.AlignYMiddle),
		string(streamdconsts.AlignYBottom),
	}, func(s string) {
		cfg.AlignY = streamdconsts.AlignY(s)
	})
	chatAlignY.SetSelected(string(cfg.AlignY))

	chatOffsetX := xfyne.NewNumericalEntry()
	chatOffsetX.SetText(fmt.Sprintf("%v", cfg.OffsetX))
	chatOffsetX.OnChanged = func(s string) {
		if s == "" {
			s = "0"
		}
		v, err := strconv.ParseFloat(s, 64)
		if err != nil {
			p.DisplayError(fmt.Errorf("unable to parse '%s' as a float: %w", s, err))
			return
		}
		cfg.OffsetX = v
	}
	chatOffsetY := xfyne.NewNumericalEntry()
	chatOffsetY.SetText(fmt.Sprintf("%v", cfg.OffsetY))
	chatOffsetY.OnChanged = func(s string) {
		if s == "" {
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
			widget.NewLabel("Z-Index / layer:"),
			zIndex,
			widget.NewLabel("Size:"),
			container.NewHBox(widget.NewLabel("X:"), chatWidth, widget.NewLabel(`%`), widget.NewSeparator(), widget.NewLabel("Y:"), chatHeight, widget.NewLabel(`%`)),
			widget.NewLabel("Align:"),
			container.NewHBox(widget.NewLabel("X:"), chatAlignX, widget.NewSeparator(), widget.NewLabel("Y:"), chatAlignY),
			widget.NewLabel("Offset:"),
			container.NewHBox(widget.NewLabel("X:"), chatOffsetX, widget.NewLabel(`%`), widget.NewSeparator(), widget.NewLabel("Y:"), chatOffsetY, widget.NewLabel(`%`)),
		),
	)
	w.SetContent(content)
	w.Show()
}
