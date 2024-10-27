//go:build !android
// +build !android

package builtin

import (
	"context"
	"fmt"

	"fyne.io/fyne/v2/canvas"
	"github.com/xaionaro-go/streamctl/pkg/recoder/libav/recoder"
)

func (p *Player) initImageFor(
	_ context.Context,
	frame *recoder.Frame,
) error {
	var err error
	p.currentImage, err = frame.Data().GuessImageFormat()
	if err != nil {
		return fmt.Errorf("unable to guess the image format: %w", err)
	}
	p.canvasImage = canvas.NewImageFromImage(p.currentImage)
	p.window.SetContent(p.canvasImage)
	return nil
}
