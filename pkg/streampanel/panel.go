package streampanel

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync/atomic"
	"time"
	"unicode"

	"fyne.io/fyne/v2"
	fyneapp "fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	child_process_manager "github.com/AgustinSRG/go-child-process-manager"
	"github.com/facebookincubator/go-belt/tool/experimental/errmon"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/hashicorp/go-multierror"
	"github.com/xaionaro-go/obs-grpc-proxy/protobuf/go/obs_grpc"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/streamctl/pkg/autoupdater"
	"github.com/xaionaro-go/streamctl/pkg/buildvars"
	"github.com/xaionaro-go/streamctl/pkg/command"
	gconsts "github.com/xaionaro-go/streamctl/pkg/consts"
	"github.com/xaionaro-go/streamctl/pkg/oauthhandler"
	"github.com/xaionaro-go/streamctl/pkg/screenshot"
	"github.com/xaionaro-go/streamctl/pkg/screenshoter"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/kick"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/obs"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/twitch"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/youtube"
	"github.com/xaionaro-go/streamctl/pkg/streamd"
	"github.com/xaionaro-go/streamctl/pkg/streamd/api"
	"github.com/xaionaro-go/streamctl/pkg/streamd/client"
	streamdconfig "github.com/xaionaro-go/streamctl/pkg/streamd/config"
	"github.com/xaionaro-go/streamctl/pkg/streamd/grpc/go/streamd_grpc"
	"github.com/xaionaro-go/streamctl/pkg/streampanel/audio"
	"github.com/xaionaro-go/streamctl/pkg/streampanel/config"
	"github.com/xaionaro-go/streamctl/pkg/streampanel/consts"
	"github.com/xaionaro-go/xpath"
	"github.com/xaionaro-go/xsync"
)

const youtubeTitleLength = 90

type Panel struct {
	StreamD      api.StreamD
	Screenshoter Screenshoter
	Audio        *audio.Audio

	OnInternallySubmittedOAuthCode func(
		ctx context.Context,
		platID streamcontrol.PlatformName,
		code string,
	) error

	screenshoterClose  context.CancelFunc
	screenshoterLocker xsync.Mutex

	app                      fyne.App
	Config                   Config
	configLocker             xsync.RWMutex
	streamMutex              xsync.Mutex
	updateStreamClockHandler *updateTimerHandler
	profilesOrder            []streamcontrol.ProfileName
	profilesOrderFiltered    []streamcontrol.ProfileName
	selectedProfileName      *streamcontrol.ProfileName
	defaultContext           context.Context

	mainWindow             fyne.Window
	dashboardWindow        *dashboardWindow
	setupStreamButton      *widget.Button
	startStopButton        *widget.Button
	profilesListWidget     *widget.List
	streamTitleField       *widget.Entry
	streamDescriptionField *widget.Entry

	dashboardLocker         xsync.Mutex
	dashboardShowHideButton *widget.Button

	appStatus     *widget.Label
	appStatusData struct {
		prevUpdateTS time.Time
		prevBytesIn  uint64
		prevBytesOut uint64
	}
	streamStatus       map[streamcontrol.PlatformName]*streamStatus
	streamStatusLocker xsync.Mutex

	filterValue string

	youtubeCheck *widget.Check
	twitchCheck  *widget.Check
	kickCheck    *widget.Check

	configPath        string
	configCacheLocker xsync.Mutex
	configCache       *streamdconfig.Config

	setStatusFunc func(string)

	displayErrorLocker xsync.Mutex
	displayErrorWindow fyne.Window

	waitStreamDConnectWindowLocker xsync.Mutex
	//waitStreamDConnectWindow        fyne.Window
	waitStreamDConnectWindowCounter int32
	waitStreamDCallWindowLocker     xsync.Mutex
	//waitStreamDCallWindow           fyne.Window
	waitStreamDCallWindowCounter int32

	imageLocker         xsync.Mutex
	imageLastDownloaded map[consts.ImageID][]byte

	lastDisplayedError error

	streamServersWidget *fyne.Container
	streamsWidget       *fyne.Container
	destinationsWidget  *fyne.Container
	restreamsWidget     *fyne.Container
	playersWidget       *fyne.Container

	previousNumBytesLocker xsync.Mutex
	previousNumBytes       map[any][4]uint64
	previousNumBytesTS     map[any]time.Time

	streamServersLocker              xsync.Mutex
	streamServersUpdaterCanceller    context.CancelFunc
	streamForwardersLocker           xsync.Mutex
	streamForwardersUpdaterCanceller context.CancelFunc
	streamPlayersLocker              xsync.Mutex
	streamPlayersUpdaterCanceller    context.CancelFunc

	obsSelectScene *widget.Select

	errorReportsLocker xsync.Mutex
	errorReports       map[string]errorReport

	statusPanelLocker xsync.Mutex
	statusPanel       *widget.Label

	eventSensor *eventSensor

	windowsLocker    xsync.Mutex
	windowsCounter   atomic.Uint64
	permanentWindows map[uint64]windowDriver

	streamStartedLocker xsync.Mutex
	streamStartedWindow fyne.Window
}

func New(
	configPath string,
	opts ...Option,
) (*Panel, error) {
	configPath, err := getExpandedConfigPath(configPath)
	if err != nil {
		return nil, fmt.Errorf("unable to get the path to the config file: %w", err)
	}

	var cfg Config
	err = config.ReadConfigFromPath(configPath, &cfg)
	if err != nil {
		return nil, fmt.Errorf("unable to read the config from path '%s': %w", configPath, err)
	}

	ctx := context.TODO()
	audio := audio.NewAudio(ctx)
	logger.Infof(ctx, "audio backend is %T", audio.Playbacker.PlayerPCM)

	p := &Panel{
		Audio:               audio,
		configPath:          configPath,
		Config:              Options(opts).ApplyOverrides(cfg),
		Screenshoter:        screenshoter.New(screenshot.Implementation{}),
		imageLastDownloaded: map[consts.ImageID][]byte{},
		streamStatus:        map[streamcontrol.PlatformName]*streamStatus{},
		previousNumBytes:    map[any][4]uint64{},
		previousNumBytesTS:  map[any]time.Time{},
		errorReports:        map[string]errorReport{},
		streamMutex: xsync.Mutex{
			PanicOnDeadlock: ptr(false),
		},
		permanentWindows: map[uint64]windowDriver{},
	}
	return p, nil
}

func (p *Panel) SetStatus(msg string) {
	p.statusPanelSet(msg)
	if p.setStatusFunc == nil {
		return
	}
	p.setStatusFunc(msg)
}

func (p *Panel) dumpConfig(ctx context.Context) {
	if logger.FromCtx(ctx).Level() < logger.LevelTrace {
		return
	}

	var buf bytes.Buffer
	_, err := p.Config.WriteTo(&buf)
	if err != nil {
		logger.Error(ctx, err)
		return
	}

	logger.Tracef(ctx, "the current config is: %s", buf.String())
}

func (p *Panel) Loop(ctx context.Context, opts ...LoopOption) error {
	if p.defaultContext != nil {
		return fmt.Errorf("Loop was already used, and cannot be used the second time")
	}
	p.dumpConfig(ctx)

	initCfg := loopOptions(opts).Config()

	p.defaultContext = ctx

	if err := p.LazyInitStreamD(ctx); err != nil {
		return fmt.Errorf("unable to initialize stream controller: %w", err)
	}

	p.app = fyneapp.New()
	p.app.Driver().SetDisableScreenBlanking(true)
	logger.Tracef(ctx, "SetDisableScreenBlanking(true)")

	var loadingWindow fyne.Window
	if p.Config.RemoteStreamDAddr == "" {
		logger.Tracef(ctx, "is not a remote streamd")
		loadingWindow = p.newLoadingWindow(ctx)
		resizeWindow(loadingWindow, fyne.NewSize(600, 600))
	} else {
		logger.Tracef(ctx, "is a remote streamd")
		loadingWindow = p.newConnectingWindow(ctx)
		resizeWindow(loadingWindow, fyne.NewSize(600, 600))
	}

	loadingWindowText := widget.NewRichTextFromMarkdown("")
	loadingWindowText.Wrapping = fyne.TextWrapWord
	loadingWindow.SetContent(loadingWindowText)
	p.setStatusFunc = func(msg string) {
		loadingWindowText.ParseMarkdown(fmt.Sprintf("# %s", msg))
	}

	closeLoadingWindow := func() {
		logger.Tracef(ctx, "closing the loading window")
		loadingWindow.Hide()
		observability.Go(ctx, func() {
			time.Sleep(10 * time.Millisecond)
			loadingWindow.Hide()
			time.Sleep(100 * time.Millisecond)
			loadingWindow.Hide()
			time.Sleep(time.Second)
			loadingWindow.Close()
		})
	}

	observability.Go(ctx, func() {
		if streamD, ok := p.StreamD.(*client.Client); ok {
			p.setStatusFunc("Connecting...")
			err := p.startOAuthListenerForRemoteStreamD(ctx, streamD)
			if err != nil {
				p.setStatusFunc(
					fmt.Sprintf(
						"Connection failed, please restart the application.\n\nError: %v",
						err,
					),
				)
				<-ctx.Done()
			}
			closeLoadingWindow()
			p.setStatusFunc = nil
		} else {
			defer loadingWindow.Close()
			// TODO: delete this hardcoding of the port
			defer closeLoadingWindow()
			streamD := p.StreamD.(*streamd.StreamD)
			streamD.AddOAuthListenPort(p.Config.OAuth.ListenPorts.Twitch)
			observability.Go(ctx, func() {
				<-ctx.Done()
				streamD.RemoveOAuthListenPort(p.Config.OAuth.ListenPorts.Twitch)
			})
			logger.Tracef(ctx, "started oauth listener for the local streamd")
		}

		streamDRunErr := p.StreamD.Run(ctx)
		logger.Tracef(ctx, "streamd.Run(): %v", streamDRunErr)
		p.setStatusFunc = nil
		if streamD, ok := p.StreamD.(*streamd.StreamD); ok {
			assert(streamD.StreamServer != nil)
		}

		p.reinitScreenshoter(ctx)
		p.initEventSensor(ctx)

		err := p.initStreamDConfig(ctx)
		if err != nil {
			p.DisplayError(fmt.Errorf("unable to initialize the streamd config: %w", err))
		}

		p.initMainWindow(ctx, initCfg.StartingPage)
		if streamDRunErr != nil {
			p.DisplayError(
				fmt.Errorf("unable to initialize the streaming controllers: %w", streamDRunErr),
			)
		}

		logger.Tracef(ctx, "p.rearrangeProfiles")
		if err := p.rearrangeProfiles(ctx); err != nil {
			err = fmt.Errorf("unable to arrange the profiles: %w", err)
			p.DisplayError(err)
		}

		logger.Tracef(ctx, "ended stream controllers initialization")

		if initCfg.AutoUpdater != nil {
			observability.Go(ctx, func() {
				p.checkForUpdates(ctx, initCfg.AutoUpdater)
			})
		}

		p.initFyneHacks(ctx)
	})

	p.app.Run()
	return nil
}

