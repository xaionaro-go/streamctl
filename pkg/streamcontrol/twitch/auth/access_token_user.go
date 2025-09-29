package auth

import (
	"context"
	"fmt"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/secret"
	twitch "github.com/xaionaro-go/streamctl/pkg/streamcontrol/twitch/types"
)

func NewTokenByUser(
	ctx context.Context,
	client twitch.Client,
	clientCode secret.String,
) (secret.String, secret.String, error) {
	logger.Debugf(ctx, "getNewTokenByUser")
	defer func() { logger.Debugf(ctx, "/getNewTokenByUser") }()

	if clientCode.Get() == "" {
		return secret.New(""), secret.New(""), fmt.Errorf("internal error: ClientCode is empty")
	}

	logger.Debugf(ctx, "requesting user access token...")
	resp, err := client.RequestUserAccessToken(clientCode.Get())
	if observability.IsOnInsecureDebug(ctx) {
		logger.Debugf(ctx, "requesting user access token result: %#+v %v", resp, err)
	}
	if err != nil {
		return secret.New(""), secret.New(""), fmt.Errorf("unable to get user access token: %w", err)
	}
	if resp.ErrorStatus != 0 {
		return secret.New(""), secret.New(""), fmt.Errorf(
			"unable to query: %d %v: %v",
			resp.ErrorStatus,
			resp.Error,
			resp.ErrorMessage,
		)
	}

	return secret.New(resp.Data.AccessToken), secret.New(resp.Data.RefreshToken), nil
}
