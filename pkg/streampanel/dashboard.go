package streampanel

import (
	"context"
	"encoding/json"
	"fmt"
	"image/color"
	"image/draw"
	"math"
	"os"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
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
	"github.com/davecgh/go-spew/spew"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/lusingander/colorpicker"
	"github.com/xaionaro-go/iterate"
	"github.com/xaionaro-go/obs-grpc-proxy/protobuf/go/obs_grpc"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/streamctl/pkg/colorx"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/kick"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/obs"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/twitch"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/youtube"
	"github.com/xaionaro-go/streamctl/pkg/streamd/api"
	"github.com/xaionaro-go/streamctl/pkg/streamd/client"
	streamdconfig "github.com/xaionaro-go/streamctl/pkg/streamd/config"
	streamdconsts "github.com/xaionaro-go/streamctl/pkg/streamd/consts"
	"github.com/xaionaro-go/streamctl/pkg/streampanel/consts"
	"github.com/xaionaro-go/streamctl/pkg/ximage"
	xfyne "github.com/xaionaro-go/xfyne/widget"
	"github.com/xaionaro-go/xpath"
	"github.com/xaionaro-go/xsync"
)

const (
	dashboardDebug               = false
	dashboardFullUpdatesInterval = 2 * time.Second
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
	imagesLocker        xsync.RWMutex
	imagesLayerObj      *canvas.Raster
	images              []imageInfo
	renderedImagesLayer *image.NRGBA
	streamStatus        map[streamcontrol.PlatformName]*widget.Label
	streamStatusLocker  xsync.Mutex
	changedImages       []*imageInfo
	lastFullUpdateAt    time.Time
	chat                *chatUI

	iteratorReusableBuffer iterate.TwoDReusableBuffers
}

type imageInfo struct {
	ElementName string
	streamdconfig.DashboardElementConfig
	Image *image.NRGBA
}

func (w *dashboardWindow) renderLocalStatus(ctx context.Context) {
	// TODO: remove the ugly hardcode above, and make it generic (support different use cases)

	b, err := os.ReadFile(must(xpath.Expand(`~/quality`)))
	if err != nil {
		logger.Debugf(ctx, "unable to open the 'quality' file: %v", err)
		return
	}
	words := strings.Split(strings.Trim(string(b), "\n\r\t"), " ")
	if len(words) != 3 {
		logger.Debugf(ctx, "expected 3 words, but received %d", len(words))
		return
	}

	aQ, err := strconv.ParseInt(words[0], 10, 64)
	if err != nil {
		logger.Debugf(ctx, "unable to parse aQ '%s': %v", words[0], err)
		return
	}

	pQ, err := strconv.ParseInt(words[1], 10, 64)
	if err != nil {
		logger.Debugf(ctx, "unable to parse pQ '%s': %v", words[0], err)
		return
	}

	wQ, err := strconv.ParseInt(words[2], 10, 64)
	if err != nil {
		logger.Debugf(ctx, "unable to parse wQ '%s': %v", words[0], err)
		return
	}

	qToImportance := func(in int64) widget.Importance {
		switch {
		case in <= -5:
			return widget.DangerImportance
		case in <= 0:
			return widget.MediumImportance
		default:
			return widget.SuccessImportance
		}
	}

	qToStr := func(in int64) string {
		if in <= -30 {
			return "DEAD"
		}
		if in <= -5 {
			return "BAD"
		}
		if in <= 0 {
			return "SO-SO"
		}
		if in > 0 {
			return "GOOD"
		}
		return "UNKNOWN"
	}

	qToLabel := func(name string, q int64) *widget.Label {
		l := widget.NewLabel(name + ":" + qToStr(q))
		l.Importance = qToImportance(q)
		l.TextStyle.Bold = true
		return l
	}

	w.localStatus.Objects = []fyne.CanvasObject{
		container.NewHBox(
			layout.NewSpacer(),
			qToLabel("A", aQ),
			qToLabel("P", pQ),
			qToLabel("W", wQ),
		),
	}
}