func (p *Panel) initStreamDConfig(
	ctx context.Context,
) (_err error) {
	logger.Debugf(ctx, "initStreamDConfig")
	defer func() { logger.Debugf(ctx, "/initStreamDConfig: %v", _err) }()

	err := p.localConfigCacheUpdater(ctx)
	if err != nil {
		return fmt.Errorf("unable to initialize the config cache updater: %w", err)
	}

	cfg, err := p.GetStreamDConfig(ctx)
	if err != nil {
		return fmt.Errorf("unable to get the config: %w", err)
	}
	if cfg.Backends == nil {
		cfg.Backends = make(streamcontrol.Config)
	}

	configHasChanged := false

	// TODO: move the 'git' configuration here as well.

	for _, platName := range []streamcontrol.PlatformName{
		twitch.ID,
		kick.ID,
		obs.ID,
		youtube.ID,
	} {
		if streamcontrol.IsInitialized(cfg.Backends, platName) {
			continue
		}
		platCfg := cfg.Backends[platName]
		if platCfg != nil && platCfg.Enable != nil && !*platCfg.Enable {
			logger.Debugf(ctx, "platform '%s' is explicitly disabled", platName)
			continue
		}
		logger.Debugf(ctx, "'%s' is not initialized: %#+v, fixing", platName, platCfg)
		configHasChanged = true

		err := p.inputUserInfo(ctx, cfg, platName)
		if err != nil {
			p.DisplayError(fmt.Errorf("unable to input config for '%s': %w", platName, err))
			continue
		}
	}

	if configHasChanged {
		err := p.SetStreamDConfig(ctx, cfg)
		if err != nil {
			return fmt.Errorf("unable to set the new config: %w", err)
		}

		err = p.StreamD.SaveConfig(ctx)
		if err != nil {
			return fmt.Errorf("unable to save the new config: %w", err)
		}

		err = p.StreamD.EXPERIMENTAL_ReinitStreamControllers(ctx)
		if err != nil {
			return fmt.Errorf("unable to reinit the stream controllers: %w", err)
		}
	}

	return nil
}

func (p *Panel) inputUserInfo(
	ctx context.Context,
	cfg *streamdconfig.Config,
	platName streamcontrol.PlatformName,
) error {
	if cfg.Backends[platName] == nil {
		switch platName {
		case youtube.ID:
			youtube.InitConfig(cfg.Backends)
		case twitch.ID:
			twitch.InitConfig(cfg.Backends)
		case kick.ID:
			kick.InitConfig(cfg.Backends)
		case obs.ID:
			obs.InitConfig(cfg.Backends)
		}
	}
	platCfg := cfg.Backends[platName]

	var status BackendStatusCode
	switch platName {
	case youtube.ID:
		platCfg := streamcontrol.ConvertPlatformConfig[youtube.PlatformSpecificConfig, youtube.StreamProfile](
			ctx,
			platCfg,
		)
		status = p.InputYouTubeUserInfo(ctx, platCfg)
		cfg.Backends[platName] = streamcontrol.ToAbstractPlatformConfig(ctx, platCfg)
	case twitch.ID:
		platCfg := streamcontrol.ConvertPlatformConfig[twitch.PlatformSpecificConfig, twitch.StreamProfile](
			ctx,
			platCfg,
		)
		status = p.InputTwitchUserInfo(ctx, platCfg)
		cfg.Backends[platName] = streamcontrol.ToAbstractPlatformConfig(ctx, platCfg)
	case kick.ID:
		platCfg := streamcontrol.ConvertPlatformConfig[kick.PlatformSpecificConfig, kick.StreamProfile](
			ctx,
			platCfg,
		)
		status = p.InputKickUserInfo(ctx, platCfg)
		cfg.Backends[platName] = streamcontrol.ToAbstractPlatformConfig(ctx, platCfg)
	case obs.ID:
		platCfg := streamcontrol.ConvertPlatformConfig[obs.PlatformSpecificConfig, obs.StreamProfile](
			ctx,
			platCfg,
		)
		status = p.InputOBSConnectInfo(ctx, platCfg)
		cfg.Backends[platName] = streamcontrol.ToAbstractPlatformConfig(ctx, platCfg)
	}

	switch status {
	case BackendStatusCodeReady:
		cfg.Backends[platName].Enable = ptr(true)
	case BackendStatusCodeNotNow:
	case BackendStatusCodeDisable:
		cfg.Backends[platName].Enable = ptr(false)
	}
	return nil
}

func (p *Panel) startOAuthListenerForRemoteStreamD(
	ctx context.Context,
	streamD *client.Client,
) error {
	logger.Debugf(ctx, "startOAuthListenerForRemoteStreamD")
	defer logger.Debugf(ctx, "/startOAuthListenerForRemoteStreamD")

	ctx, cancelFn := context.WithCancel(ctx)
	receiver, listenPort, err := oauthhandler.NewCodeReceiver(
		ctx,
		p.Config.OAuth.ListenPorts.Twitch,
	)
	if err != nil {
		cancelFn()
		return fmt.Errorf("unable to start listener for OAuth responses: %w", err)
	}

	oauthURLChan, err := streamD.SubscribeToOAuthURLs(ctx, listenPort)
	if err != nil {
		cancelFn()
		return fmt.Errorf("unable to subscribe to OAuth requests of streamd: %w", err)
	}

	logger.Debugf(ctx, "started oauth listener for the remote streamd")
	observability.Go(ctx, func() {
		logger.Debugf(ctx, "oauthListenerForRemoteStreamD")
		defer logger.Debugf(ctx, "/oauthListenerForRemoteStreamD")
		defer cancelFn()
		defer p.DisplayError(fmt.Errorf("oauth handler was closed"))
		for {
			select {
			case <-ctx.Done():
				return
			case req, ok := <-oauthURLChan:
				logger.Debugf(ctx, "<-oauthURLChan")
				if !ok {
					logger.Errorf(ctx, "oauth request receiver is closed")
					return
				}

				if req == nil || req.AuthURL == "" {
					logger.Errorf(ctx, "received an empty oauth request")
					time.Sleep(1 * time.Second)
					continue
				}

				if err := p.openBrowser(ctx, req.GetAuthURL(), "It is required to confirm access in Twitch/YouTube using browser"); err != nil {
					p.DisplayError(
						fmt.Errorf(
							"unable to open browser with URL '%s': %w",
							req.GetAuthURL(),
							err,
						),
					)
					continue
				}

				if req.PlatID == "<OpenBrowser>" {
					logger.Debugf(ctx, "this was just a request to open a browser")
					// TODO: delete me!
					continue
				}

				logger.Debugf(ctx, "waiting for the authentication code")
				code, ok := <-receiver
				if !ok {
					p.DisplayError(fmt.Errorf("auth code receiver channel is closed"))
					continue
				}
				if code == "" {
					p.DisplayError(fmt.Errorf("received auth code is empty"))
					continue
				}
				logger.Debugf(ctx, "received oauth code: %s", code)
				_, err := p.StreamD.SubmitOAuthCode(ctx, &streamd_grpc.SubmitOAuthCodeRequest{
					PlatID: req.GetPlatID(),
					Code:   code,
				})
				if err != nil {
					p.DisplayError(
						fmt.Errorf(
							"unable to submit the oauth code of '%s': %w",
							req.GetPlatID(),
							err,
						),
					)
					continue
				}
			}
		}
	})
	return nil
}

func (p *Panel) checkForUpdates(
	ctx context.Context,
	autoUpdater AutoUpdater,
) (_err error) {
	logger.Debugf(ctx, "checkForUpdates")
	defer func() { logger.Debugf(ctx, "/checkForUpdates: %v", _err) }()

	update, err := autoUpdater.CheckForUpdates(ctx)
	switch err {
	case nil:
	case autoupdater.ErrNoUpdates{}:
		logger.Debugf(ctx, "no updates")
		return
	default:
		logger.Errorf(ctx, "unable to check for updates: %v", err)
		return
	}

	w := dialog.NewConfirm(
		"Install an update?",
		"There is an update for this program, do you want to install it?",
		func(b bool) {
			if !b {
				return
			}
			logger.Debugf(ctx, "update was confirmed")
			err := p.applyUpdate(ctx, update)
			if err != nil {
				p.DisplayError(fmt.Errorf("unable to install the update %s: %w", update.ReleaseName(), err))
			}
		},
		p.mainWindow,
	)
	w.Show()
	return nil
}

func (p *Panel) applyUpdate(
	ctx context.Context,
	update Update,
) error {
	return update.Apply(ctx, p.NewUpdateProgressBar())
}

func (p *Panel) newLoadingWindow(ctx context.Context) fyne.Window {
	logger.FromCtx(ctx).Debugf("newLoadingWindow")
	defer logger.FromCtx(ctx).Debugf("endof newLoadingWindow")

	w := p.app.NewWindow(gconsts.AppName + ": Loading...")
	w.Show()

	return w
}

