package twitch

import (
	"context"

	"github.com/xaionaro-go/streamctl/pkg/oauthhandler"
)

type OAuthHandler func(context.Context, oauthhandler.OAuthHandlerArgument) error
