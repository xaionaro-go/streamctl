package streampanel

import (
	"context"
	"image"
	"time"

	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"github.com/anthonynsimon/bild/adjust"
	"github.com/facebookincubator/go-belt/tool/logger"
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
		p.updateMonitorPage(ctx)

		t := time.NewTicker(200 * time.Millisecond)
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}

			p.updateMonitorPage(ctx)
		}
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

func (p *Panel) updateMonitorPage(
	ctx context.Context,
) {
	logger.Tracef(ctx, "updateMonitorPage")
	defer logger.Tracef(ctx, "/updateMonitorPage")

	{
		img, err := p.getImage(ctx, consts.ImageScreenshot)

		if err != nil {
			logger.Error(ctx, err)
		} else {
			s := p.screenshotContainer.Size()
			img = imgFitTo(img, image.Point{X: int(s.Width), Y: int(s.Height)})
			img = adjust.Brightness(img, -0.5)
			imgFyne := canvas.NewImageFromImage(img)
			imgFyne.FillMode = canvas.ImageFillOriginal

			p.screenshotContainer.Layout = layout.NewBorderLayout(imgFyne, nil, nil, nil)
			p.screenshotContainer.Objects = p.screenshotContainer.Objects[:0]
			p.screenshotContainer.Objects = append(p.screenshotContainer.Objects, imgFyne)
			p.screenshotContainer.Refresh()
		}
	}

	{
		img, err := p.getImage(ctx, consts.ImageChat)
		if err != nil {
			logger.Error(ctx, err)
		} else {
			s := p.chatContainer.Size()
			img = imgFitTo(img, image.Point{X: int(s.Width), Y: int(s.Height)})
			imgFyne := canvas.NewImageFromImage(img)
			imgFyne.FillMode = canvas.ImageFillOriginal

			p.chatContainer.Layout = layout.NewVBoxLayout()
			p.chatContainer.Objects = p.chatContainer.Objects[:0]
			p.chatContainer.Objects = append(p.chatContainer.Objects, layout.NewSpacer(), container.NewHBox(imgFyne, layout.NewSpacer()))
			p.chatContainer.Refresh()
		}
	}
}
