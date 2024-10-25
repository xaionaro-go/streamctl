package ui

import (
	"context"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/oauthhandler"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
)

type UI interface {
	SetStatus(string)
	DisplayError(error)
	OAuthHandlerTwitch(ctx context.Context, arg oauthhandler.OAuthHandlerArgument) error
	OAuthHandlerKick(ctx context.Context, arg oauthhandler.OAuthHandlerArgument) error
	OAuthHandlerYouTube(ctx context.Context, arg oauthhandler.OAuthHandlerArgument) error
	OpenBrowser(ctx context.Context, url string) error
	OnSubmittedOAuthCode(
		ctx context.Context,
		platID streamcontrol.PlatformName,
		code string,
	) error
	SetLoggingLevel(
		ctx context.Context,
		level logger.Level,
	)
}
