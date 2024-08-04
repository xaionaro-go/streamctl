package streampanel

import (
	"context"
	"runtime"
	"sync"

	"image"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/widget"
	"github.com/anthonynsimon/bild/adjust"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/observability"
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

	go func(ctx context.Context) {
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
	}(ctx)
}

func (p *Panel) updateMonitorPageImages(
	ctx context.Context,
) {
	logger.Tracef(ctx, "updateMonitorPageImages")
	defer logger.Tracef(ctx, "/updateMonitorPageImages")

	var winSize fyne.Size
	switch runtime.GOOS {
	default:
		winSize = p.monitorPage.Size()
	}
	lastWinSize := p.monitorLastWinSize
	p.monitorLastWinSize = winSize

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
		} else {
			if !changed && lastWinSize == winSize {
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
		}
	})

	wg.Add(1)
	observability.Go(ctx, func() {
		defer wg.Done()
		img, changed, err := p.getImage(ctx, consts.ImageChat)
		if err != nil {
			logger.Error(ctx, err)
		} else {
			if !changed && lastWinSize == winSize {
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
		}
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