func (w *dashboardWindow) renderStreamStatus(ctx context.Context) {
	w.streamStatusLocker.Do(ctx, func() {
		streamDClient, ok := w.StreamD.(*client.Client)
		if !ok {
			return
		}
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
	})

	w.Panel.streamStatusLocker.Do(ctx, func() {
		w.streamStatusLocker.Do(ctx, func() {
			for platID, dst := range w.streamStatus {
				observability.CallSafe(ctx, func() {
					src := w.Panel.streamStatus[platID]
					if src == nil {
						logger.Debugf(ctx, "status for '%s' is not set", platID)
						return
					}
					defer dst.Refresh()

					if !src.BackendIsEnabled {
						dst.SetText("disabled")
						return
					}

					if src.BackendError != nil {
						dst.Importance = widget.LowImportance
						dst.SetText("error")
						return
					}

					if !src.IsActive {
						dst.Importance = widget.DangerImportance
						dst.SetText("stopped")
						return
					}

					dst.Importance = widget.SuccessImportance
					if src.StartedAt == nil {
						dst.SetText("started")
						return
					}

					duration := time.Since(*src.StartedAt)

					viewerCountString := ""
					if src.ViewersCount != nil {
						viewerCountString = fmt.Sprintf(" (%d)", *src.ViewersCount)
					}

					dst.SetText(fmt.Sprintf("%s%s", duration.Truncate(time.Second).String(), viewerCountString))
				})
			}
		})
	})
}

func (p *Panel) newDashboardWindow(
	ctx context.Context,
) *dashboardWindow {
	chatUI, err := newChatUI(ctx, false, false, true, p)
	if err != nil {
		p.DisplayError(fmt.Errorf("unable to start a chat UI: %w", err))
	}
	w := &dashboardWindow{
		Window:       p.app.NewWindow("Dashboard"),
		Panel:        p,
		chat:         chatUI,
		streamStatus: map[streamcontrol.PlatformName]*widget.Label{},
	}
	for _, platID := range []streamcontrol.PlatformName{
		obs.ID,
		twitch.ID,
		kick.ID,
		youtube.ID,
	} {
		l := widget.NewLabel("")
		l.TextStyle.Bold = true
		w.streamStatus[platID] = l
	}

	bg := image.NewGray(image.Rect(0, 0, 1, 1))
	bgFyne := canvas.NewImageFromImage(bg)
	bgFyne.FillMode = canvas.ImageFillStretch

	p.localStatus = container.NewStack()
	p.appStatus = widget.NewLabel("")
	p.appStatus.TextStyle.Bold = true
	obsLabel := widget.NewLabel("OBS:")
	obsLabel.Importance = widget.HighImportance
	obsLabel.TextStyle.Bold = true
	p.streamStatus[obs.ID] = &streamStatus{}
	twLabel := widget.NewLabel("TW:")
	twLabel.Importance = widget.HighImportance
	twLabel.TextStyle.Bold = true
	p.streamStatus[twitch.ID] = &streamStatus{}
	kcLabel := widget.NewLabel("Kc:")
	kcLabel.Importance = widget.HighImportance
	kcLabel.TextStyle.Bold = true
	p.streamStatus[kick.ID] = &streamStatus{}
	ytLabel := widget.NewLabel("YT:")
	ytLabel.Importance = widget.HighImportance
	ytLabel.TextStyle.Bold = true
	p.streamStatus[youtube.ID] = &streamStatus{}
	streamInfoItems := container.NewVBox()
	if _, ok := p.StreamD.(*client.Client); ok {
		appLabel := widget.NewLabel("App:")
		appLabel.Importance = widget.HighImportance
		appLabel.TextStyle.Bold = true
		streamInfoItems.Add(container.NewHBox(
			appLabel,
			p.appStatus,
			layout.NewSpacer(),
		))
	}
	streamInfoItems.Add(container.NewHBox(p.localStatus, layout.NewSpacer()))
	streamInfoItems.Add(container.NewHBox(obsLabel, w.streamStatus[obs.ID], layout.NewSpacer()))
	streamInfoItems.Add(container.NewHBox(twLabel, w.streamStatus[twitch.ID], layout.NewSpacer()))
	streamInfoItems.Add(container.NewHBox(kcLabel, w.streamStatus[kick.ID], layout.NewSpacer()))
	streamInfoItems.Add(container.NewHBox(ytLabel, w.streamStatus[youtube.ID], layout.NewSpacer()))
	streamInfoContainer := container.NewBorder(
		nil,
		nil,
		streamInfoItems,
		nil,
	)
	w.imagesLayerObj = canvas.NewRaster(w.imagesLayer)

	layers := []fyne.CanvasObject{
		bgFyne,
	}
	if w.chat != nil {
		c := w.chat.List
		w.chat.OnAdd = func(ctx context.Context, _ api.ChatMessage) {
			screenHeight := w.Canvas().Size().Height
			switch runtime.GOOS {
			case "android":
				screenHeight -= 110 // TODO: delete me
			}
			demandedHeight := float32(w.chat.TotalListHeight) + c.Theme().Size(theme.SizeNamePadding)*float32(c.Length())
			logger.Tracef(ctx, "demanded height: %v; screen height: %v", demandedHeight, screenHeight)
			allowedHeight := math.Min(
				float64(screenHeight),
				float64(demandedHeight),
			)
			logger.Tracef(ctx, "allowed height: %v", allowedHeight)
			pos := fyne.NewPos(0, screenHeight-float32(allowedHeight))
			size := fyne.NewSize(w.Canvas().Size().Width, float32(allowedHeight))
			logger.Tracef(ctx, "resulting size and position: %#+v %#+v", size, pos)
			c.Resize(size)
			c.Move(pos)

			fyneTryLoop(ctx, func() { c.ScrollToBottom() })
			fyneTryLoop(ctx, func() { c.Refresh() })
			fyneTryLoop(ctx, func() { c.ScrollToBottom() })
			fyneTryLoop(ctx, func() { c.Refresh() })
		}
		w.chat.OnAdd(ctx, api.ChatMessage{})
		layers = append(layers,
			c,
		)
	}
	layers = append(layers,
		w.imagesLayerObj,
		streamInfoContainer,
	)

	stack := container.NewStack(layers...)
	w.Window.SetContent(stack)
	w.Window.Show()
	return w
}

