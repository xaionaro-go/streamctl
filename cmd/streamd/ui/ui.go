package ui

import (
	"context"
	"fmt"
	"time"

	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/oauthhandler"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/kick"
	obs "github.com/xaionaro-go/streamctl/pkg/streamcontrol/obs/types"
	twitch "github.com/xaionaro-go/streamctl/pkg/streamcontrol/twitch/types"
	youtube "github.com/xaionaro-go/streamctl/pkg/streamcontrol/youtube/types"
	"github.com/xaionaro-go/streamctl/pkg/streamd/ui"
	"github.com/xaionaro-go/xsync"
)

type UI struct {
	OpenBrowserFn         func(context.Context, string) error
	OAuthURLOpenFn        func(listenPort uint16, platID streamcontrol.PlatformName, authURL string) bool
	Belt                  *belt.Belt
	RestartFn             func(context.Context, string)
	CodeChMap             map[streamcontrol.PlatformName]chan string
	CodeChMapLocker       xsync.Mutex
	SetLoggingLevelFn     func(context.Context, logger.Level)
	InputTwitchUserInfoFn func(
		ctx context.Context,
		cfg *streamcontrol.PlatformConfig[twitch.PlatformSpecificConfig, twitch.StreamProfile],
	) (bool, error)
	InputKickUserInfoFn func(
		ctx context.Context,
		cfg *streamcontrol.PlatformConfig[kick.PlatformSpecificConfig, kick.StreamProfile],
	) (bool, error)
	InputYouTubeUserInfoFn func(
		ctx context.Context,
		cfg *streamcontrol.PlatformConfig[youtube.PlatformSpecificConfig, youtube.StreamProfile],
	) (bool, error)
	InputOBSConnectInfoFn func(
		ctx context.Context,
		cfg *streamcontrol.PlatformConfig[obs.PlatformSpecificConfig, obs.StreamProfile],
	) (bool, error)
}

var _ ui.UI = (*UI)(nil)

func NewUI(
	ctx context.Context,
	openBrowserFn func(context.Context, string) error,
	oauthURLOpener func(listenPort uint16, platID streamcontrol.PlatformName, authURL string) bool,
	restartFn func(context.Context, string),
	setLoggingLevel func(context.Context, logger.Level),
) *UI {
	return &UI{
		OpenBrowserFn:     openBrowserFn,
		OAuthURLOpenFn:    oauthURLOpener,
		Belt:              belt.CtxBelt(ctx),
		RestartFn:         restartFn,
		CodeChMap:         map[streamcontrol.PlatformName]chan string{},
		CodeChMapLocker:   xsync.RWMutex{},
		SetLoggingLevelFn: setLoggingLevel,
	}
}

func (ui *UI) OpenBrowser(ctx context.Context, url string) error {
	logger.Debugf(ctx, "UI.OpenBrowser(ctx, '%s')", url)
	defer logger.Debugf(ctx, "/UI.OpenBrowser(ctx, '%s')", url)
	return ui.OpenBrowserFn(ctx, url)
}

func (ui *UI) SetLoggingLevel(ctx context.Context, level logger.Level) {
	ui.SetLoggingLevelFn(ctx, level)
}

func (ui *UI) SetStatus(msg string) {
	logger.FromBelt(ui.Belt).Infof("status: %s", msg)
}

func (ui *UI) DisplayError(err error) {
	logger.FromBelt(ui.Belt).Errorf("error: %v", err)
}

func (ui *UI) Restart(ctx context.Context, msg string) {
	ui.RestartFn(ctx, msg)
}

func (*UI) InputGitUserData(
	ctx context.Context,
) (bool, string, []byte, error) {
	return false, "", nil, nil
}

func (ui *UI) newOAuthCodeReceiver(
	ctx context.Context,
	platID streamcontrol.PlatformName,
) (<-chan string, context.CancelFunc) {
	return xsync.DoR2(ctx, &ui.CodeChMapLocker, func() (<-chan string, context.CancelFunc) {
		return ui.newOAuthCodeReceiverNoLock(ctx, platID)
	})
}

