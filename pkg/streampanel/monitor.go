package streampanel

import (
	"context"
	"sync"

	"image"
	"time"

	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
	"github.com/anthonynsimon/bild/adjust"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/obs"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/twitch"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/youtube"
	"github.com/xaionaro-go/streamctl/pkg/streampanel/consts"
)

func (p *Panel) startMonitorPage(
	ctx context.Context,
) {
	logger.Debugf(ctx, "startMonitorPage")
	defer logger.Debugf(ctx, "/startMonitorPage")

	p.monitorPageUpdaterLocker.Lock()
	defer p.monitorPageUpdaterLocker.Unlock()
	ctx, cancelFn := context.WithCancel(ctx)
	p.monitorPageUpdaterCancel = cancelFn
	go func(ctx context.Context) {
		p.updateMonitorPageImages(ctx)
		p.updateMonitorPageStreamStatus(ctx)

		go func() {
			t := time.NewTicker(200 * time.Millisecond)
			for {
				select {
				case <-ctx.Done():
					return
				case <-t.C:
				}

				p.updateMonitorPageImages(ctx)
			}
		}()

		go func() {
			t := time.NewTicker(2 * time.Second)
			for {
				select {
				case <-ctx.Done():
					return
				case <-t.C:
				}

				p.updateMonitorPageStreamStatus(ctx)
			}
		}()
	}(ctx)
}

func (p *Panel) stopMonitorPage(
	ctx context.Context,
) {
	logger.Debugf(ctx, "stopMonitorPage")
	defer logger.Debugf(ctx, "/stopMonitorPage")

	p.monitorPageUpdaterLocker.Lock()
	defer p.monitorPageUpdaterLocker.Unlock()

	if p.monitorPageUpdaterCancel == nil {
		return
	}

	p.monitorPageUpdaterCancel()
	p.monitorPageUpdaterCancel = nil
}

func (p *Panel) updateMonitorPageImages(
	ctx context.Context,
) {
	logger.Tracef(ctx, "updateMonitorPageImages")
	defer logger.Tracef(ctx, "/updateMonitorPageImages")

	p.monitorPageUpdaterLocker.Lock()
	defer p.monitorPageUpdaterLocker.Unlock()

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		img, changed, err := p.getImage(ctx, consts.ImageScreenshot)

		if err != nil {
			logger.Error(ctx, err)
		} else {
			if !changed {
				return
			}
			//s := p.mainWindow.Canvas().Size()
			//img = imgFitTo(img, image.Point{X: 1450, Y: 1450})
			img = adjust.Brightness(img, -0.5)
			imgFyne := canvas.NewImageFromImage(img)
			imgFyne.FillMode = canvas.ImageFillOriginal

			p.screenshotContainer.Layout = layout.NewBorderLayout(imgFyne, nil, nil, nil)
			p.screenshotContainer.Objects = p.screenshotContainer.Objects[:0]
			p.screenshotContainer.Objects = append(p.screenshotContainer.Objects, imgFyne)
			p.screenshotContainer.Refresh()
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		img, changed, err := p.getImage(ctx, consts.ImageChat)
		if err != nil {
			logger.Error(ctx, err)
		} else {
			if !changed {
				return
			}
			//s := p.mainWindow.Canvas().Size()
			//img = imgFitTo(img, image.Point{X: int(s.Width), Y: int(s.Height)})
			img = imgFitTo(img, image.Point{X: 1450, Y: 1450})
			imgFyne := canvas.NewImageFromImage(img)
			imgFyne.FillMode = canvas.ImageFillOriginal

			p.chatContainer.Layout = layout.NewVBoxLayout()
			p.chatContainer.Objects = p.chatContainer.Objects[:0]
			p.chatContainer.Objects = append(p.chatContainer.Objects, layout.NewSpacer(), container.NewHBox(imgFyne, layout.NewSpacer()))
			p.chatContainer.Refresh()
		}
	}()

}

func (p *Panel) updateMonitorPageStreamStatus(
	ctx context.Context,
) {
	logger.Tracef(ctx, "updateMonitorPageStreamStatus")
	defer logger.Tracef(ctx, "/updateMonitorPageStreamStatus")

	p.monitorPageUpdaterLocker.Lock()
	defer p.monitorPageUpdaterLocker.Unlock()

	var wg sync.WaitGroup
	for _, platID := range []streamcontrol.PlatformName{
		obs.ID,
		youtube.ID,
		twitch.ID,
	} {
		wg.Add(1)
		go func() {
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
		}()
	}

	wg.Wait()
}
