package ui

import (
	"context"

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
	Belt      *belt.Belt
	RestartFn func(context.Context, string)
}

var _ ui.UI = (*UI)(nil)

func NewUI(
	ctx context.Context,
	restartFn func(context.Context, string),
) *UI {
	return &UI{
		Belt:      belt.CtxBelt(ctx),
		RestartFn: restartFn,
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

func (*UI) OAuthHandlerTwitch(
	ctx context.Context,
	arg oauthhandler.OAuthHandlerArgument,
) error {
	return oauthhandler.OAuth2HandlerViaCLI(ctx, arg)
}

func (*UI) OAuthHandlerYouTube(
	ctx context.Context,
	arg oauthhandler.OAuthHandlerArgument,
) error {
	return oauthhandler.OAuth2HandlerViaCLI(ctx, arg)
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