func (p *Panel) newConnectingWindow(ctx context.Context) fyne.Window {
	logger.FromCtx(ctx).Debugf("newConnectingWindow")
	defer logger.FromCtx(ctx).Debugf("endof newConnectingWindow")

	w := p.app.NewWindow(gconsts.AppName + ": Connecting...")
	w.Show()

	return w
}

func getExpandedConfigPath(configPath string) (string, error) {
	return xpath.Expand(configPath)
}

func (p *Panel) SaveConfig(
	ctx context.Context,
) error {
	err := config.WriteConfigToPath(ctx, p.configPath, p.Config)
	if err != nil {
		return fmt.Errorf("unable to save the config: %w", err)
	}

	return nil
}

func (p *Panel) OpenBrowser(ctx context.Context, url string) error {
	return p.openBrowser(ctx, url, "")
}

func (p *Panel) SetLoggingLevel(ctx context.Context, level logger.Level) {
	observability.LogLevelFilter.SetLevel(level)
}

func removeNonDigits(input string) string {
	var result []rune
	for _, r := range input {
		if unicode.IsDigit(r) {
			result = append(result, r)
		}
	}
	return string(result)
}

func (p *Panel) OnSubmittedOAuthCode(
	ctx context.Context,
	platID streamcontrol.PlatformName,
	code string,
) error {
	logger.Debugf(ctx, "OnSubmittedOAuthCode(ctx, '%s', '%s')", platID, code)
	return nil
}

func (p *Panel) OAuthHandlerTwitch(
	ctx context.Context,
	arg oauthhandler.OAuthHandlerArgument,
) error {
	logger.Infof(ctx, "OAuthHandlerTwitch: %#+v", arg)
	defer logger.Infof(ctx, "/OAuthHandlerTwitch")
	return p.oauthHandler(ctx, twitch.ID, arg)
}

func (p *Panel) OAuthHandlerKick(
	ctx context.Context,
	arg oauthhandler.OAuthHandlerArgument,
) error {
	logger.Infof(ctx, "OAuthHandlerKick: %#+v", arg)
	defer logger.Infof(ctx, "/OAuthHandlerKick")
	return p.oauthHandler(ctx, kick.ID, arg)
}

func (p *Panel) OAuthHandlerYouTube(
	ctx context.Context,
	arg oauthhandler.OAuthHandlerArgument,
) error {
	logger.Infof(ctx, "OAuthHandlerYouTube: %#+v", arg)
	defer logger.Infof(ctx, "/OAuthHandlerYouTube")
	return p.oauthHandler(ctx, youtube.ID, arg)
}

func (p *Panel) oauthHandler(
	ctx context.Context,
	platID streamcontrol.PlatformName,
	arg oauthhandler.OAuthHandlerArgument,
) error {
	logger.Debugf(ctx, "oauthHandler(ctx, '%s', %#+v)", platID, arg)
	defer logger.Debugf(ctx, "/oauthHandler(ctx, '%s', %#+v)", platID, arg)

	ctx, cancelFn := context.WithCancel(ctx)
	defer cancelFn()
	codeCh, _, err := oauthhandler.NewCodeReceiver(ctx, arg.ListenPort)
	if err != nil {
		return fmt.Errorf("unable to make a code receiver: %w", err)
	}

	if err := p.openBrowser(ctx, arg.AuthURL, "It is required to confirm access in Twitch/YouTube using browser"); err != nil {
		return fmt.Errorf("unable to open browser with URL '%s': %w", arg.AuthURL, err)
	}

	logger.Infof(
		ctx,
		"Your browser has been launched (URL: %s).\nPlease approve the permissions.\n",
		arg.AuthURL,
	)

	// Wait for the web server to get the code.
	code := <-codeCh
	logger.Debugf(ctx, "received the auth code")
	err = arg.ExchangeFn(ctx, code)
	if err != nil {
		return fmt.Errorf("unable to exchange the code: %w", err)
	}
	if p.OnInternallySubmittedOAuthCode != nil {
		err := p.OnInternallySubmittedOAuthCode(ctx, platID, code)
		if err != nil {
			return fmt.Errorf("OnInternallySubmittedOAuthCode return an error: %w", err)
		}
	}
	return nil
}

func (p *Panel) openBrowser(
	ctx context.Context,
	urlString string,
	reason string,
) (_err error) {
	logger.Debugf(ctx, "openBrowser(ctx, '%s', '%s')", urlString, reason)
	defer func() { logger.Debugf(ctx, "/openBrowser(ctx, '%s', '%s'): %v", urlString, reason, _err) }()

	if p.Config.Browser.Command != "" {
		args := []string{p.Config.Browser.Command, urlString}
		logger.Debugf(
			ctx,
			"the browser command is configured to be '%s', so running '%s'",
			p.Config.Browser.Command,
			strings.Join(args, " "),
		)
		return exec.Command(args[0], args[1:]...).Start()
	}

	var browserCmd string
	switch runtime.GOOS {
	case "linux":
		if envBrowser := os.Getenv("BROWSER"); envBrowser != "" {
			browserCmd = envBrowser
		} else {
			browserCmd = "xdg-open"
		}
	default:
		url, err := url.Parse(urlString)
		if err != nil {
			return fmt.Errorf("unable to parse URL '%s': %w", urlString, err)
		}
		return p.app.OpenURL(url)
	}

	waitCh := make(chan struct{})

	w := p.app.NewWindow(gconsts.AppName + ": Browser selection window")
	resizeWindow(w, fyne.NewSize(600, 400))
	if reason != "" {
		reason += ". "
	}
	promptText := widget.NewRichTextWithText(reason + "Select a browser for that:")
	promptText.Wrapping = fyne.TextWrapWord
	browserField := widget.NewEntry()
	browserField.SetText(browserCmd)
	browserField.PlaceHolder = "command to execute the browser"
	browserField.OnSubmitted = func(s string) {
		close(waitCh)
	}
	okButton := widget.NewButton("OK", func() {
		close(waitCh)
	})
	w.SetContent(container.NewBorder(
		container.NewVBox(
			promptText,
			browserField,
		),
		okButton,
		nil,
		nil,
		nil,
	))

	w.Show()
	<-waitCh
	w.Hide()

	browserCmd = browserField.Text
	logger.Debugf(ctx, "chosen browser command is: '%s'", browserCmd)
	if browserCmd != p.Config.Browser.Command {
		logger.Debugf(ctx, "updating the browser command in the config")
		p.Config.Browser.Command = browserCmd
		err := p.SaveConfig(ctx)
		errmon.ObserveErrorCtx(ctx, err)
	}

	logger.Debugf(ctx, "openBrowser(ctx, '%s', '%s'): resulting command '%s %s'", urlString, reason, browserCmd, urlString)
	return exec.Command(browserCmd, urlString).Start()
}

var twitchAppsCreateLink = must(url.Parse("https://dev.twitch.tv/console/apps/create"))

