package streampanel

import (
	"bytes"
	"context"
	"fmt"
	"strconv"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/davecgh/go-spew/spew"
	"github.com/facebookincubator/go-belt/tool/logger"
	gconsts "github.com/xaionaro-go/streamctl/pkg/consts"
	"github.com/xaionaro-go/streamctl/pkg/screen"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/kick"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/obs"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/twitch"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/youtube"
	streamdconfig "github.com/xaionaro-go/streamctl/pkg/streamd/config"
	"github.com/xaionaro-go/streamctl/pkg/streampanel/config"
	xfyne "github.com/xaionaro-go/xfyne/widget"
)

const (
	hardcodedLLMEndpointName = "ChatGPT"
)

func (p *Panel) openSettingsWindowNoLock(
	ctx context.Context,
	streamDCfg *streamdconfig.Config,
) error {
	{
		var buf bytes.Buffer
		_, err := streamDCfg.WriteTo(&buf)
		if err != nil {
			logger.Warnf(ctx, "unable to serialize the config: %v", err)
		} else {
			logger.Debugf(ctx, "current config: %s", buf.String())
		}
	}

	backendEnabled := map[streamcontrol.PlatformID]bool{}
	for _, backendID := range []streamcontrol.PlatformID{
		obs.ID,
		twitch.ID,
		kick.ID,
		youtube.ID,
	} {
		isEnabled, err := p.StreamD.IsBackendEnabled(ctx, backendID)
		if err != nil {
			return fmt.Errorf("unable to get info if backend '%s' is enabled: %w", backendID, err)
		}
		backendEnabled[backendID] = isEnabled
	}

	w := p.app.NewWindow(gconsts.AppName + ": Settings")
	resizeWindow(w, fyne.NewSize(400, 900))

	var obsCfg *streamcontrol.PlatformConfig[obs.AccountConfig, obs.StreamProfile]
	if backendEnabled[obs.ID] {
		obsCfg = streamcontrol.GetPlatformConfig[obs.AccountConfig, obs.StreamProfile](ctx, streamDCfg.Backends, obs.ID)
		if obsCfg != nil {
			logger.Debugf(ctx, "current OBS config: %#+v", *obsCfg)
		}
	}

	cmdBeforeStartStream, _ := streamDCfg.Backends[obs.ID].GetCustomString(
		config.CustomConfigKeyBeforeStreamStart,
	)
	cmdBeforeStopStream, _ := streamDCfg.Backends[obs.ID].GetCustomString(
		config.CustomConfigKeyBeforeStreamStop,
	)
	cmdAfterStartStream, _ := streamDCfg.Backends[obs.ID].GetCustomString(
		config.CustomConfigKeyAfterStreamStart,
	)
	cmdAfterStopStream, _ := streamDCfg.Backends[obs.ID].GetCustomString(
		config.CustomConfigKeyAfterStreamStop,
	)

	beforeStartStreamCommandEntry := widget.NewEntry()
	beforeStartStreamCommandEntry.SetText(cmdBeforeStartStream)
	beforeStopStreamCommandEntry := widget.NewEntry()
	beforeStopStreamCommandEntry.SetText(cmdBeforeStopStream)
	afterStartStreamCommandEntry := widget.NewEntry()
	afterStartStreamCommandEntry.SetText(cmdAfterStartStream)
	afterStopStreamCommandEntry := widget.NewEntry()
	afterStopStreamCommandEntry.SetText(cmdAfterStopStream)

	afterReceivedChatMessage := widget.NewEntry()
	afterReceivedChatMessage.SetText(p.Config.Chat.CommandOnReceiveMessage)

	enableChatNotifications := widget.NewCheck(
		"Enable on-screen notifications for chat messages",
		func(b bool) {},
	)
	enableChatNotifications.SetChecked(p.Config.Chat.NotificationsEnabled())
	enableChatMessageSoundsAlerts := widget.NewCheck(
		"Enable sound alerts for chat messages",
		func(b bool) {},
	)
	enableChatMessageSoundsAlerts.SetChecked(p.Config.Chat.ReceiveMessageSoundAlarmEnabled())

	oldScreenshotConfig := p.Config.Screenshot

	mpvPathEntry := widget.NewEntry()
	mpvPathEntry.SetText(streamDCfg.StreamServer.VideoPlayer.MPV.Path)
	mpvPathEntry.OnChanged = func(s string) {
		streamDCfg.StreamServer.VideoPlayer.MPV.Path = s
	}

	cancelButton := widget.NewButtonWithIcon("Cancel", theme.CancelIcon(), func() {
		w.Close()
	})
	saveButton := widget.NewButtonWithIcon("Save", theme.DocumentSaveIcon(), func() {
		p.Config.Chat.CommandOnReceiveMessage = afterReceivedChatMessage.Text
		p.Config.Chat.EnableNotifications = ptr(enableChatNotifications.Checked)
		p.Config.Chat.EnableReceiveMessageSoundAlarm = ptr(enableChatMessageSoundsAlerts.Checked)

		if err := p.SaveConfig(ctx); err != nil {
			p.DisplayError(fmt.Errorf("unable to save the local config: %w", err))
		} else {
			if p.Config.Screenshot != oldScreenshotConfig {
				p.reinitScreenshoter(ctx)
			}
		}

		if obsCfg != nil {
			obsCfg.SetCustomString(
				config.CustomConfigKeyBeforeStreamStart, beforeStartStreamCommandEntry.Text)
			obsCfg.SetCustomString(
				config.CustomConfigKeyBeforeStreamStop, beforeStopStreamCommandEntry.Text)
			obsCfg.SetCustomString(
				config.CustomConfigKeyAfterStreamStart, afterStartStreamCommandEntry.Text)
			obsCfg.SetCustomString(
				config.CustomConfigKeyAfterStreamStop, afterStopStreamCommandEntry.Text)
			streamDCfg.Backends[obs.ID] = streamcontrol.ToAbstractPlatformConfig(ctx, obsCfg)
		}

		if err := p.SetStreamDConfig(ctx, streamDCfg); err != nil {
			p.DisplayError(fmt.Errorf("unable to update the remote config: %w", err))
		} else {
			if err := p.StreamD.SaveConfig(ctx); err != nil {
				p.DisplayError(fmt.Errorf("unable to save the remote config: %w", err))
			}
		}

		w.Close()
	})

	templateInstruction := widget.NewRichTextFromMarkdown(
		"Commands support [Go templates](https://pkg.go.dev/text/template) with two custom functions predefined:\n* `devnull` nullifies any inputs\n* `httpGET` makes an HTTP GET request and inserts the response body",
	)
	templateInstruction.Wrapping = fyne.TextWrapWord

	manageAccountsButton := widget.NewButtonWithIcon("Manage accounts", theme.SettingsIcon(), func() {
		p.OpenAccountManagementWindow(ctx)
		w.Close() // Close settings when opening account management to avoid confusion? Or keep both open?
	})

	screenshotBoundsXEntry := widget.NewEntry()
	screenshotBoundsXEntry.SetPlaceHolder("x")
	screenshotBoundsXEntry.SetText(fmt.Sprintf("%d", p.Config.Screenshot.Bounds.Min.Y))
	screenshotBoundsXEntry.OnSubmitted = func(s string) {
		v, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			p.DisplayError(fmt.Errorf("unable to parse screenshot bound 'x': %w", err))
			return
		}
		dx := p.Config.Screenshot.Bounds.Dx()
		p.Config.Screenshot.Bounds.Min.X = int(v)
		p.Config.Screenshot.Bounds.Max.X = int(v) + dx
	}
	screenshotBoundsYEntry := widget.NewEntry()
	screenshotBoundsYEntry.SetPlaceHolder("y")
	screenshotBoundsYEntry.SetText(fmt.Sprintf("%d", p.Config.Screenshot.Bounds.Min.Y))
	screenshotBoundsYEntry.OnSubmitted = func(s string) {
		v, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			p.DisplayError(fmt.Errorf("unable to parse screenshot bound 'y': %w", err))
			return
		}
		dy := p.Config.Screenshot.Bounds.Dy()
		p.Config.Screenshot.Bounds.Min.Y = int(v)
		p.Config.Screenshot.Bounds.Max.Y = int(v) + dy
	}
	screenshotBoundsWEntry := widget.NewEntry()
	screenshotBoundsWEntry.SetPlaceHolder("w")
	screenshotBoundsWEntry.SetText(fmt.Sprintf("%d", p.Config.Screenshot.Bounds.Dx()))
	screenshotBoundsWEntry.OnSubmitted = func(s string) {
		v, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			p.DisplayError(fmt.Errorf("unable to parse screenshot bound 'w': %w", err))
			return
		}
		p.Config.Screenshot.Bounds.Max.X = p.Config.Screenshot.Bounds.Min.X + int(v)
	}
	screenshotBoundsHEntry := widget.NewEntry()
	screenshotBoundsHEntry.SetPlaceHolder("h")
	screenshotBoundsHEntry.SetText(fmt.Sprintf("%d", p.Config.Screenshot.Bounds.Dy()))
	screenshotBoundsHEntry.OnSubmitted = func(s string) {
		v, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			p.DisplayError(fmt.Errorf("unable to parse screenshot bound 'h': %w", err))
			return
		}
		p.Config.Screenshot.Bounds.Max.Y = p.Config.Screenshot.Bounds.Min.Y + int(v)
	}

	numDisplays := screen.GetNumber()
	var displays []string
	caption2id := map[string]int{}
	for i := 0; i < int(numDisplays); i++ {
		caption := fmt.Sprintf("display #%d", i+1)
		displays = append(displays, caption)
		caption2id[caption] = i
	}
	displayIDSelector := widget.NewSelect(displays, func(s string) {
		id := caption2id[s]

		screenBounds := screen.GetBounds(id)
		screenshotBoundsXEntry.SetText(fmt.Sprintf("%d", screenBounds.Min.X))
		screenshotBoundsXEntry.OnSubmitted(fmt.Sprintf("%d", screenBounds.Min.X))
		screenshotBoundsYEntry.SetText(fmt.Sprintf("%d", screenBounds.Min.Y))
		screenshotBoundsYEntry.OnSubmitted(fmt.Sprintf("%d", screenBounds.Min.Y))
		screenshotBoundsWEntry.SetText(fmt.Sprintf("%d", screenBounds.Dx()))
		screenshotBoundsWEntry.OnSubmitted(fmt.Sprintf("%d", screenBounds.Dx()))
		screenshotBoundsHEntry.SetText(fmt.Sprintf("%d", screenBounds.Dy()))
		screenshotBoundsHEntry.OnSubmitted(fmt.Sprintf("%d", screenBounds.Dy()))
	})

	enableDisableScreenshoter := func(b bool) {
		if b {
			screenshotBoundsXEntry.Enable()
			screenshotBoundsYEntry.Enable()
			screenshotBoundsWEntry.Enable()
			screenshotBoundsHEntry.Enable()
			displayIDSelector.Enable()
		} else {
			screenshotBoundsXEntry.Disable()
			screenshotBoundsYEntry.Disable()
			screenshotBoundsWEntry.Disable()
			screenshotBoundsHEntry.Disable()
			displayIDSelector.Disable()
		}
	}

	enableScreenshotSendingCheckbox := widget.NewCheck(
		"Send screenshots from this computer",
		func(b bool) {
			p.Config.Screenshot.Enabled = ptr(b)
			enableDisableScreenshoter(b)
		},
	)
	if p.Config.Screenshot.Enabled == nil {
		p.Config.Screenshot.Enabled = ptr(false)
	}
	enableScreenshotSendingCheckbox.SetChecked(*p.Config.Screenshot.Enabled)
	enableDisableScreenshoter(*p.Config.Screenshot.Enabled)

	obsSettings := container.NewVBox()
	if backendEnabled[obs.ID] {
		resp, err := p.getOBSSceneList(ctx)
		if err != nil {
			p.DisplayError(err)
		} else {
			var options []string
			options = append(options, "")
			for _, scene := range resp.Scenes {
				options = append(options, scene.GetSceneName())
			}
			accountCfg := obsCfg.Accounts[""]
			sceneAfterStreamingSelector := widget.NewSelect(options, func(s string) {
				accountCfg := obsCfg.Accounts[""]
				accountCfg.SceneAfterStream.Name = s
				obsCfg.Accounts[""] = accountCfg
			})
			sceneAfterStreamingSelector.SetSelected(accountCfg.SceneAfterStream.Name)
			sceneAfterStreamingDuration := xfyne.NewNumericalEntry()
			sceneAfterStreamingDuration.SetText(fmt.Sprintf("%f", accountCfg.SceneAfterStream.Duration.Seconds()))
			sceneAfterStreamingDuration.OnChanged = func(s string) {
				if s == "" || s == "-" {
					s = "0"
				}
				v, err := strconv.ParseFloat(s, 64)
				if err != nil {
					p.DisplayError(fmt.Errorf("unable to parse '%s' as a float: %w", s, err))
					return
				}
				accountCfg := obsCfg.Accounts[""]
				accountCfg.SceneAfterStream.Duration = time.Duration(float64(time.Second) * v)
				obsCfg.Accounts[""] = accountCfg
			}

			obsExecCommand := widget.NewEntry()
			obsExecCommand.SetPlaceHolder("command to exec OBS")
			obsExecCommand.SetText(accountCfg.RestartOnUnavailable.ExecCommand)
			if !accountCfg.RestartOnUnavailable.Enable {
				obsExecCommand.Hide()
			}
			obsExecCommand.OnChanged = func(s string) {
				accountCfg := obsCfg.Accounts[""]
				accountCfg.RestartOnUnavailable.ExecCommand = s
				obsCfg.Accounts[""] = accountCfg
			}

			autoRestartEnable := widget.NewCheck("Auto-restart (if OBS is hanging or not started)", func(b bool) {
				accountCfg := obsCfg.Accounts[""]
				accountCfg.RestartOnUnavailable.Enable = b
				obsCfg.Accounts[""] = accountCfg
				if accountCfg.RestartOnUnavailable.Enable {
					obsExecCommand.Show()
				} else {
					obsExecCommand.Hide()
				}
			})
			autoRestartEnable.SetChecked(accountCfg.RestartOnUnavailable.Enable)

			obsSettings.Add(container.NewVBox(
				widget.NewRichTextFromMarkdown(`# OBS`),
				widget.NewLabel("Switch to scene after streaming:"),
				sceneAfterStreamingSelector,
				widget.NewLabel("Hold the scene for (seconds):"),
				sceneAfterStreamingDuration,
				autoRestartEnable,
				obsExecCommand,
			))
		}
	}

	chatGPTAPIKeyEntry := widget.NewEntry()
	chatGPTAPIKeyEntry.OnSubmitted = func(s string) {
		if streamDCfg.LLM.Endpoints == nil {
			streamDCfg.LLM.Endpoints = make(streamdconfig.LLMEndpoints)
		}
		b := streamDCfg.LLM.Endpoints[hardcodedLLMEndpointName]
		if b == nil {
			b = &streamdconfig.LLMEndpoint{
				Provider:  streamdconfig.LLMProviderChatGPT,
				ModelName: "gpt-4o",
			}
			streamDCfg.LLM.Endpoints[hardcodedLLMEndpointName] = b
		}
		b.APIKey = s
		logger.Debugf(ctx, "LLM config: %#+v", spew.Sdump(streamDCfg.LLM))
	}
	chatGPTAPIKeyEntry.SetText(streamDCfg.LLM.Endpoints.GetAPIKey(hardcodedLLMEndpointName))

	w.SetContent(container.NewBorder(
		container.NewVBox(
			container.NewHBox(
				container.NewVBox(
					container.NewVBox(
						widget.NewRichTextFromMarkdown(`# Streaming platforms`),
						manageAccountsButton,
					),
					container.NewVBox(
						widget.NewRichTextFromMarkdown(`# Chat`),
						enableChatNotifications,
						enableChatMessageSoundsAlerts,
					),
					container.NewVBox(
						widget.NewRichTextFromMarkdown(`# Dashboard`),
						enableScreenshotSendingCheckbox,
						widget.NewLabel("The screen/display to screenshot:"),
						displayIDSelector,
						widget.NewLabel("Crop to:"),
						container.NewHBox(
							screenshotBoundsXEntry,
							screenshotBoundsYEntry,
							screenshotBoundsWEntry,
							screenshotBoundsHEntry,
						),
					),
					obsSettings,
				),
				container.NewVBox(
					container.NewVBox(
						widget.NewRichTextFromMarkdown(`# Video players`),
						widget.NewLabel("Path to 'mpv':"),
						mpvPathEntry,
					),
					container.NewVBox(
						widget.NewRichTextFromMarkdown(`# Commands`),
						templateInstruction,
						widget.NewLabel("Run command on stream start (before):"),
						beforeStartStreamCommandEntry,
						widget.NewLabel("Run command on stream start (after):"),
						afterStartStreamCommandEntry,
						widget.NewLabel("Run command on stream stop (before):"),
						beforeStopStreamCommandEntry,
						widget.NewLabel("Run command on stream stop (after):"),
						afterStopStreamCommandEntry,
						widget.NewLabel("Run command on receiving a chat message (after):"),
						afterReceivedChatMessage,
					),
					container.NewVBox(
						widget.NewRichTextFromMarkdown(`# LLM`),
						widget.NewLabel("ChatGPT API Key:"),
						chatGPTAPIKeyEntry,
					),
				),
			),
		),
		container.NewHBox(
			cancelButton,
			saveButton,
		),
		nil,
		nil,
	))

	w.Show()

	return nil
}