func (w *dashboardWindow) imagesLayer(width, height int) (_ret image.Image) {
	ctx := context.TODO()
	logger.Tracef(ctx, "imagesLayer(%d, %d)", width, height)
	defer func() { logger.Tracef(ctx, "/imagesLayer(%d, %d): size:%v", width, height, _ret.Bounds()) }()
	ctx = xsync.WithNoLogging(ctx, true)
	return xsync.DoR1(ctx, &w.imagesLocker, func() image.Image {
		return w.renderImagesNoLock(ctx, width, height)
	})
}

func (w *dashboardWindow) renderImagesNoLock(
	ctx context.Context,
	width, height int,
) image.Image {
	if w.renderedImagesLayer == nil {
		w.renderedImagesLayer = image.NewNRGBA(image.Rectangle{
			Min: image.Point{},
			Max: image.Point{
				X: width,
				Y: height,
			},
		})
	}

	sizeChanged := false
	dstImg := w.renderedImagesLayer
	dstSize := dstImg.Bounds().Size()
	if dstSize.X != width || dstSize.Y != height {
		w.renderedImagesLayer = image.NewNRGBA(image.Rectangle{
			Min: image.Point{},
			Max: image.Point{
				X: width,
				Y: height,
			},
		})
		dstImg = w.renderedImagesLayer
		dstSize = dstImg.Bounds().Size()
		sizeChanged = true
	}

	canvasRatio := float64(dstSize.X) / float64(dstSize.Y)

	logger.Tracef(ctx, "len(w.changedImages) == %d", len(w.changedImages))
	imgIsChanged := map[string]struct{}{}
	for _, img := range w.changedImages {
		if dashboardDebug {
			logger.Tracef(ctx, "changed element: '%s'", img.ElementName)
		}
		imgIsChanged[img.ElementName] = struct{}{}
	}

	type imageT struct {
		Image     *ximage.Transform
		Name      string
		IsChanged bool
		Scaling   float64
	}
	transformedImages := make([]*imageT, 0, len(w.images))
	for idx, img := range w.images {
		if img.Image == nil {
			logger.Tracef(
				ctx,
				"image '%s' %d (%s) is not set",
				img.ElementName,
				idx,
				spew.Sdump(img.DashboardElementConfig),
			)
			continue
		}
		imgSize := img.Image.Bounds().Size()
		imgRatio := float64(imgSize.X) / float64(imgSize.Y)
		imgCanvasRatio := imgRatio / canvasRatio
		imgWidth := img.Width
		imgHeight := img.Height
		switch {
		case imgCanvasRatio == 1:
		case imgCanvasRatio > 1:
			imgHeight /= imgCanvasRatio
		case imgCanvasRatio < 1:
			imgWidth *= imgCanvasRatio
		}
		var xMin, yMin float64
		switch img.AlignX {
		case streamdconsts.AlignXLeft:
			xMin = img.OffsetX
		case streamdconsts.AlignXMiddle:
			xMin = (100-imgWidth)/2 + img.OffsetX
		case streamdconsts.AlignXRight:
			xMin = (100 - imgWidth) + img.OffsetX
		}
		xMax := xMin + imgWidth
		switch img.AlignY {
		case streamdconsts.AlignYTop:
			yMin = img.OffsetY
		case streamdconsts.AlignYMiddle:
			yMin = (100-imgHeight)/2 + img.OffsetY
		case streamdconsts.AlignYBottom:
			yMin = (100 - imgHeight) + img.OffsetY
		}
		yMax := yMin + imgHeight
		rectangle := ximage.RectangleFloat64{
			Min: ximage.PointFloat64{
				X: xMin / 100,
				Y: yMin / 100,
			},
			Max: ximage.PointFloat64{
				X: xMax / 100,
				Y: yMax / 100,
			},
		}
		_, isChanged := imgIsChanged[img.ElementName]
		logger.Tracef(
			ctx,
			"transformation rectangle (isChanged:%t) for '%s' %d (%s) is %v",
			isChanged,
			img.ElementName,
			idx,
			spew.Sdump(img.DashboardElementConfig),
			rectangle,
		)
		tImg := ximage.NewTransform(img.Image, color.NRGBAModel, rectangle)
		transformedImages = append(
			transformedImages,
			&imageT{
				Image:     tImg,
				Name:      img.ElementName,
				IsChanged: isChanged,
				Scaling: min(
					(float64(dstSize.X)*tImg.To.Size().X)/(float64(tImg.ImageSize.X)),
					(float64(dstSize.Y)*tImg.To.Size().Y)/(float64(tImg.ImageSize.Y)),
				),
			},
		)
	}

	if dashboardDebug {
		for tIdx, tImg := range transformedImages {
			logger.Tracef(ctx, "transformedImages[%d]: %s", tIdx, tImg.Name)
		}
	}

	var changedAreas []iterate.Area
	for _, tImg := range transformedImages {
		x0F := tImg.Image.To.Min.X
		y0F := tImg.Image.To.Min.Y
		x1F := tImg.Image.To.Max.X
		y1F := tImg.Image.To.Max.Y
		if x1F < x0F {
			logger.Errorf(ctx, "x1F < x0F: %v < %v", x1F, x0F)
		}
		if y1F < y0F {
			logger.Errorf(ctx, "y1F < y0F: %v < %v", y1F, y0F)
		}
		x0 := int(x0F*float64(width) + 0.5)
		x1 := int(x1F*float64(width) + 0.5)
		y0 := int(y0F*float64(height) + 0.5)
		y1 := int(y1F*float64(height) + 0.5)
		rect := iterate.Rect[string](x0, y0, x1, y1)
		rect.Data = tImg.Name
		changedAreas = append(changedAreas, rect)
	}

	changedPoints := iterate.TwoDPointsCount(&w.iteratorReusableBuffer, changedAreas...)
	if changedPoints == 0 && !sizeChanged {
		return dstImg
	}
	totalPoints := width * height
	onlyDelta := !sizeChanged && float32(changedPoints) < float32(totalPoints)*0.75

	now := time.Now()
	if dashboardFullUpdatesInterval > 0 && now.Sub(w.lastFullUpdateAt) < dashboardFullUpdatesInterval {
		onlyDelta = false
		w.lastFullUpdateAt = now
	}

	if onlyDelta {
		for y, xSegments := range iterate.TwoDForEachY(&w.iteratorReusableBuffer, changedAreas...) {
			idx := y * dstImg.Stride
			for _, xSegment := range xSegments {
				for x := xSegment.S; x < xSegment.E; x++ {
					idx := idx + x*4
					dstImg.Pix[idx+3] = 0 // setting only alpha to zero, as a faster way to erase the picture; TODO: use SIMD instead
				}
			}
		}
	} else {
		for i := 3; i < len(dstImg.Pix); i += 4 { // setting only alpha to zero, as a faster way to erase the picture
			dstImg.Pix[i] = 0
		}
		changedAreas = changedAreas[:0]
		changedAreas = append(changedAreas, iterate.Rect[struct{}](0, 0, width, height))
	}
	if dashboardDebug {
		changedAreasBytes, _ := json.Marshal(changedAreas)
		logger.Debugf(ctx, "changedPoints: %d/%d; changedAreas: %s", changedPoints, totalPoints, changedAreasBytes)
	}

	var tImgs []*imageT
	var xBoundaries []float64
	for y, xSegments := range iterate.TwoDForEachY(&w.iteratorReusableBuffer, changedAreas...) {
		if dashboardDebug && y == height/2 {
			xSegmentsBytes, _ := json.Marshal(xSegments)
			logger.Tracef(ctx, "y:%d; xSegments:%s", y, xSegmentsBytes)
		}
		yF := (float64(y) + 0.5) / float64(height)
		idxY := (y - dstImg.Rect.Min.Y) * dstImg.Stride
		xBoundaries = xBoundaries[:0]
		xBoundaries = append(xBoundaries, 0)
		for _, tImg := range transformedImages {
			if yF < tImg.Image.To.Min.Y || yF > tImg.Image.To.Max.Y {
				continue
			}
			xBoundaries = append(
				xBoundaries,
				float64(tImg.Image.To.Min.X),
				float64(tImg.Image.To.Max.X),
			)
		}
		slices.Sort(xBoundaries)
		{
			i, j := 1, 1
			for ; i < len(xBoundaries); i++ {
				if xBoundaries[j-1] == xBoundaries[i] {
					continue
				}
				xBoundaries[j] = xBoundaries[i]
				j++
			}
			xBoundaries = xBoundaries[:j]
		}
		if y == height/2 {
			logger.Tracef(ctx, "xBoundaries == %#+v", xBoundaries)
		}

		boundaryIdx := -1
		nextSwitchX := float64(-1)
		for _, xSegment := range xSegments {
			for x := xSegment.S; x < xSegment.E; x++ {
				idxX := (x - dstImg.Rect.Min.X) * 4
				idx := idxY + idxX
				if dstImg.Pix[idx+3] > 0 { // alpha > 0
					if dashboardDebug && x == width/2 && y == height/2 {
						logger.Tracef(ctx, "dst alpha > 0")
					}
					continue
				}
				xF := (float64(x) + 0.5) / float64(width)
				if dashboardDebug && x == width/2 && y == height/2 {
					logger.Tracef(ctx, "xF == %f; nextSwitchX == %f", xF, nextSwitchX)
				}
				if xF > nextSwitchX {
					tImgs = tImgs[:0]
					for tImgIdx, tImg := range transformedImages {
						c := tImg.Image.AtFloat64(xF, yF)
						if c == nil {
							if dashboardDebug && x == width/2 && y == height/2 {
								logger.Tracef(ctx, "transformedImages[%d]: %s: no color", tImgIdx, tImg.Name)
							}
							continue
						}
						tImgs = append(tImgs, tImg)
					}
					boundaryIdx++
					if boundaryIdx < len(xBoundaries) {
						nextSwitchX = xBoundaries[boundaryIdx]
					}
				}
				if dashboardDebug && x == width/2 && y == height/2 {
					logger.Tracef(ctx, "len(tImgs) == %d; xF == %f; yF == %f", len(tImgs), xF, yF)
				}
				if len(tImgs) == 0 {
					continue
				}
				dstOffset := dstImg.PixOffset(x, y)
				if dstOffset < 0 || dstOffset+4 > len(dstImg.Pix) {
					continue
				}
				for tIdx, tImg := range tImgs {
					if dashboardDebug && x == width/2 && y == height/2 {
						logger.Tracef(ctx, "tImgs[%d]: name:%s", tIdx, tImg.Name)
					}
					srcX, srcY, ok := tImg.Image.Coords(xF, yF)
					if !ok {
						if dashboardDebug && x == width/2 && y == height/2 {
							logger.Tracef(ctx, "tImgs[%d]: continue: no color", tIdx)
						}
						continue
					}
					srcImg := tImg.Image.Image.(*image.NRGBA)
					srcOffset := srcImg.PixOffset(srcX, srcY)
					if srcOffset < 0 || srcOffset+4 >= len(srcImg.Pix) {
						if dashboardDebug && x == width/2 && y == height/2 {
							logger.Tracef(ctx, "tImgs[%d]: continue: outside of the bounds", tIdx)
						}
						continue
					}
					if srcImg.Pix[srcOffset+3] == 0 { // alpha == 0
						if dashboardDebug && x == width/2 && y == height/2 {
							logger.Tracef(ctx, "tImgs[%d]: continue: alpha == 0", tIdx)
						}
						continue
					}
					xE, yE := 0, 0
					if tIdx == 0 {
						xE = max(0, int(float64(x+1)*tImg.Scaling)-int(float64(x)*tImg.Scaling)-1)
						yE = max(0, int(float64(y+1)*tImg.Scaling)-int(float64(y)*tImg.Scaling)-1)
					}
					for i := 0; i <= yE; i++ {
						dstOffset := dstImg.PixOffset(x, y+i)
						dstOffsetEnd := dstOffset + 4*xE
						for ; dstOffset <= dstOffsetEnd; dstOffset += 4 {
							if dstOffset < 0 || dstOffset+4 >= len(dstImg.Pix) {
								continue
							}
							if dstImg.Pix[dstOffset+3] > 0 { // alpha > 0
								continue
							}
							copy(
								dstImg.Pix[dstOffset:dstOffset+4:dstOffset+4],
								srcImg.Pix[srcOffset:srcOffset+4:srcOffset+4],
							)
						}
					}
					if dashboardDebug && x == width/2 && y == height/2 {
						logger.Tracef(ctx, "tImgs[%d]: break", tIdx)
					}
					break
				}
			}
		}
	}

	w.changedImages = w.changedImages[:0]
	return dstImg
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

	observability.Go(ctx, func() {
		w.Panel.app.Driver()
		t := time.NewTicker(time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				w.chat.OnAdd(ctx, api.ChatMessage{})
			}
		}
	})

	w.renderLocalStatus(ctx)
	observability.Go(ctx, func() {
		t := time.NewTicker(2 * time.Second)
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}

			w.renderLocalStatus(ctx)
		}
	})

	cfg, err := w.GetStreamDConfig(ctx)
	if err != nil {
		w.DisplayError(fmt.Errorf("unable to get the current config: %w", err))
		return
	}

	observability.Go(ctx, func() {
		w.updateImages(ctx, cfg.Dashboard)
		w.updateStreamStatus(ctx)
		w.renderStreamStatus(ctx)

		observability.Go(ctx, func() {
			t := time.NewTicker(1000 * time.Millisecond)
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
				w.renderStreamStatus(ctx)
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
	ctx, cancelFn := context.WithCancel(ctx)
	p.dashboardShowHideButton.SetText("Hide")
	p.dashboardShowHideButton.SetIcon(theme.WindowCloseIcon())
	if p.dashboardWindow != nil {
		p.dashboardWindow.RequestFocus()
	} else {
		p.dashboardWindow = p.newDashboardWindow(ctx)
	}
	w := p.dashboardWindow
	w.startUpdating(ctx)
	var cfg *Config
	p.configLocker.Do(ctx, func() {
		cfg = &p.Config
		w.Window.Resize(fyne.NewSize(float32(cfg.Dashboard.Size.Width), float32(cfg.Dashboard.Size.Height)))
	})
	w.Window.SetOnClosed(func() {
		p.dashboardLocker.Do(ctx, func() {
			logger.Debugf(ctx, "dashboard.Close")
			defer logger.Debugf(ctx, "/dashboard.Close")
			w.stopUpdating(ctx)
			p.dashboardShowHideButton.SetText("Open")
			p.dashboardShowHideButton.SetIcon(theme.ComputerIcon())
			p.dashboardWindow = nil

			s := w.Window.Canvas().Size()
			w, h := uint(s.Width), uint(s.Height)
			if w == cfg.Dashboard.Size.Width && h == cfg.Dashboard.Size.Height {
				return
			}
			cfg.Dashboard.Size.Width = w
			cfg.Dashboard.Size.Height = h
			err := p.SaveConfig(ctx)
			if err != nil {
				logger.Errorf(ctx, "SaveConfig error: %v", err)
			}
			cancelFn()
		})
	})
	return nil
}

func (w *dashboardWindow) updateImages(
	ctx context.Context,
	dashboardCfg streamdconfig.DashboardConfig,
) {
	logger.Debugf(ctx, "updateImages")
	defer logger.Debugf(ctx, "/updateImages")

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

	const screenshotElementName = "screenshot"

	elements := make([]imageInfo, 0, 1+len(dashboardCfg.Elements))
	elements = append(elements, imageInfo{
		ElementName: screenshotElementName,
		DashboardElementConfig: streamdconfig.DashboardElementConfig{
			Width:  100,
			Height: 100,
			AlignX: streamdconsts.AlignXMiddle,
			AlignY: streamdconsts.AlignYTop,
			ZIndex: -math.MaxFloat64,
		},
	})
	for elName, el := range dashboardCfg.Elements {
		elements = append(elements, imageInfo{
			ElementName:            elName,
			DashboardElementConfig: el,
		})
	}
	sort.Slice(elements, func(i, j int) bool {
		if elements[i].ZIndex != elements[j].ZIndex {
			return elements[i].ZIndex > elements[j].ZIndex
		}
		return elements[i].ElementName > elements[j].ElementName
	})

	elementsMap := map[string]*imageInfo{}
	for idx := range elements {
		item := &elements[idx]
		elementsMap[item.ElementName] = item
	}

	for _, item := range w.images {
		obj := elementsMap[item.ElementName]
		if obj == nil {
			continue
		}
		obj.Image = item.Image
	}
	w.imagesLocker.Do(xsync.WithNoLogging(ctx, true), func() {
		w.images = elements
	})

	changedElements := make(chan *imageInfo, 10)
	var wg sync.WaitGroup

	for idx := range elements {
		{
			el := &elements[idx]
			if el == elementsMap[screenshotElementName] {
				continue
			}
			wg.Add(1)
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
				b := img.Bounds()
				m := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
				draw.Draw(m, m.Bounds(), img, b.Min, draw.Src)
				w.imagesLocker.Do(xsync.WithNoLogging(ctx, true), func() {
					el.Image = m
				})
				changedElements <- el
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
		logger.Tracef(ctx,
			"updating the screenshot image: %v %#+v %#+v",
			changed, lastWinSize, winSize,
		)
		b := img.Bounds()
		m := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
		draw.Draw(m, m.Bounds(), img, b.Min, draw.Src)
		var el *imageInfo
		w.imagesLocker.Do(xsync.WithNoLogging(ctx, true), func() {
			el = elementsMap[screenshotElementName]
			el.Image = m
		})
		logger.Tracef(ctx, "push screenshot to changedElements")
		changedElements <- el
	})

	var receiverWG sync.WaitGroup
	changedCount := 0
	w.imagesLocker.Do(xsync.WithNoLogging(ctx, true), func() {
		w.changedImages = w.changedImages[:0]
		receiverWG.Add(1)
		go func() {
			defer receiverWG.Done()
			for el := range changedElements {
				logger.Tracef(ctx, "<-changedElement: %s", el.ElementName)
				changedCount++
				w.changedImages = append(w.changedImages, el)
			}
		}()
	})
	wg.Wait()
	close(changedElements)
	receiverWG.Wait()

	if changedCount > 0 {
		w.imagesLayerObj.Refresh()
	}
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
		volumeColorActiveParsed = color.NRGBA{R: 0, G: 255, B: 0, A: 255}
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
		volumeColorPassiveParsed = color.NRGBA{R: 0, G: 0, B: 0, A: 0}
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
		string(streamdconfig.DashboardSourceImageTypeStreamScreenshot),
		string(streamdconfig.DashboardSourceImageTypeOBSScreenshot),
		string(streamdconfig.DashboardSourceImageTypeOBSVolume),
		string(streamdconfig.DashboardSourceImageTypeDummy),
	}, func(s string) {
		switch streamdconfig.DashboardSourceImageType(s) {
		case streamdconfig.DashboardSourceImageTypeOBSScreenshot:
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
	sourceTypeSelect.SetSelected(string(streamdconfig.DashboardSourceImageTypeOBSScreenshot))

	saveButton := widget.NewButtonWithIcon("Save", theme.DocumentSaveIcon(), func() {
		if elementName.Text == "" {
			p.DisplayError(fmt.Errorf("element name is not set"))
			return
		}
		switch streamdconfig.DashboardSourceImageType(sourceTypeSelect.Selected) {
		case streamdconfig.DashboardSourceImageTypeOBSScreenshot:
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
