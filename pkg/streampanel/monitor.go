package streampanel

import (
	"context"
	"time"

	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/layout"
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

		t := time.NewTicker(time.Second)
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
		img, err := p.getImage(ctx, consts.VarKeyImage(consts.ImageScreenshot))
		if err != nil {
			logger.Error(ctx, err)
		} else {
			imgFyne := canvas.NewImageFromImage(img)
			imgFyne.FillMode = canvas.ImageFillOriginal

			p.screenshotContainer.Layout = layout.NewBorderLayout(imgFyne, nil, nil, nil)
			p.screenshotContainer.Objects = p.screenshotContainer.Objects[:0]
			p.screenshotContainer.Objects = append(p.screenshotContainer.Objects, imgFyne)
			p.screenshotContainer.Refresh()
		}
	}

	{
		img, err := p.getImage(ctx, consts.VarKeyImage(consts.ImageChat))
		if err != nil {
			logger.Error(ctx, err)
		} else {
			imgFyne := canvas.NewImageFromImage(img)
			imgFyne.FillMode = canvas.ImageFillContain

			p.chatContainer.RemoveAll()
			p.chatContainer.Add(imgFyne)
			p.chatContainer.Refresh()
		}
	}
}
