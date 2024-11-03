package streampanel

import (
	"bytes"
	"context"
	"fmt"
	"image/png"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"github.com/goccy/go-yaml"
	"github.com/xaionaro-go/streamctl/pkg/consts"

	qrcode "github.com/skip2/go-qrcode"
)

func (p *Panel) showLinkDeviceQRWindow(
	ctx context.Context,
) {
	cfg, err := p.GetStreamDConfig(ctx)
	if err != nil {
		p.DisplayError(fmt.Errorf("unable to get the streamd config: %w", err))
		return
	}

	b, err := yaml.Marshal(cfg.P2PNetwork)
	if err != nil {
		p.DisplayError(fmt.Errorf("unable to serialize the P2PNetwork config: %w", err))
		return
	}

	pngBytes, err := qrcode.Encode(string(b), qrcode.Highest, 400)
	if err != nil {
		p.DisplayError(fmt.Errorf("unable to generate the QR code: %w", err))
		return
	}

	img, err := png.Decode(bytes.NewReader(pngBytes))
	if err != nil {
		p.DisplayError(fmt.Errorf("unable to parse the generated PNG with the QR code: %w", err))
		return
	}

	w := p.app.NewWindow(consts.AppName + ": link a device")
	w.SetContent(container.NewStack(canvas.NewImageFromImage(img)))
	resizeWindow(w, fyne.NewSize(500, 600))
	w.Show()
}
