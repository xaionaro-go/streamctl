package streampanel

import (
	"context"
	"fmt"
	"strconv"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/xaionaro-go/obs-grpc-proxy/protobuf/go/obs_grpc"
	gconsts "github.com/xaionaro-go/streamctl/pkg/consts"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/obs"
)

func (p *Panel) obsSetScene(
	ctx context.Context,
	sceneName string,
) error {
	obsServer, obsServerClose, err := p.StreamD.OBS(ctx)
	if obsServerClose != nil {
		defer obsServerClose()
	}
	if err != nil {
		return fmt.Errorf("unable to initialize a client to OBS: %w", err)
	}
	_, err = obsServer.SetCurrentProgramScene(ctx, &obs_grpc.SetCurrentProgramSceneRequest{
		SceneName: &sceneName,
	})
	if err != nil {
		return fmt.Errorf("unable to set the OBS scene: %w", err)
	}
	return nil
}

func (p *Panel) InputOBSConnectInfo(
	ctx context.Context,
	cfg *streamcontrol.PlatformConfig[obs.PlatformSpecificConfig, obs.StreamProfile],
) BackendStatusCode {
	w := p.app.NewWindow(gconsts.AppName + ": Input OBS connection info")
	resizeWindow(w, fyne.NewSize(600, 200))

	hostField := widget.NewEntry()
	hostField.SetPlaceHolder("OBS hostname, e.g. 192.168.0.134")
	portField := widget.NewEntry()
	portField.OnChanged = func(s string) {
		filtered := removeNonDigits(s)
		if s != filtered {
			portField.SetText(filtered)
		}
	}
	portField.SetPlaceHolder("OBS port, usually it is 4455")
	passField := widget.NewEntry()
	passField.SetPlaceHolder("OBS password")
	instructionText := widget.NewRichText(
		&widget.ListSegment{Items: []widget.RichTextSegment{
			&widget.TextSegment{Text: `Open OBS`},
			&widget.TextSegment{Text: `Click "Tools" on the top menu`},
			&widget.TextSegment{Text: `Select "WebSocket Server Settings"`},
			&widget.TextSegment{Text: `Check the "Enable WebSocket server" checkbox`},
			&widget.TextSegment{Text: `In the window click "Show Connect Info"`},
			&widget.TextSegment{Text: `Copy the data from the connect info to the fields above`},
		}},
	)
	instructionText.Wrapping = fyne.TextWrapWord

	waitCh := make(chan struct{})
	disable := false
	disableButton := widget.NewButtonWithIcon("Disable", theme.ConfirmIcon(), func() {
		disable = true
		close(waitCh)
	})

	notNow := false
	notNowButton := widget.NewButtonWithIcon("Not now", theme.ConfirmIcon(), func() {
		notNow = true
		close(waitCh)
	})

	var port uint64
	okButton := widget.NewButtonWithIcon("OK", theme.ConfirmIcon(), func() {
		var err error
		port, err = strconv.ParseUint(portField.Text, 10, 16)
		if err != nil {
			p.DisplayError(fmt.Errorf("unable to parse port '%s': %w", portField.Text, err))
			return
		}

		close(waitCh)
	})

	w.SetContent(container.NewBorder(
		widget.NewRichTextWithText("Enter OBS user info:"),
		container.NewHBox(disableButton, notNowButton, okButton),
		nil,
		nil,
		container.NewVBox(
			hostField,
			portField,
			passField,
			instructionText,
		),
	))
	w.Show()
	<-waitCh
	w.Hide()

	if disable {
		return BackendStatusCodeDisable
	}
	if notNow {
		return BackendStatusCodeNotNow
	}

	cfg.Config.Host = hostField.Text
	cfg.Config.Port = uint16(port)
	cfg.Config.Password.Set(passField.Text)

	return BackendStatusCodeReady
}

func (p *Panel) getOBSSceneList(
	ctx context.Context,
) (*obs_grpc.GetSceneListResponse, error) {
	obsServer, obsServerClose, err := p.StreamD.OBS(ctx)
	if obsServerClose != nil {
		defer obsServerClose()
	}
	if err != nil {
		return nil, fmt.Errorf("unable to initialize a client to OBS: %w", err)
	}

	sceneListResp, err := obsServer.GetSceneList(ctx, &obs_grpc.GetSceneListRequest{})
	if err != nil {
		return nil, fmt.Errorf("unable to request the list of scenes: %w", err)
	}

	return sceneListResp, nil
}
