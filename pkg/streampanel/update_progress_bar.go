package streampanel

import (
	"context"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
	"github.com/facebookincubator/go-belt/tool/logger"
)

type UpdateProgressBar struct {
	CloseOnce   sync.Once
	Panel       *Panel
	Window      fyne.Window
	Label       *widget.Label
	ProgressBar *widget.ProgressBar
}

func (p *Panel) NewUpdateProgressBar() *UpdateProgressBar {
	w := p.app.NewWindow("Updating...")

	label := widget.NewLabel("Updating the application...")
	progressBar := widget.NewProgressBar()
	progressBar.SetValue(0)
	w.SetContent(container.NewBorder(
		label,
		nil,
		nil,
		nil,
		progressBar,
	))
	w.Show()
	return &UpdateProgressBar{
		Panel:       p,
		Window:      w,
		Label:       label,
		ProgressBar: progressBar,
	}
}

func (u *UpdateProgressBar) SetProgress(progress float64) {
	ctx := context.TODO()
	logger.Debugf(ctx, "SetProgress(%f)", progress)
	u.ProgressBar.SetValue(progress)
	if progress >= 1.0 {
		u.CloseOnce.Do(func() {
			u.Label.SetText("Finished, restarting the application...")
			time.Sleep(time.Second)
			u.Window.Close()
		})
	}
}