func (ui *UI) newOAuthCodeReceiverNoLock(
	ctx context.Context,
	platID streamcontrol.PlatformName,
) (<-chan string, context.CancelFunc) {
	if oldCh, ok := ui.CodeChMap[platID]; ok {
		return oldCh, nil
	}

	ch := make(chan string)
	ui.CodeChMap[platID] = ch

	return ch, func() {
		ui.CodeChMapLocker.Do(ctx, func() {
			delete(ui.CodeChMap, platID)
		})
	}
}

func (ui *UI) getOAuthCodeReceiver(
	ctx context.Context,
	platID streamcontrol.PlatformName,
) chan<- string {
	return xsync.DoR1(ctx, &ui.CodeChMapLocker, func() chan<- string {
		return ui.CodeChMap[platID]
	})
}

func (ui *UI) oauth2Handler(
	ctx context.Context,
	platID streamcontrol.PlatformName,
	arg oauthhandler.OAuthHandlerArgument,
) error {
	logger.Debugf(ctx, "oauth2Handler(ctx, '%s', %#+v)", platID, arg)
	defer logger.Debugf(ctx, "/oauth2Handler(ctx, '%s', %#+v)", platID, arg)

	ctx, cancelFn := context.WithCancel(ctx)
	defer func() {
		logger.Debugf(ctx, "cancelling the context")
		cancelFn()
	}()

	codeCh, removeReceiver := ui.newOAuthCodeReceiver(ctx, platID)
	if codeCh == nil {
		return fmt.Errorf("there is already another oauth handler for this platform running")
	}
	if removeReceiver != nil {
		defer removeReceiver()
	}

	logger.Debugf(
		ctx,
		"asking to open the URL '%s' using listen port %d for platform '%s'",
		arg.AuthURL,
		arg.ListenPort,
		platID,
	)
	ui.OAuthURLOpenFn(arg.ListenPort, platID, arg.AuthURL)

	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for {
		logger.Debugf(ctx, "waiting for an auth code")
		select {
		case <-ctx.Done():
			return fmt.Errorf("oauth2Handler is cancelled: %w", ctx.Err())
		case code, ok := <-codeCh:
			if !ok {
				return fmt.Errorf("internal error: codeCh is closed in oauth2Handler")
			}
			if code == "" {
				return fmt.Errorf("internal error: code is empty in oauth2Handler")
			}
			err := arg.ExchangeFn(code)
			if err != nil {
				return fmt.Errorf("ExchangeFn returned an error: %w", err)
			}
			return nil
		case <-t.C:
			logger.Debugf(ctx, "re-asking to open the URL: %s", arg.AuthURL)
			ui.OAuthURLOpenFn(arg.ListenPort, platID, arg.AuthURL)
		}
	}
}

func (ui *UI) OnSubmittedOAuthCode(
	ctx context.Context,
	platID streamcontrol.PlatformName,
	code string,
) error {
	if code == "" {
		return fmt.Errorf("code is empty")
	}

	codeCh := ui.getOAuthCodeReceiver(ctx, platID)
	if codeCh == nil {
		logger.Debugf(ctx, "no code receiver for '%s'", platID)
		return nil
	}

	codeCh <- code
	return nil
}

func (ui *UI) OAuthHandlerTwitch(
	ctx context.Context,
	arg oauthhandler.OAuthHandlerArgument,
) error {
	return ui.oauth2Handler(ctx, twitch.ID, arg)
}

func (ui *UI) OAuthHandlerKick(
	ctx context.Context,
	arg oauthhandler.OAuthHandlerArgument,
) error {
	return ui.oauth2Handler(ctx, kick.ID, arg)
}

func (ui *UI) OAuthHandlerYouTube(
	ctx context.Context,
	arg oauthhandler.OAuthHandlerArgument,
) error {
	return ui.oauth2Handler(ctx, youtube.ID, arg)
}
