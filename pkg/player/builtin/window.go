package builtin

import (
	"context"
	"image"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"github.com/xaionaro-go/streamctl/pkg/audio"
)

type WindowRenderer struct {
	window      fyne.Window
	canvasImage *canvas.Image
}

func (r *WindowRenderer) SetImage(img image.Image) error {
	r.canvasImage = canvas.NewImageFromImage(img)
	r.window.SetContent(r.canvasImage)
	return nil
}

func (r *WindowRenderer) Render() error {
	r.canvasImage.Refresh()
	return nil
}

func (r *WindowRenderer) SetVisible(visible bool) error {
	if visible {
		r.window.Show()
	} else {
		r.window.Hide()
	}
	return nil
}

func NewWindow(
	ctx context.Context,
	title string,
) *Player {
	return New(
		ctx,
		&WindowRenderer{
			window: fyne.CurrentApp().NewWindow(title),
		},
		audio.NewAudioAuto(ctx),
	)
}