func (p *Panel) InputTwitchUserInfo(
	ctx context.Context,
	cfg *streamcontrol.PlatformConfig[twitch.PlatformSpecificConfig, twitch.StreamProfile],
) BackendStatusCode {
	w := p.app.NewWindow(gconsts.AppName + ": Input Twitch user info")
	resizeWindow(w, fyne.NewSize(600, 200))

	clientSecretIsBuiltin := buildvars.TwitchClientID != "" && buildvars.TwitchClientSecret != ""

	channelField := widget.NewEntry()
	channelField.SetPlaceHolder(
		"channel ID (copy&paste it from the browser: https://www.twitch.tv/<the channel ID is here>)",
	)
	clientIDField := widget.NewEntry()
	clientIDField.SetPlaceHolder("client ID")
	if clientSecretIsBuiltin {
		clientIDField.Hide()
	}
	clientSecretField := widget.NewEntry()
	clientSecretField.SetPlaceHolder("client secret")
	if clientSecretIsBuiltin {
		clientSecretField.Hide()
	}
	instructionText := widget.NewRichText(
		&widget.TextSegment{Text: "Go to\n", Style: widget.RichTextStyle{Inline: true}},
		&widget.HyperlinkSegment{Text: twitchAppsCreateLink.String(), URL: twitchAppsCreateLink},
		&widget.TextSegment{
			Text:  `,` + "\n" + `create an application (enter "http://localhost:8091/" as the "OAuth Redirect URLs" value), then click "Manage" then "New Secret", and copy&paste client ID and client secret.`,
			Style: widget.RichTextStyle{Inline: true},
		},
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

	okButton := widget.NewButtonWithIcon("OK", theme.ConfirmIcon(), func() {
		close(waitCh)
	})

	w.SetContent(container.NewBorder(
		widget.NewRichTextWithText("Enter Twitch user info:"),
		container.NewHBox(disableButton, notNowButton, okButton),
		nil,
		nil,
		container.NewVBox(
			channelField,
			clientIDField,
			clientSecretField,
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

	cfg.Config.AuthType = "user"
	channelWords := strings.Split(channelField.Text, "/")
	cfg.Config.Channel = channelWords[len(channelWords)-1]
	cfg.Config.ClientID = clientIDField.Text
	cfg.Config.ClientSecret.Set(clientSecretField.Text)

	return BackendStatusCodeReady
}

var kickAppsCreateLink = must(url.Parse("https://kick.com/settings/developer?action=create"))

func (p *Panel) InputKickUserInfo(
	ctx context.Context,
	cfg *streamcontrol.PlatformConfig[kick.PlatformSpecificConfig, kick.StreamProfile],
) BackendStatusCode {
	w := p.app.NewWindow(gconsts.AppName + ": Input Kick user info")
	resizeWindow(w, fyne.NewSize(600, 200))

	clientSecretIsBuiltin := buildvars.KickClientID != "" && buildvars.KickClientSecret != ""

	channelField := widget.NewEntry()
	channelField.SetPlaceHolder(
		"channel ID (copy&paste it from the browser: https://www.kick.com/<the channel ID is here>)",
	)
	clientIDField := widget.NewEntry()
	clientIDField.SetPlaceHolder("client ID")
	if clientSecretIsBuiltin {
		clientIDField.Hide()
	}
	clientSecretField := widget.NewEntry()
	clientSecretField.SetPlaceHolder("client secret")
	if clientSecretIsBuiltin {
		clientSecretField.Hide()
	}
	instructionText := widget.NewRichText(
		&widget.TextSegment{Text: "Go to\n", Style: widget.RichTextStyle{Inline: true}},
		&widget.HyperlinkSegment{Text: kickAppsCreateLink.String(), URL: kickAppsCreateLink},
		&widget.TextSegment{
			Text:  `,` + "\n" + `create an application (enter "http://localhost:8091/" as the "OAuth Redirect URLs" value), then click "Manage" then "New Secret", and copy&paste client ID and client secret.`,
			Style: widget.RichTextStyle{Inline: true},
		},
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

	okButton := widget.NewButtonWithIcon("OK", theme.ConfirmIcon(), func() {
		close(waitCh)
	})

	w.SetContent(container.NewBorder(
		widget.NewRichTextWithText("Enter Kick user info:"),
		container.NewHBox(disableButton, notNowButton, okButton),
		nil,
		nil,
		container.NewVBox(
			channelField,
			clientIDField,
			clientSecretField,
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

	channelWords := strings.Split(channelField.Text, "/")
	cfg.Config.Channel = channelWords[len(channelWords)-1]
	cfg.Config.ClientID = clientIDField.Text
	cfg.Config.ClientSecret.Set(clientSecretField.Text)

	return BackendStatusCodeReady
}

var youtubeCredentialsCreateLink, _ = url.Parse(
	"https://console.cloud.google.com/apis/credentials/oauthclient",
)

func (p *Panel) InputYouTubeUserInfo(
	ctx context.Context,
	cfg *streamcontrol.PlatformConfig[youtube.PlatformSpecificConfig, youtube.StreamProfile],
) BackendStatusCode {
	w := p.app.NewWindow(gconsts.AppName + ": Input YouTube user info")
	resizeWindow(w, fyne.NewSize(600, 200))

	clientIDField := widget.NewEntry()
	clientIDField.SetPlaceHolder("client ID")
	clientSecretField := widget.NewEntry()
	clientSecretField.SetPlaceHolder("client secret")
	instructionText := widget.NewRichText(
		&widget.TextSegment{Text: "Go to\n", Style: widget.RichTextStyle{Inline: true}},
		&widget.HyperlinkSegment{
			Text: youtubeCredentialsCreateLink.String(),
			URL:  youtubeCredentialsCreateLink,
		},
		&widget.TextSegment{
			Text:  `,` + "\n" + `configure "consent screen" (note: you may add yourself into Test Users to avoid problems further on, and don't forget to add "YouTube Data API v3" scopes) and go back to` + "\n",
			Style: widget.RichTextStyle{Inline: true},
		},
		&widget.HyperlinkSegment{
			Text: youtubeCredentialsCreateLink.String(),
			URL:  youtubeCredentialsCreateLink,
		},
		&widget.TextSegment{
			Text:  `,` + "\n" + `choose "Desktop app", confirm and copy&paste client ID and client secret.`,
			Style: widget.RichTextStyle{Inline: true},
		},
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

	okButton := widget.NewButtonWithIcon("OK", theme.ConfirmIcon(), func() {
		close(waitCh)
	})

	w.SetContent(container.NewBorder(
		widget.NewRichTextWithText("Enter YouTube user info:"),
		container.NewHBox(disableButton, notNowButton, okButton),
		nil,
		nil,
		container.NewVBox(
			clientIDField,
			clientSecretField,
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

	cfg.Config.ClientID = clientIDField.Text
	cfg.Config.ClientSecret.Set(clientSecretField.Text)

	return BackendStatusCodeReady
}

func containTagSubstringCI(tags []string, s string) bool {
	for _, tag := range tags {
		if strings.Contains(strings.ToLower(tag), s) {
			return true
		}
	}
	return false
}

func ptrStringMatchCI(ptrString *string, s string) bool {
	if ptrString == nil {
		return false
	}

	return strings.Contains(strings.ToLower(*ptrString), s)
}

func (p *Panel) openSettingsWindow(ctx context.Context) error {
	cfg, err := p.GetStreamDConfig(ctx)
	if err != nil {
		return fmt.Errorf("unable to get config: %w", err)
	}

	return xsync.DoA2R1(ctx, &p.configLocker, p.openSettingsWindowNoLock, ctx, cfg)
}

func (p *Panel) resetCache(ctx context.Context) {
	p.StreamD.ResetCache(ctx)
	err := p.StreamD.InitCache(ctx)
	if err != nil {
		p.DisplayError(fmt.Errorf("unable to re-initialize the cache: %w", err))
	}
}

func (p *Panel) openMenuWindow(ctx context.Context) {
	popupMenu := widget.NewPopUpMenu(fyne.NewMenu("menu",
		fyne.NewMenuItem("Settings", func() {
			err := p.openSettingsWindow(ctx)
			if err != nil {
				p.DisplayError(fmt.Errorf("unable to handle the settings window: %w", err))
			}
		}),
		fyne.NewMenuItem("Reset cache", func() {
			w := dialog.NewConfirm(
				"Resetting the cache",
				"Are you sure you want to drop the cache and re-download the data (it might take a while)?",
				func(b bool) {
					if b {
						p.resetCache(ctx)
					}
				},
				p.mainWindow,
			)
			w.Show()
		}),
		fyne.NewMenuItem("Link a device", func() {
			p.showLinkDeviceQRWindow(ctx)
		}),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Show errors", func() {
			p.ShowErrorReports()
		}),
		fyne.NewMenuItem("Panic", func() {
			w := dialog.NewConfirm(
				"Panic?",
				"Are you sure you want the app to panic?",
				func(b bool) {
					if b {
						panic("They said I should panic!")
					}
				},
				p.mainWindow,
			)
			w.Show()
		}),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Quit", func() {
			p.app.Quit()
		}),
	), p.mainWindow.Canvas())
	popupMenu.Show()
}

func resizeWindow(w fyne.Window, newSize fyne.Size) {
	w.Resize(newSize)
}

func setupStreamString() string {
	switch runtime.GOOS {
	case "android":
		return "Set!"
	default:
		return "Setup stream"
	}
}

func startStreamString() string {
	switch runtime.GOOS {
	case "android":
		return "Go!"
	default:
		return "Start stream"
	}
}

func (p *Panel) getUpdatedStatus(ctx context.Context) {
	logger.Tracef(ctx, "getUpdatedStatus")
	defer logger.Tracef(ctx, "/getUpdatedStatus")
	p.getUpdatedStatus_startStopStreamButton(ctx)
	p.getUpdatedStatus_backends(ctx)
}

func (p *Panel) getUpdatedStatus_backends(ctx context.Context) {
	p.streamMutex.Do(ctx, func() {
		p.getUpdatedStatus_backends_noLock(ctx)
	})
}
func (p *Panel) getUpdatedStatus_backends_noLock(ctx context.Context) {
	backendEnabled := map[streamcontrol.PlatformName]bool{}
	for _, backendID := range []streamcontrol.PlatformName{
		obs.ID,
		twitch.ID,
		kick.ID,
		youtube.ID,
	} {
		isEnabled, err := p.StreamD.IsBackendEnabled(ctx, backendID)
		if err != nil {
			p.ReportError(
				fmt.Errorf("unable to get info if backend '%s' is enabled: %w", backendID, err),
			)
		}
		backendEnabled[backendID] = isEnabled
	}
	if backendEnabled[twitch.ID] {
		p.twitchCheck.Enable()
	}
	if backendEnabled[kick.ID] {
		p.kickCheck.Enable()
	}
	if backendEnabled[youtube.ID] {
		p.youtubeCheck.Enable()
	}

	if backendEnabled[obs.ID] {
		observability.Call(ctx, func() {
			obsServer, obsServerClose, err := p.StreamD.OBS(ctx)
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
	} else {
		if p.updateStreamClockHandler != nil {
			p.updateStreamClockHandler.Close()
			p.updateStreamClockHandler = nil
		}
		p.startStopButton.SetText(startStreamString())
		p.startStopButton.Icon = theme.MediaRecordIcon()
		p.startStopButton.Importance = widget.SuccessImportance
		p.startStopButton.Disable()
	}
}

func (p *Panel) getUpdatedStatus_startStopStreamButton(ctx context.Context) {
	p.streamMutex.Do(ctx, func() {
		p.getUpdatedStatus_startStopStreamButton_noLock(ctx)
	})
}

func (p *Panel) getUpdatedStatus_startStopStreamButton_noLock(ctx context.Context) {
	obsIsEnabled, _ := p.StreamD.IsBackendEnabled(ctx, obs.ID)
	if obsIsEnabled {
		obsStreamStatus, err := p.StreamD.GetStreamStatus(ctx, obs.ID)
		if err != nil {
			logger.Error(ctx, fmt.Errorf("unable to get stream status from OBS: %v", err))
			return
		}
		logger.Tracef(ctx, "obsStreamStatus == %#+v", obsStreamStatus)

		if obsStreamStatus.IsActive {
			p.startStopButton.Icon = theme.MediaStopIcon()
			p.startStopButton.Importance = widget.DangerImportance
			p.startStopButton.Enable()
			if p.updateStreamClockHandler == nil {
				if obsStreamStatus.StartedAt == nil {
					p.startStopButton.SetText("Stop stream")
				} else {
					p.startStopButton.SetText("...")
					logger.Debugf(ctx, "stream was already started at %s", obsStreamStatus.StartedAt.Format(time.RFC3339))
					p.updateStreamClockHandler = newUpdateTimerHandler(p.startStopButton, *obsStreamStatus.StartedAt)
				}
			}
			return
		}
	}

	if p.updateStreamClockHandler != nil {
		p.updateStreamClockHandler.Close()
		p.updateStreamClockHandler = nil
	}
	p.startStopButton.SetText(startStreamString())
	p.startStopButton.Icon = theme.MediaRecordIcon()
	p.startStopButton.Importance = widget.SuccessImportance

	ytIsEnabled, err := p.StreamD.IsBackendEnabled(ctx, youtube.ID)
	if err != nil {
		logger.Error(ctx, fmt.Errorf("unable to check if YouTube is enabled: %v", err))
		return
	}

	if !ytIsEnabled || !p.youtubeCheck.Checked {
		if obsIsEnabled {
			p.startStopButton.Enable()
		}
		return
	}

	ytStreamStatus, err := p.StreamD.GetStreamStatus(ctx, youtube.ID)
	if err != nil {
		logger.Error(ctx, fmt.Errorf("unable to get stream status from YouTube: %v", err))
		return
	}
	logger.Tracef(ctx, "ytStreamStatus == %#+v", ytStreamStatus)

	if d, ok := ytStreamStatus.CustomData.(youtube.StreamStatusCustomData); ok {
		logger.Tracef(
			ctx,
			"len(d.UpcomingBroadcasts) == %d; len(d.Streams) == %d",
			len(d.UpcomingBroadcasts),
			len(d.Streams),
		)
		if len(d.UpcomingBroadcasts) != 0 {
			p.startStopButton.Enable()
		}
	}
}

func (p *Panel) initMainWindow(
	ctx context.Context,
	startingPage consts.Page,
) {
	logger.Debugf(ctx, "initMainWindow")
	defer logger.Debugf(ctx, "/initMainWindow")

	w := p.newPermanentWindow(ctx, gconsts.AppName)
	p.mainWindow = w
	w.SetMaster()
	resizeWindow(w, fyne.NewSize(400, 600))

	profileFilter := widget.NewEntry()
	profileFilter.SetPlaceHolder("filter")
	profileFilter.OnChanged = func(s string) {
		p.setFilter(ctx, s)
	}

	selectedProfileButtons := []*widget.Button{
		widget.NewButtonWithIcon("", theme.ContentCopyIcon(), func() {
			p.cloneProfileWindow(ctx)
		}),
		widget.NewButtonWithIcon("", theme.SettingsIcon(), func() {
			p.editProfileWindow(ctx)
		}),
		widget.NewButtonWithIcon("", theme.ContentRemoveIcon(), func() {
			p.deleteProfileWindow(ctx)
		}),
	}

	menuButton := widget.NewButtonWithIcon("", theme.MenuIcon(), func() {
		p.openMenuWindow(ctx)
	})

	profileControl := container.NewHBox(
		widget.NewSeparator(),
		widget.NewRichTextWithText("Profile:"),
		widget.NewButtonWithIcon("", theme.ContentAddIcon(), func() {
			p.newProfileWindow(ctx)
		}),
	)

	topPanel := container.NewHBox(
		menuButton,
		profileControl,
	)

	for _, button := range selectedProfileButtons {
		button.Disable()
		profileControl.Add(button)
	}

	p.setupStreamButton = widget.NewButtonWithIcon(
		setupStreamString(),
		theme.SettingsIcon(),
		func() {
			p.onSetupStreamButton(ctx)
		},
	)
	p.setupStreamButton.Disable()

	p.startStopButton = widget.NewButtonWithIcon(
		startStreamString(),
		theme.MediaRecordIcon(),
		func() {
			p.onStartStopButton(ctx)
		},
	)
	p.startStopButton.Importance = widget.SuccessImportance
	p.startStopButton.Disable()

	profilesList := widget.NewList(
		p.profilesListLength,
		p.profilesListItemCreate,
		p.profilesListItemUpdate,
	)
	profilesList.OnSelected = func(id widget.ListItemID) {
		p.onProfilesListSelect(id)
		for _, button := range selectedProfileButtons {
			button.Enable()
		}
	}
	profilesList.OnUnselected = func(id widget.ListItemID) {
		p.onProfilesListUnselect(id)
		for _, button := range selectedProfileButtons {
			button.Disable()
		}
	}
	p.streamTitleField = widget.NewEntry()
	p.streamTitleField.SetPlaceHolder("stream title")
	p.streamTitleField.OnChanged = func(s string) {
		if len(s) > youtubeTitleLength {
			p.streamTitleField.SetText(s[:youtubeTitleLength])
		}
	}
	p.streamTitleField.OnSubmitted = func(s string) {
		if p.updateStreamClockHandler == nil {
			return
		}

		p.startStopButton.OnTapped()
		p.startStopButton.OnTapped()
	}

	p.streamDescriptionField = widget.NewMultiLineEntry()
	p.streamDescriptionField.SetPlaceHolder("stream description")
	p.streamDescriptionField.OnSubmitted = func(s string) {
		if p.updateStreamClockHandler == nil {
			return
		}

		p.startStopButton.OnTapped()
		p.startStopButton.OnTapped()
	}

	p.twitchCheck = widget.NewCheck("Twitch", nil)
	p.twitchCheck.SetChecked(true)
	p.twitchCheck.Disable()

	p.kickCheck = widget.NewCheck("Kick", nil)
	p.kickCheck.SetChecked(true)
	p.kickCheck.Disable()

	p.youtubeCheck = widget.NewCheck("YouTube", nil)
	p.youtubeCheck.SetChecked(true)
	p.youtubeCheck.Disable()

	bottomPanel := container.NewVBox(
		p.streamTitleField,
		p.streamDescriptionField,
		container.NewBorder(
			nil,
			nil,
			container.NewHBox(p.twitchCheck, p.youtubeCheck, p.setupStreamButton),
			nil,
			p.startStopButton,
		),
	)

	controlPage := container.NewBorder(
		profileFilter,
		bottomPanel,
		nil,
		nil,
		profilesList,
	)

	var prevScene string
	p.obsSelectScene = widget.NewSelect(nil, func(s string) {
		if s == prevScene {
			logger.Debugf(ctx, "OBS scene remained to be '%s'", s)
			return
		}
		prevScene = s
		logger.Debugf(ctx, "OBS scene is changed to '%s'", s)
		err := p.obsSetScene(ctx, s)
		if err != nil {
			p.DisplayError(err)
		}
	})
	obsPage := container.NewBorder(
		nil,
		nil,
		nil,
		nil,
		container.NewVBox(
			container.NewHBox(widget.NewLabel("Scene:"), p.obsSelectScene),
		),
	)

	p.streamServersWidget = container.NewVBox()
	addStreamServerButton := widget.NewButtonWithIcon("Add server", theme.ContentAddIcon(), func() {
		p.openAddStreamServerWindow(ctx)
	})
	p.streamsWidget = container.NewVBox()
	addStreamButton := widget.NewButtonWithIcon("Add stream", theme.ContentAddIcon(), func() {
		p.openAddStreamWindow(ctx)
	})
	p.destinationsWidget = container.NewVBox()
	addDestination := widget.NewButtonWithIcon("Add destination", theme.ContentAddIcon(), func() {
		p.openAddDestinationWindow(ctx)
	})
	p.restreamsWidget = container.NewVBox()
	addRestream := widget.NewButtonWithIcon("Add restream", theme.ContentAddIcon(), func() {
		p.openAddRestreamWindow(ctx)
	})
	playersLabel := widget.NewLabel("Players:")
	p.playersWidget = container.NewVBox()
	addPlayer := widget.NewButtonWithIcon("Add player", theme.ContentAddIcon(), func() {
		p.openAddPlayerWindow(ctx)
	})
	switch runtime.GOOS {
	case "android":
		playersLabel.Hide()
		p.playersWidget.Hide()
		addPlayer.Hide()
	}
	restreamPage := container.NewVScroll(container.NewBorder(
		nil,
		nil,
		nil,
		nil,
		container.NewVBox(
			widget.NewLabel("Servers:"),
			p.streamServersWidget,
			addStreamServerButton,
			widget.NewLabel("Streams:"),
			p.streamsWidget,
			addStreamButton,
			widget.NewLabel("Destinations:"),
			p.destinationsWidget,
			addDestination,
			widget.NewLabel("Resteams:"),
			p.restreamsWidget,
			addRestream,
			playersLabel,
			p.playersWidget,
			addPlayer,
		),
	))

	selectServerUI := newSelectServerUI(p)
	selectServerPage := selectServerUI.CanvasObject

	timersUI := NewTimersUI(ctx, p)
	triggersUI := NewTriggerRulesUI(ctx, p)

	moreControlPage := container.NewBorder(
		nil,
		nil,
		nil,
		nil,
		container.NewVBox(
			timersUI.CanvasObject,
			widget.NewSeparator(),
			triggersUI.CanvasObject,
			widget.NewSeparator(),
		),
	)

	chatPage := container.NewBorder(nil, nil, nil, nil)
	chatUI, err := newChatUI(ctx, p)
	if err != nil {
		logger.Errorf(ctx, "unable to initialize the page for chat: %v", err)
	} else {
		chatPage = container.NewBorder(
			nil,
			nil,
			nil,
			nil,
			chatUI.CanvasObject,
		)
	}

	p.dashboardShowHideButton = widget.NewButtonWithIcon("Open", theme.ComputerIcon(), func() {
		p.dashboardLocker.Do(ctx, func() {
			if p.dashboardWindow == nil {
				observability.Go(ctx, func() { p.focusDashboardWindow(ctx) })
			} else {
				p.dashboardWindow.Window.Close()
			}
		})
	})
	dashboardPage := container.NewBorder(
		p.dashboardShowHideButton,
		widget.NewButtonWithIcon("Settings", theme.SettingsIcon(), func() {
			p.newDashboardSettingsWindow(ctx)
		}),
		nil,
		nil,
	)

	var cancelPage context.CancelFunc
	setPage := func(page consts.Page) {
		logger.Debugf(ctx, "setPage(%s)", page)
		defer logger.Debugf(ctx, "/setPage(%s)", page)

		if cancelPage != nil {
			cancelPage()
		}

		var pageCtx context.Context
		pageCtx, cancelPage = context.WithCancel(ctx)

		switch page {
		case consts.PageControl:
			obsPage.Hide()
			restreamPage.Hide()
			moreControlPage.Hide()
			chatPage.Hide()
			dashboardPage.Hide()
			selectServerPage.Hide()
			timersUI.StopRefreshingFromRemote(ctx)
			profileControl.Show()
			controlPage.Show()
		case consts.PageMoreControl:
			obsPage.Hide()
			restreamPage.Hide()
			chatPage.Hide()
			profileControl.Hide()
			controlPage.Hide()
			dashboardPage.Hide()
			selectServerPage.Hide()
			moreControlPage.Show()
			timersUI.StartRefreshingFromRemote(ctx)
		case consts.PageChat:
			obsPage.Hide()
			restreamPage.Hide()
			moreControlPage.Hide()
			profileControl.Hide()
			controlPage.Hide()
			dashboardPage.Hide()
			selectServerPage.Hide()
			chatPage.Show()
		case consts.PageDashboard:
			profileControl.Hide()
			controlPage.Hide()
			moreControlPage.Hide()
			obsPage.Hide()
			restreamPage.Hide()
			chatPage.Hide()
			moreControlPage.Hide()
			obsPage.Hide()
			selectServerPage.Hide()
			dashboardPage.Show()
			p.focusDashboardWindow(ctx)
		case consts.PageOBS:
			controlPage.Hide()
			profileControl.Hide()
			restreamPage.Hide()
			chatPage.Hide()
			moreControlPage.Hide()
			dashboardPage.Hide()
			selectServerPage.Hide()
			timersUI.StopRefreshingFromRemote(ctx)
			obsPage.Show()
		case consts.PageRestream:
			controlPage.Hide()
			profileControl.Hide()
			moreControlPage.Hide()
			chatPage.Hide()
			obsPage.Hide()
			dashboardPage.Hide()
			selectServerPage.Hide()
			timersUI.StopRefreshingFromRemote(ctx)
			restreamPage.Show()
			p.startRestreamPage(pageCtx)
		case consts.PageSelectServer:
			profileControl.Hide()
			controlPage.Hide()
			moreControlPage.Hide()
			obsPage.Hide()
			restreamPage.Hide()
			chatPage.Hide()
			dashboardPage.Hide()
			timersUI.StopRefreshingFromRemote(ctx)
			selectServerPage.Show()
		}
	}

	pageSelector := widget.NewSelect(
		[]string{
			string(consts.PageControl),
			string(consts.PageMoreControl),
			string(consts.PageChat),
			string(consts.PageDashboard),
			string(consts.PageOBS),
			string(consts.PageRestream),
		},
		func(page string) {
			setPage(consts.Page(page))
		},
	)
	pageSelector.SetSelected(string(startingPage))
	topPanel.Add(layout.NewSpacer())
	topPanel.Add(pageSelector)

	p.statusPanel = widget.NewLabel("")
	p.statusPanel.Wrapping = fyne.TextWrapWord
	p.statusPanel.Truncation = fyne.TextTruncateEllipsis

	w.SetContent(container.NewBorder(
		container.NewVBox(
			p.statusPanel,
			widget.NewSeparator(),
			topPanel,
		),
		container.NewVBox(
			widget.NewSeparator(),
		),
		nil,
		nil,
		container.NewStack(controlPage, moreControlPage, chatPage, dashboardPage, obsPage, restreamPage),
	))

	w.Show()
	p.profilesListWidget = profilesList

	if _, ok := p.StreamD.(*client.Client); ok {
		p.subscribeUpdateControlPage(ctx)
	}
	p.statusPanelSet("ready")
}

func (p *Panel) subscribeUpdateControlPage(ctx context.Context) {
	logger.Debugf(ctx, "subscribe to streams and config changes")
	defer logger.Debugf(ctx, "/subscribe to streams and config changes")

	chStreams, err := p.StreamD.SubscribeToStreamsChanges(ctx)
	if err != nil {
		p.DisplayError(err)
		//return
	}

	// TODO: deduplicate with localConfigCacheUpdater
	chConfigs, err := p.StreamD.SubscribeToConfigChanges(ctx)
	if err != nil {
		p.DisplayError(err)
		//return
	}

	p.getUpdatedStatus(ctx)

	observability.Go(ctx, func() {
		t := time.NewTicker(time.Second * 5)
		for {
			select {
			case <-ctx.Done():
				return
			case <-chStreams:
			case <-chConfigs:
			case <-t.C:
			}
			p.getUpdatedStatus(ctx)
		}
	})
}

func (p *Panel) execCommand(
	ctx context.Context,
	cmdString string,
	execContext any,
) {
	cmdExpanded, err := command.Expand(ctx, cmdString, execContext)
	if err != nil {
		p.DisplayError(err)
	}

	if len(cmdExpanded) == 0 {
		return
	}

	logger.Infof(ctx, "executing %s with arguments %v", cmdExpanded[0], cmdExpanded[1:])
	cmd := exec.Command(cmdExpanded[0], cmdExpanded[1:]...)
	err = child_process_manager.ConfigureCommand(cmd)
	if err != nil {
		logger.Errorf(ctx, "unable to configure the command so that the process will die automatically: %v", err)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	observability.Go(ctx, func() {
		err := cmd.Run()
		if err == nil {
			err = child_process_manager.AddChildProcess(cmd.Process)
			if err != nil {
				if runtime.GOOS == "windows" {
					// this is actually an error, but I have no idea how to fix it, so demoting to a debug message
					logger.Debugf(ctx, "unable to register the command to be auto-killed: %v", err)
				} else {
					logger.Errorf(ctx, "unable to register the command to be auto-killed: %v", err)
				}
			}
		} else {
			p.DisplayError(err)
		}

		logger.Debugf(ctx, "stdout: %s", stdout.Bytes())
		logger.Debugf(ctx, "stderr: %s", stderr.Bytes())
	})
}

func (p *Panel) streamIsRunning(
	ctx context.Context,
	platID streamcontrol.PlatformName,
) bool {
	streamStatus, err := p.StreamD.GetStreamStatus(ctx, platID)
	if err != nil {
		p.DisplayError(err)
		return false
	}

	return streamStatus.IsActive
}

func (p *Panel) setupStream(ctx context.Context) {
	p.streamMutex.Do(ctx, func() {
		p.setupStreamNoLock(ctx)
	})
}

func (p *Panel) setupStreamNoLock(ctx context.Context) {
	if p.streamTitleField.Text == "" {
		p.DisplayError(fmt.Errorf("title is not set"))
		return
	}

	backendEnabled := map[streamcontrol.PlatformName]bool{}
	for _, backendID := range []streamcontrol.PlatformName{
		obs.ID,
		twitch.ID,
		kick.ID,
		youtube.ID,
	} {
		isEnabled, err := p.StreamD.IsBackendEnabled(ctx, backendID)
		if err != nil {
			p.DisplayError(
				fmt.Errorf("unable to get info if backend '%s' is enabled: %w", backendID, err),
			)
			return
		}
		backendEnabled[backendID] = isEnabled
	}

	if backendEnabled[obs.ID] {
		obsIsActive := p.streamIsRunning(ctx, obs.ID)
		if !obsIsActive {
			p.startStopButton.Disable()
			defer p.startStopButton.Enable()
		}
	}

	profile := p.getSelectedProfile()

	if p.twitchCheck.Checked && backendEnabled[twitch.ID] {
		err := p.StreamD.StartStream(
			ctx,
			twitch.ID,
			p.streamTitleField.Text,
			p.streamDescriptionField.Text,
			profile.PerPlatform[twitch.ID],
		)
		if err != nil {
			p.DisplayError(fmt.Errorf("unable to setup the stream on Twitch: %w", err))
		}
	}

	if p.kickCheck.Checked && backendEnabled[kick.ID] {
		err := p.StreamD.StartStream(
			ctx,
			kick.ID,
			p.streamTitleField.Text,
			p.streamDescriptionField.Text,
			profile.PerPlatform[kick.ID],
		)
		if err != nil {
			p.DisplayError(fmt.Errorf("unable to setup the stream on Twitch: %w", err))
		}
	}

	if p.youtubeCheck.Checked && backendEnabled[youtube.ID] {
		if p.streamIsRunning(ctx, youtube.ID) {
			logger.Debugf(ctx, "updating the stream info at YouTube")
			err := p.StreamD.UpdateStream(
				ctx,
				youtube.ID,
				p.streamTitleField.Text,
				p.streamDescriptionField.Text,
				profile.PerPlatform[youtube.ID],
			)
			logger.Infof(ctx, "updated the stream info at YouTube")
			if err != nil {
				p.DisplayError(fmt.Errorf("unable to start the stream on YouTube: %w", err))
			}
		} else {
			logger.Debugf(ctx, "creating the stream at YouTube")
			err := p.StreamD.StartStream(
				ctx,
				youtube.ID,
				p.streamTitleField.Text,
				p.streamDescriptionField.Text,
				profile.PerPlatform[youtube.ID],
			)
			logger.Infof(ctx, "created the stream at YouTube")
			if err != nil {
				p.DisplayError(fmt.Errorf("unable to start the stream on YouTube: %w", err))
			}
		}

		// I don't know why, but if we don't open the livestream control page on YouTube
		// in the browser, then the stream does not want to start.
		//
		// And here we wait until the hack with opening the page will complete.
		observability.Go(ctx, func() {
			waitFor := 15 * time.Second
			deadline := time.Now().Add(waitFor)

			p.streamMutex.Do(ctx, func() {
				p.startStopButton.Disable()
				p.startStopButton.Icon = theme.ViewRefreshIcon()
				p.startStopButton.Importance = widget.DangerImportance

				t := time.NewTicker(100 * time.Millisecond)
				defer t.Stop()
				for {
					<-t.C
					timeDiff := time.Until(deadline).Truncate(100 * time.Millisecond)
					if timeDiff < 0 {
						return
					}
					p.startStopButton.SetText(fmt.Sprintf("%.1fs", timeDiff.Seconds()))
				}
			})
		})
	}
}

func (p *Panel) startStream(ctx context.Context) {
	p.streamMutex.ManualLock(ctx)
	defer func() {
		observability.Go(ctx, func() {
			time.Sleep(10 * time.Second) // TODO: remove this
			p.streamMutex.ManualUnlock(ctx)
		})
	}()

	if p.startStopButton.Disabled() {
		return
	}
	p.startStopButton.Disable()
	defer p.startStopButton.Enable()

	p.startStopButton.SetText("Starting stream...")
	p.startStopButton.Icon = theme.MediaStopIcon()
	p.startStopButton.Importance = widget.DangerImportance
	if p.updateStreamClockHandler != nil {
		p.updateStreamClockHandler.Stop()
	}
	p.updateStreamClockHandler = newUpdateTimerHandler(p.startStopButton, time.Now())

	isEnabled, err := p.StreamD.IsBackendEnabled(ctx, obs.ID)
	if err != nil {
		p.DisplayError(fmt.Errorf("unable to get info if backend '%s' is enabled: %w", obs.ID, err))
		return
	}

	if !isEnabled {
		p.DisplayError(fmt.Errorf("connection to OBS is not configured: %w", err))
		return
	}

	profile := p.getSelectedProfile()
	err = p.StreamD.StartStream(
		ctx,
		obs.ID,
		p.streamTitleField.Text,
		p.streamDescriptionField.Text,
		profile.PerPlatform[obs.ID],
	)
	if err != nil {
		p.DisplayError(fmt.Errorf("unable to start the stream on YouTube: %w", err))
	}

	p.startStopButton.Refresh()
	p.afterStreamStart(ctx)
}

func (p *Panel) afterStreamStart(ctx context.Context) {
	var platCfg *streamcontrol.AbstractPlatformConfig
	p.configCacheLocker.Do(ctx, func() {
		platCfg = p.configCache.Backends[obs.ID]
	})
	if onStreamStart, ok := platCfg.GetCustomString(config.CustomConfigKeyAfterStreamStart); ok {
		p.execCommand(ctx, onStreamStart, nil)
	}

	observability.Go(ctx, func() { p.openStreamStartedWindow(ctx) })
}

func (p *Panel) stopStream(ctx context.Context) {
	observability.Go(ctx, func() {
		p.streamStartedLocker.Do(ctx, func() {
			if p.streamStartedWindow != nil {
				p.streamStartedWindow.Close()
				p.streamStartedWindow = nil
			}
		})
	})
	p.streamMutex.Do(ctx, func() {
		p.doStopStream(ctx)
	})
}
func (p *Panel) doStopStream(ctx context.Context) {
	backendEnabled := map[streamcontrol.PlatformName]bool{}
	for _, backendID := range []streamcontrol.PlatformName{
		obs.ID,
		youtube.ID,
		twitch.ID,
	} {
		isEnabled, err := p.StreamD.IsBackendEnabled(ctx, backendID)
		if err != nil {
			p.DisplayError(
				fmt.Errorf("unable to get info if backend '%s' is enabled: %w", backendID, err),
			)
			return
		}
		backendEnabled[backendID] = isEnabled
	}

	p.startStopButton.Disable()

	streamDCfg, err := p.GetStreamDConfig(ctx)
	if err != nil {
		p.DisplayError(fmt.Errorf("unable to get the config from the backend: %w", err))
	}

	if p.updateStreamClockHandler != nil {
		p.updateStreamClockHandler.Stop()
		p.updateStreamClockHandler = nil
	}

	if backendEnabled[obs.ID] {
		if streamDCfg != nil {
			obsCfg := streamcontrol.GetPlatformConfig[obs.PlatformSpecificConfig, obs.StreamProfile](ctx, streamDCfg.Backends, obs.ID)
			if obsCfg.Config.SceneAfterStream.Name != "" {
				p.startStopButton.SetText("Switching the scene")
				err := p.obsSetScene(ctx, obsCfg.Config.SceneAfterStream.Name)
				if err != nil {
					p.DisplayError(fmt.Errorf("unable to change the OBS scene: %w", err))
				}
			}
			if obsCfg.Config.SceneAfterStream.Duration > 0 {
				p.startStopButton.SetText(fmt.Sprintf("Holding the scene: %s", obsCfg.Config.SceneAfterStream.Duration))
				time.Sleep(obsCfg.Config.SceneAfterStream.Duration)
			}
		}

		p.startStopButton.SetText("Stopping OBS...")
		err := p.StreamD.EndStream(ctx, obs.ID)
		if err != nil {
			p.DisplayError(fmt.Errorf("unable to stop the stream on OBS: %w", err))
		}
	}

	if p.youtubeCheck.Checked && backendEnabled[youtube.ID] {
		p.startStopButton.SetText("Stopping YouTube...")
		err := p.StreamD.EndStream(ctx, youtube.ID)
		if err != nil {
			p.DisplayError(fmt.Errorf("unable to stop the stream on YouTube: %w", err))
		}
	}

	if backendEnabled[twitch.ID] {
		p.twitchCheck.Enable()
	}
	if backendEnabled[kick.ID] {
		p.kickCheck.Enable()
	}
	if backendEnabled[youtube.ID] {
		p.youtubeCheck.Enable()
	}

	p.startStopButton.SetText("OnStopStream command...")

	var platCfg *streamcontrol.AbstractPlatformConfig
	p.configCacheLocker.Do(ctx, func() {
		platCfg = p.configCache.Backends[obs.ID]
	})
	if onStreamStop, ok := platCfg.GetCustomString(config.CustomConfigKeyAfterStreamStop); ok {
		p.execCommand(ctx, onStreamStop, nil)
	}

	p.startStopButton.SetText(startStreamString())
	p.startStopButton.Icon = theme.MediaRecordIcon()
	p.startStopButton.Importance = widget.SuccessImportance

	p.startStopButton.Refresh()
}

func (p *Panel) onSetupStreamButton(ctx context.Context) {
	p.setupStream(ctx)
}

func (p *Panel) onStartStopButton(ctx context.Context) {
	var shouldStop bool
	p.streamMutex.Do(ctx, func() {
		shouldStop = p.updateStreamClockHandler != nil
	})

	if shouldStop {
		w := dialog.NewConfirm(
			"Ending the stream",
			"Are you sure you want to end the stream?",
			func(b bool) {
				if b {
					p.stopStream(ctx)
				}
			},
			p.mainWindow,
		)
		w.Show()
	} else {
		w := dialog.NewConfirm(
			"Starting the stream",
			"Are you ready to start the stream?",
			func(b bool) {
				if b {
					p.startStream(ctx)
				}
			},
			p.mainWindow,
		)
		w.Show()
	}
}

func cleanTwitchCategoryName(in string) string {
	return strings.ToLower(strings.Trim(in, " "))
}

func cleanYoutubeRecordingName(in string) string {
	return strings.ToLower(strings.Trim(in, " "))
}

func ptr[T any](in T) *T {
	return &in
}

type tagEditButton struct {
	Icon     fyne.Resource
	Label    string
	Callback func(*tagsEditorSection, []tagInfo)
}

type tagInfo struct {
	Tag       string
	Container fyne.CanvasObject
}

type tagsEditorSection struct {
	fyne.CanvasObject
	tagsContainer *fyne.Container
	Tags          []tagInfo
}

func (t *tagsEditorSection) getTagInfo(tag string) tagInfo {
	for _, tagCmp := range t.Tags {
		if tagCmp.Tag == tag {
			return tagCmp
		}
	}
	return tagInfo{}
}

func (t *tagsEditorSection) getIdx(tag tagInfo) int {
	for idx, tagCmp := range t.Tags {
		if tagCmp.Tag == tag.Tag {
			return idx
		}
	}

	return -1
}

func (t *tagsEditorSection) move(srcIdx, dstIdx int) {
	newTags := make([]tagInfo, 0, len(t.Tags))
	newObjs := make([]fyne.CanvasObject, 0, len(t.Tags))

	objs := t.tagsContainer.Objects
	for i := 0; i < len(t.Tags); i++ {
		if i == dstIdx {
			newTags = append(newTags, t.Tags[srcIdx])
			newObjs = append(newObjs, objs[srcIdx])
		}
		if i == srcIdx {
			continue
		}
		newTags = append(newTags, t.Tags[i])
		newObjs = append(newObjs, objs[i])
	}
	if dstIdx >= len(t.Tags) {
		newTags = append(newTags, t.Tags[srcIdx])
		newObjs = append(newObjs, objs[srcIdx])
	}

	t.Tags = newTags
	t.tagsContainer.Objects = newObjs
	t.tagsContainer.Refresh()
}

func (t *tagsEditorSection) GetTags() []string {
	result := make([]string, 0, len(t.Tags))
	for _, tag := range t.Tags {
		result = append(result, tag.Tag)
	}
	return result
}

func newTagsEditor(
	initialTags []string,
	tagCountLimit uint,
	additionalButtons ...tagEditButton,
) *tagsEditorSection {
	t := &tagsEditorSection{}
	tagsEntryField := widget.NewEntry()
	tagsEntryField.SetPlaceHolder("add a tag")
	s := tagsEntryField.Size()
	s.Width = 200
	tagsMap := map[string]struct{}{}
	tagsEntryField.Resize(s)
	tagsControlsContainer := container.NewHBox()
	t.tagsContainer = container.NewGridWrap(fyne.NewSize(200, 30))
	selectedTags := map[string]struct{}{}
	selectedTagsOrdered := func() []tagInfo {
		var result []tagInfo
		for _, tag := range t.Tags {
			if _, ok := selectedTags[tag.Tag]; ok {
				result = append(result, tag)
			}
		}
		return result
	}

	tagContainerToFirstButton := widget.NewButtonWithIcon("", theme.MediaFastRewindIcon(), func() {
		for _, tag := range selectedTagsOrdered() {
			idx := t.getIdx(tag)
			if idx < 1 {
				return
			}
			t.move(idx, 0)
		}
	})
	tagsControlsContainer.Add(tagContainerToFirstButton)

	tagContainerToPrevButton := widget.NewButtonWithIcon("", theme.NavigateBackIcon(), func() {
		for _, tag := range selectedTagsOrdered() {
			idx := t.getIdx(tag)
			if idx < 1 {
				return
			}
			t.move(idx, idx-1)
		}
	})
	tagsControlsContainer.Add(tagContainerToPrevButton)
	tagContainerToNextButton := widget.NewButtonWithIcon("", theme.NavigateNextIcon(), func() {
		for _, tag := range reverse(selectedTagsOrdered()) {
			idx := t.getIdx(tag)
			if idx >= len(t.Tags)-1 {
				return
			}
			t.move(idx, idx+2)
		}
	})
	tagsControlsContainer.Add(tagContainerToNextButton)
	tagContainerToLastButton := widget.NewButtonWithIcon("", theme.MediaFastForwardIcon(), func() {
		for _, tag := range reverse(selectedTagsOrdered()) {
			idx := t.getIdx(tag)
			if idx >= len(t.Tags)-1 {
				return
			}
			t.move(idx, len(t.Tags))
		}
	})
	tagsControlsContainer.Add(tagContainerToLastButton)

	removeTag := func(tag string) {
		tagInfo := t.getTagInfo(tag)
		t.tagsContainer.Remove(tagInfo.Container)
		delete(tagsMap, tag)
		for idx, tagCmp := range t.Tags {
			if tagCmp.Tag == tag {
				t.Tags = append(t.Tags[:idx], t.Tags[idx+1:]...)
				break
			}
		}
	}

	tagsControlsContainer.Add(widget.NewSeparator())
	tagsControlsContainer.Add(widget.NewSeparator())
	tagsControlsContainer.Add(widget.NewSeparator())
	tagContainerRemoveButton := widget.NewButtonWithIcon("", theme.ContentClearIcon(), func() {
		for tag := range selectedTags {
			removeTag(tag)
		}
	})
	tagsControlsContainer.Add(tagContainerRemoveButton)

	tagsControlsContainer.Add(widget.NewSeparator())
	tagsControlsContainer.Add(widget.NewSeparator())
	tagsControlsContainer.Add(widget.NewSeparator())
	for _, additionalButtonInfo := range additionalButtons {
		button := widget.NewButtonWithIcon(
			additionalButtonInfo.Label,
			additionalButtonInfo.Icon,
			func() {
				additionalButtonInfo.Callback(t, selectedTagsOrdered())
			},
		)
		tagsControlsContainer.Add(button)
	}

	addTag := func(tagName string) {
		if tagCountLimit > 0 && len(t.Tags) >= int(tagCountLimit) {
			removeTag(t.Tags[tagCountLimit-1].Tag)
		}
		tagName = strings.Trim(tagName, " ")
		if tagName == "" {
			return
		}
		if _, ok := tagsMap[tagName]; ok {
			return
		}

		tagsMap[tagName] = struct{}{}
		tagContainer := container.NewHBox()
		t.Tags = append(t.Tags, tagInfo{
			Tag:       tagName,
			Container: tagContainer,
		})

		tagLabel := tagName
		overflown := false
		for {
			size := fyne.MeasureText(
				tagLabel,
				fyne.CurrentApp().Settings().Theme().Size("text"),
				fyne.TextStyle{},
			)
			if size.Width < 100 {
				break
			}
			tagLabel = tagLabel[:len(tagLabel)-1]
			overflown = true
		}
		if overflown {
			tagLabel += "…"
		}
		tagSelector := widget.NewCheck(tagLabel, func(b bool) {
			if b {
				selectedTags[tagName] = struct{}{}
			} else {
				delete(selectedTags, tagName)
			}
		})
		tagContainer.Add(tagSelector)
		t.tagsContainer.Add(tagContainer)
	}
	tagsEntryField.OnSubmitted = func(text string) {
		for _, tag := range strings.Split(text, ",") {
			addTag(tag)
		}
		tagsEntryField.SetText("")
	}

	for _, tag := range initialTags {
		addTag(tag)
	}
	t.CanvasObject = container.NewVBox(
		t.tagsContainer,
		tagsControlsContainer,
		tagsEntryField,
	)
	return t
}

const aggregationDelayBeforeNotificationStart = time.Second
const aggregationDelayBeforeNotificationEnd = 100 * time.Millisecond

func (p *Panel) showWaitStreamDCallWindow(ctx context.Context) {
	atomic.AddInt32(&p.waitStreamDCallWindowCounter, 1)
	observability.Go(ctx, func() {
		defer func() {
			<-ctx.Done()
			p.waitStreamDCallWindowLocker.Do(ctx, func() {
				if atomic.AddInt32(&p.waitStreamDCallWindowCounter, -1) != 0 {
					return
				}
				time.Sleep(aggregationDelayBeforeNotificationEnd)
				// TODO: set "ready" only if we have set "in process" before.
				p.statusPanelSet("ready")
				logger.Tracef(ctx, "closed the 'network operation is in progress' notification")
			})
		}()

		select {
		case <-ctx.Done():
			return
		case <-time.After(aggregationDelayBeforeNotificationStart):
		}

		p.waitStreamDCallWindowLocker.Do(ctx, func() {
			logger.Debugf(ctx, "making a 'network operation is in progress' notification")
			p.statusPanelSet("Network operation is in process, please wait...")
		})
	})
}

func (p *Panel) showWaitStreamDConnectWindow(ctx context.Context) {
	atomic.AddInt32(&p.waitStreamDConnectWindowCounter, 1)
	observability.Go(ctx, func() {
		defer func() {
			<-ctx.Done()
			p.waitStreamDConnectWindowLocker.Do(ctx, func() {
				if atomic.AddInt32(&p.waitStreamDConnectWindowCounter, -1) != 0 {
					return
				}
				time.Sleep(aggregationDelayBeforeNotificationEnd)
				p.statusPanelSet("(re-)connected")
				logger.Debugf(ctx, "closed the 'connecting is in progress' window")
			})
		}()

		select {
		case <-ctx.Done():
			return
		case <-time.After(aggregationDelayBeforeNotificationStart):
		}

		p.waitStreamDConnectWindowLocker.Do(ctx, func() {
			logger.Debugf(ctx, "making a 'connecting is in progress' window")
			defer logger.Debugf(ctx, "made a 'connecting is in progress' window")
			p.statusPanelSet("Connecting is in process, please wait...")
		})
	})
}

func (p *Panel) Close() error {
	var err *multierror.Error
	err = multierror.Append(err, p.eventSensor.Close())
	// TODO: remove observability.Go, Quit should be executed synchronously,
	// but there is a bug in fyne and it hangs
	observability.Go(context.TODO(), p.app.Quit)
	return err.ErrorOrNil()
}

func (p *Panel) GetStreamDConfig(ctx context.Context) (*streamdconfig.Config, error) {
	return xsync.DoR1(ctx, &p.configCacheLocker, func() *streamdconfig.Config {
		return p.configCache
	}), nil
}

func (p *Panel) SetStreamDConfig(
	ctx context.Context,
	newCfg *streamdconfig.Config,
) (_err error) {
	logger.Debugf(ctx, "SetStreamDConfig")
	defer func() { logger.Debugf(ctx, "SetStreamDConfig: %v", _err) }()

	return xsync.DoR1(ctx, &p.configCacheLocker, func() error {
		if err := p.StreamD.SetConfig(ctx, newCfg); err != nil {
			return err
		}

		p.configCache = newCfg
		return nil
	})
}

func (p *Panel) localConfigCacheUpdater(ctx context.Context) (_err error) {
	logger.Debugf(ctx, "localConfigCacheUpdater")
	defer logger.Debugf(ctx, "/localConfigCacheUpdater: %v", _err)

	cfgChangeCh, err := p.StreamD.SubscribeToConfigChanges(ctx)
	if err != nil {
		return fmt.Errorf("unable to subscribe to config changes: %w", err)
	}

	newCfg, err := p.StreamD.GetConfig(ctx)
	if err != nil {
		return fmt.Errorf("unable to get the new config: %w", err)
	}

	err = newCfg.Convert()
	if err != nil {
		return fmt.Errorf("unable to convert the config: %w", err)
	}

	observability.SecretsProviderFromCtx(ctx).(*observability.SecretsStaticProvider).ParseSecretsFrom(newCfg)
	logger.Debugf(ctx, "updated the secrets")

	p.configCacheLocker.Do(ctx, func() {
		p.configCache = newCfg
	})

	observability.Go(ctx, func() {
		logger.Debugf(ctx, "localConfigUpdaterLoop")
		defer logger.Debugf(ctx, "/localConfigUpdaterLoop")

		for {
			select {
			case <-ctx.Done():
				return
			case <-cfgChangeCh:
				newCfg, err := p.StreamD.GetConfig(ctx)
				if err != nil {
					logger.Errorf(ctx, "unable to get the new config: %v", err)
					continue
				}
				err = newCfg.Convert()
				if err != nil {
					logger.Errorf(ctx, "unable to convert the config: %v", err)
					continue
				}
				p.configCacheLocker.Do(ctx, func() {
					p.configCache = newCfg
				})
				logger.Debugf(ctx, "updated the config cache")
				observability.SecretsProviderFromCtx(ctx).(*observability.SecretsStaticProvider).ParseSecretsFrom(newCfg)
				logger.Debugf(ctx, "updated the secrets")
			}
		}
	})

	return nil
}
