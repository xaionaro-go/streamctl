package ui

import (
	"context"

	"github.com/xaionaro-go/streamctl/pkg/oauthhandler"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	obs "github.com/xaionaro-go/streamctl/pkg/streamcontrol/obs/types"
	twitch "github.com/xaionaro-go/streamctl/pkg/streamcontrol/twitch/types"
	youtube "github.com/xaionaro-go/streamctl/pkg/streamcontrol/youtube/types"
)

type UI interface {
	SetStatus(string)
	DisplayError(error)
	Restart(context.Context, string)
	InputGitUserData(
		ctx context.Context,
	) (bool, string, []byte, error)
	OAuthHandlerTwitch(ctx context.Context, arg oauthhandler.OAuthHandlerArgument) error
	OAuthHandlerYouTube(ctx context.Context, arg oauthhandler.OAuthHandlerArgument) error
	InputTwitchUserInfo(
		ctx context.Context,
		cfg *streamcontrol.PlatformConfig[twitch.PlatformSpecificConfig, twitch.StreamProfile],
	) (bool, error)
	InputYouTubeUserInfo(
		ctx context.Context,
		cfg *streamcontrol.PlatformConfig[youtube.PlatformSpecificConfig, youtube.StreamProfile],
	) (bool, error)
	InputOBSConnectInfo(
		ctx context.Context,
		cfg *streamcontrol.PlatformConfig[obs.PlatformSpecificConfig, obs.StreamProfile],
	) (bool, error)
	OnSubmittedOAuthCode(
		ctx context.Context,
		platID streamcontrol.PlatformName,
		code string,
	) error
}
