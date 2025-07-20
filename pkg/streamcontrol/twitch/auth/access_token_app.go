package auth

import (
	"context"
	"fmt"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/secret"
	twitch "github.com/xaionaro-go/streamctl/pkg/streamcontrol/twitch/types"
)

func NewTokenByApp(
	ctx context.Context,
	client twitch.Client,
) (secret.String, error) {
	logger.Debugf(ctx, "getNewTokenByApp")
	defer func() { logger.Debugf(ctx, "/getNewTokenByApp") }()

	resp, err := client.RequestAppAccessToken(nil)
	if err != nil {
		return secret.New(""), fmt.Errorf("unable to get app access token: %w", err)
	}

	if resp.ErrorStatus != 0 {
		return secret.New(""), fmt.Errorf(
			"unable to get app access token (the response contains an error): %d %v: %v",
			resp.ErrorStatus,
			resp.Error,
			resp.ErrorMessage,
		)
	}

	return secret.New(resp.Data.AccessToken), nil
}
