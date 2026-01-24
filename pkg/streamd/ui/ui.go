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
	OAuthHandler(ctx context.Context, platID streamcontrol.PlatformID, arg oauthhandler.OAuthHandlerArgument) error
	OpenBrowser(ctx context.Context, url string) error
	OnSubmittedOAuthCode(
		ctx context.Context,
		platID streamcontrol.PlatformID,
		code string,
	) error
	SetLoggingLevel(
		ctx context.Context,
		level logger.Level,
	)
}
