package streampanel

import (
	"github.com/xaionaro-go/streamctl/pkg/clock"
)

import (
	"context"
	"fmt"
	"strconv"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/goccy/go-yaml"
	"github.com/xaionaro-go/obs-grpc-proxy/protobuf/go/obs_grpc"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/obs"
)

type obsProfileUI struct{}

func init() {
	registerPlatformUI(obs.ID, &obsProfileUI{})
}

func (ui *obsProfileUI) GetUserInfoItems(
	ctx context.Context,
	p *Panel,
	platID streamcontrol.PlatformID,
	accountID streamcontrol.AccountID,
	accountRaw []byte,
) ([]fyne.CanvasObject, func() ([]byte, error), error) {
	var cfg obs.AccountConfig
	if len(accountRaw) > 0 {
		if err := yaml.Unmarshal(accountRaw, &cfg); err != nil {
			return nil, nil, fmt.Errorf("unable to unmarshal account config: %w", err)
		}
	}

	hostField := widget.NewEntry()
	hostField.SetPlaceHolder("OBS hostname, e.g. 192.168.0.134")
	hostField.SetText(cfg.Host)
	portField := widget.NewEntry()
	portField.OnChanged = func(s string) {
		filtered := removeNonDigits(s)
		if s != filtered {
			portField.SetText(filtered)
		}
	}
	portField.SetPlaceHolder("OBS port, usually it is 4455")
	if cfg.Port != 0 {
		portField.SetText(fmt.Sprintf("%d", cfg.Port))
	} else {
		portField.SetText("4455")
	}
	passField := widget.NewEntry()
	passField.SetPlaceHolder("OBS password")
	passField.SetText(cfg.Password.Get())
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

	items := []fyne.CanvasObject{
		widget.NewLabel("OBS host:"),
		hostField,
		widget.NewLabel("OBS port:"),
		portField,
		widget.NewLabel("OBS password:"),
		passField,
		instructionText,
	}

	saveFunc := func() ([]byte, error) {
		port, err := strconv.ParseUint(portField.Text, 10, 16)
		if err != nil {
			return nil, fmt.Errorf("unable to parse port '%s': %w", portField.Text, err)
		}
		cfg.Host = hostField.Text
		cfg.Port = uint16(port)
		cfg.Password.Set(passField.Text)
		return yaml.Marshal(cfg)
	}

	return items, saveFunc, nil
}

func (ui *obsProfileUI) Placement() platformProfilePlacement {
	return platformProfilePlacementLeft
}

func (ui *obsProfileUI) IsReadyToStart(ctx context.Context, p *Panel) bool {
	return true
}

func (ui *obsProfileUI) AfterStartStream(ctx context.Context, p *Panel) error {
	return nil
}

func (ui *obsProfileUI) IsAlwaysChecked(ctx context.Context, p *Panel) bool {
	return true
}

func (ui *obsProfileUI) ShouldStopParallel() bool {
	return false
}

func (ui *obsProfileUI) UpdateStatus(ctx context.Context, p *Panel) {
	observability.Call(ctx, func(ctx context.Context) {
		obsServer, obsServerClose, err := p.StreamD.OBS(ctx, "")
		if obsServerClose != nil {
			defer obsServerClose()
		}
		if err != nil {
			p.ReportError(fmt.Errorf("unable to initialize a client to OBS: %w", err))
			return
		}

		sceneListResp, err := obsServer.GetSceneList(ctx, &obs_grpc.GetSceneListRequest{})
		if err != nil {
			p.ReportError(err)
			return
		}

		var options []string
		for _, scene := range sceneListResp.Scenes {
			options = append(options, *scene.SceneName)
		}
		p.obsSelectScene.Options = options

		if sceneListResp.CurrentProgramSceneName != p.obsSelectScene.Selected {
			logger.Debugf(ctx, "the scene was changed from '%s' to '%s'", p.obsSelectScene.Selected, sceneListResp.CurrentProgramSceneName)
			p.obsSelectScene.Selected = sceneListResp.CurrentProgramSceneName
			p.obsSelectScene.Refresh()
		}
	})
}

func (ui *obsProfileUI) GetColor() fyne.ThemeColorName {
	return ""
}

func (ui *obsProfileUI) AfterStopStream(ctx context.Context, p *Panel) error {
	streamDCfg, _ := p.GetStreamDConfig(ctx)

	if streamDCfg != nil {
		obsCfg := streamcontrol.GetPlatformConfig[obs.AccountConfig, obs.StreamProfile](ctx, streamDCfg.Backends, obs.ID)
		accountCfg := obsCfg.Accounts[""]
		if accountCfg.SceneAfterStream.Name != "" {
			p.backgroundRenderer.Q <- func() { p.startStopButton.SetText("Switching the scene") }
			err := p.obsSetScene(ctx, accountCfg.SceneAfterStream.Name)
			if err != nil {
				p.DisplayError(fmt.Errorf("unable to change the OBS scene: %w", err))
			}
		}
		if accountCfg.SceneAfterStream.Duration > 0 {
			p.backgroundRenderer.Q <- func() {
				p.startStopButton.SetText(fmt.Sprintf("Holding the scene: %s", accountCfg.SceneAfterStream.Duration))
			}
			clock.Get().Sleep(accountCfg.SceneAfterStream.Duration)
		}
	}

	p.backgroundRenderer.Q <- func() { p.startStopButton.SetText("Stopping OBS...") }
	return p.StreamD.SetStreamActive(ctx, streamcontrol.NewStreamIDFullyQualified(obs.ID, streamcontrol.DefaultAccountID, streamcontrol.DefaultStreamID), false)
}

func (ui *obsProfileUI) RenderStream(
	ctx context.Context,
	p *Panel,
	w fyne.Window,
	platID streamcontrol.PlatformID,
	sID streamcontrol.StreamIDFullyQualified,
	backendData any,
	streamConfig any,
) (fyne.CanvasObject, func() (any, error)) {
	var obsProfile obs.StreamProfile
	if streamConfig != nil {
		_ = yaml.Unmarshal(streamcontrol.ToRawMessage(streamConfig), &obsProfile)
	}

	enableRecordingCheck := widget.NewCheck("Enable recording", func(b bool) {
		obsProfile.EnableRecording = b
	})
	enableRecordingCheck.SetChecked(obsProfile.EnableRecording)

	return enableRecordingCheck, func() (any, error) {
		return obsProfile, nil
	}
}

func (ui *obsProfileUI) FilterMatch(
	platProfile streamcontrol.RawMessage,
	filterValue string,
) bool {
	return false
}
