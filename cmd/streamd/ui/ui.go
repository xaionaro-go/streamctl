package ui

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/oauthhandler"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	obs "github.com/xaionaro-go/streamctl/pkg/streamcontrol/obs/types"
	twitch "github.com/xaionaro-go/streamctl/pkg/streamcontrol/twitch/types"
	youtube "github.com/xaionaro-go/streamctl/pkg/streamcontrol/youtube/types"
	streamd "github.com/xaionaro-go/streamctl/pkg/streamd/types"
	"github.com/xaionaro-go/streamctl/pkg/streamd/ui"
)

type UI struct {
	OAuthURLOpenFn  func(listenPort uint16, platID streamcontrol.PlatformName, authURL string) bool
	Belt            *belt.Belt
	RestartFn       func(context.Context, string)
	CodeChMap       map[streamcontrol.PlatformName]chan string
	CodeChMapLocker sync.Mutex
}

var _ ui.UI = (*UI)(nil)

func NewUI(
	ctx context.Context,
	oauthURLOpener func(listenPort uint16, platID streamcontrol.PlatformName, authURL string) bool,
	restartFn func(context.Context, string),
) *UI {
	return &UI{
		OAuthURLOpenFn: oauthURLOpener,
		Belt:           belt.CtxBelt(ctx),
		RestartFn:      restartFn,
		CodeChMap:      map[streamcontrol.PlatformName]chan string{},
	}
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
	_ context.Context,
	platID streamcontrol.PlatformName,
) (<-chan string, context.CancelFunc) {
	ui.CodeChMapLocker.Lock()
	defer ui.CodeChMapLocker.Unlock()

	if oldCh, ok := ui.CodeChMap[platID]; ok {
		return oldCh, nil
	}

	ch := make(chan string)
	ui.CodeChMap[platID] = ch

	return ch, func() {
		ui.CodeChMapLocker.Lock()
		defer ui.CodeChMapLocker.Unlock()
		delete(ui.CodeChMap, platID)
	}
}

func (ui *UI) getOAuthCodeReceiver(
	_ context.Context,
	platID streamcontrol.PlatformName,
) chan<- string {
	ui.CodeChMapLocker.Lock()
	defer ui.CodeChMapLocker.Unlock()

	return ui.CodeChMap[platID]
}

func (ui *UI) oauth2Handler(
	ctx context.Context,
	platID streamcontrol.PlatformName,
	arg oauthhandler.OAuthHandlerArgument,
) error {
	ctx, cancelFn := context.WithCancel(ctx)
	defer cancelFn()

	codeCh, removeReceiver := ui.newOAuthCodeReceiver(ctx, platID)
	if codeCh == nil {
		return fmt.Errorf("there is already another oauth handler for this platform running")
	}
	if removeReceiver != nil {
		defer removeReceiver()
	}

	logger.Debugf(ctx, "asking to open the URL: %s", arg.AuthURL)
	ui.OAuthURLOpenFn(arg.ListenPort, platID, arg.AuthURL)

	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for {
		logger.Debugf(ctx, "waiting for an auth code")
		select {
		case <-ctx.Done():
			return ctx.Err()
		case code := <-codeCh:
			return arg.ExchangeFn(code)
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

func (ui *UI) OAuthHandlerYouTube(
	ctx context.Context,
	arg oauthhandler.OAuthHandlerArgument,
) error {
	return ui.oauth2Handler(ctx, youtube.ID, arg)
}

func (*UI) InputTwitchUserInfo(
	ctx context.Context,
	cfg *streamcontrol.PlatformConfig[twitch.PlatformSpecificConfig, twitch.StreamProfile],
) (bool, error) {
	return false, streamd.ErrSkipBackend
}

func (*UI) InputYouTubeUserInfo(
	ctx context.Context,
	cfg *streamcontrol.PlatformConfig[youtube.PlatformSpecificConfig, youtube.StreamProfile],
) (bool, error) {
	return false, streamd.ErrSkipBackend
}

func (*UI) InputOBSConnectInfo(
	ctx context.Context,
	cfg *streamcontrol.PlatformConfig[obs.PlatformSpecificConfig, obs.StreamProfile],
) (bool, error) {
	return false, streamd.ErrSkipBackend
}
