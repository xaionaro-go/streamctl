package twitch

import (
	"context"
	"fmt"
	"time"

	"github.com/facebookincubator/go-belt/tool/experimental/errmon"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/hashicorp/go-multierror"
	"github.com/nicklaw5/helix/v2"
	"github.com/xaionaro-go/streamctl/pkg/oauthhandler"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
)

type Twitch struct {
	client        *helix.Client
	broadcasterID string
}

var _ streamcontrol.StreamController[StreamProfile] = (*Twitch)(nil)

func New(
	ctx context.Context,
	cfg Config,
	safeCfgFn func(Config) error,
) (*Twitch, error) {
	if cfg.Config.Channel == "" {
		return nil, fmt.Errorf("'channel' is not set")
	}
	if cfg.Config.ClientID == "" || cfg.Config.ClientSecret == "" {
		return nil, fmt.Errorf("'clientid' or/and 'clientsecret' is/are not set; go to https://dev.twitch.tv/console/apps/create and create an app if it not created, yet")
	}
	client, err := getClient(ctx, cfg, safeCfgFn)
	if err != nil {
		return nil, err
	}
	logger.Debugf(ctx, "initialized a client")
	broadcasterID, err := getUserID(ctx, client, cfg.Config.Channel)
	if err != nil {
		return nil, fmt.Errorf("unable to get the user ID: %w", err)
	}
	logger.Debugf(ctx, "broadcaster_id: %s (login: %s)", broadcasterID, cfg.Config.Channel)
	return &Twitch{
		client:        client,
		broadcasterID: broadcasterID,
	}, nil
}

func getUserID(
	_ context.Context,
	client *helix.Client,
	login string,
) (string, error) {
	resp, err := client.GetUsers(&helix.UsersParams{
		Logins: []string{login},
	})
	if err != nil {
		return "", fmt.Errorf("unable to query user info: %w", err)
	}
	if len(resp.Data.Users) != 1 {
		return "", fmt.Errorf("expected 1 user with login, but received %d users", len(resp.Data.Users))
	}
	return resp.Data.Users[0].ID, nil
}

func (t *Twitch) editChannelInfo(
	ctx context.Context,
	params *helix.EditChannelInformationParams,
) error {
	if params == nil {
		return fmt.Errorf("params == nil")
	}
	params.BroadcasterID = t.broadcasterID
	resp, err := t.client.EditChannelInformation(params)
	if err != nil {
		return fmt.Errorf("unable to update the channel info (%#+v): %w", *params, err)
	}
	if resp.ErrorStatus != 0 {
		return fmt.Errorf("unable to update the channel info (%#+v), the response reported an error: %d %v: %v", *params, resp.ErrorStatus, resp.Error, resp.ErrorMessage)
	}
	logger.Debugf(ctx, "success")
	return nil
}

type SaveProfileHandler interface {
	SaveProfile(context.Context, StreamProfile) error
}

func (t *Twitch) ApplyProfile(
	ctx context.Context,
	profile StreamProfile,
	customArgs ...any,
) error {
	if profile.CategoryName != nil {
		if profile.CategoryID != nil {
			logger.Warnf(ctx, "both category name and ID are set; these are contradicting stream profile settings; prioritizing the name")
		}
		categoryID, err := t.getCategoryID(*profile.CategoryName)
		if err == nil {
			profile.CategoryID = &categoryID
			profile.CategoryName = nil
			saveProfile(ctx, profile, customArgs...)
		} else {
			logger.Errorf(ctx, "unable to get the category ID: %v", err)
		}
	}
	params := &helix.EditChannelInformationParams{
		Tags: profile.Tags,
	}
	if profile.Language != nil {
		params.BroadcasterLanguage = *profile.Language
	}
	if profile.CategoryID != nil {
		params.GameID = *profile.CategoryID
	}
	return t.editChannelInfo(ctx, params)
}

func saveProfile(ctx context.Context, profile StreamProfile, customArgs ...any) {
	for _, arg := range customArgs {
		saver, ok := arg.(SaveProfileHandler)
		if !ok {
			continue
		}
		if err := saver.SaveProfile(ctx, profile); err != nil {
			logger.Errorf(ctx, "unable to save profile: %v: %#+v", err, profile)
		}
	}
}

func (t *Twitch) getCategoryID(
	categoryName string,
) (string, error) {
	resp, err := t.client.GetGames(&helix.GamesParams{
		Names: []string{categoryName},
	})
	if err != nil {
		return "", fmt.Errorf("unable to query the category info (of name '%s'): %w", categoryName, err)
	}

	if len(resp.Data.Games) != 1 {
		return "", fmt.Errorf("expected exactly 1 result, but received %d", len(resp.Data.Games))
	}
	categoryInfo := resp.Data.Games[0]

	return categoryInfo.ID, nil
}

func (t *Twitch) SetTitle(
	ctx context.Context,
	title string,
) error {
	return t.editChannelInfo(ctx, &helix.EditChannelInformationParams{
		Title: title,
	})
}

func (t *Twitch) SetDescription(
	ctx context.Context,
	description string,
) error {
	// Twitch streams has no description:
	return nil
}

func (t *Twitch) InsertAdsCuePoint(
	ctx context.Context,
	ts time.Time,
	duration time.Duration,
) error {
	// Unfortunately, we do not support sending ads cues.
	// So nothing to do here:
	return nil
}

func (t *Twitch) Flush(
	ctx context.Context,
) error {
	// Unfortunately, we do not support sending accumulated changes, and we change things immediately right away.
	// So nothing to do here:
	return nil
}

func (t *Twitch) StartStream(
	ctx context.Context,
	title string,
	description string,
	profile StreamProfile,
	customArgs ...any,
) error {
	return multierror.Append(
		t.SetTitle(ctx, title),
		t.SetDescription(ctx, description),
		t.ApplyProfile(ctx, profile, customArgs...),
	).ErrorOrNil()
}

func (t *Twitch) EndStream(
	ctx context.Context,
) error {
	// Twitch ends a stream automatically, nothing to do:
	return nil
}

func getClient(
	ctx context.Context,
	cfg Config,
	safeCfgFn func(Config) error,
) (*helix.Client, error) {
	client, err := helix.NewClient(&helix.Options{
		ClientID:     cfg.Config.ClientID,
		ClientSecret: cfg.Config.ClientSecret,
		RedirectURI:  "http://localhost/",
	})
	if err != nil {
		return nil, fmt.Errorf("unable to create a helix client object: %w", err)
	}
	client.OnUserAccessTokenRefreshed(func(newAccessToken, newRefreshToken string) {
		logger.Debugf(ctx, "updated tokens")
		cfg.Config.UserAccessToken = newAccessToken
		cfg.Config.RefreshToken = newRefreshToken
		err := safeCfgFn(cfg)
		errmon.ObserveErrorCtx(ctx, err)
	})

	switch cfg.Config.AuthType {
	case "user":
		client.SetUserAccessToken(cfg.Config.UserAccessToken)
		client.SetRefreshToken(cfg.Config.RefreshToken)

		if cfg.Config.UserAccessToken == "" {
			if cfg.Config.ClientCode == "" {
				authURL := client.GetAuthorizationURL(&helix.AuthorizationURLParams{
					ResponseType: "code", // or "token"
					Scopes:       []string{"channel:manage:broadcast"},
				})

				oauthHandler := oauthhandler.NewOAuth2Handler(authURL, func(code string) error {
					cfg.Config.ClientCode = code
					err = safeCfgFn(cfg)
					errmon.ObserveErrorCtx(ctx, err)
					return nil
				}, "")
				err := oauthHandler.Handle(ctx)
				if err != nil {
					return nil, fmt.Errorf("unable to get or exchange the oauth code to a token: %w", err)
				}
			}

			resp, err := client.RequestUserAccessToken(cfg.Config.ClientCode)
			if err != nil {
				return nil, fmt.Errorf("unable to get user access token: %w", err)
			}
			if resp.ErrorStatus != 0 {
				return nil, fmt.Errorf("unable to query: %d %v: %v", resp.ErrorStatus, resp.Error, resp.ErrorMessage)
			}
			client.SetUserAccessToken(resp.Data.AccessToken)
			client.SetRefreshToken(resp.Data.RefreshToken)
			cfg.Config.ClientCode = ""
			cfg.Config.UserAccessToken = resp.Data.AccessToken
			cfg.Config.RefreshToken = resp.Data.RefreshToken
			err = safeCfgFn(cfg)
			errmon.ObserveErrorCtx(ctx, err)
		}
	case "app":
		if cfg.Config.AppAccessToken != "" {
			logger.Debugf(ctx, "already have an app access token")
			client.SetUserAccessToken(cfg.Config.AppAccessToken) // shouldn't it be "SetAppAccessToken"?
			break
		}
		logger.Debugf(ctx, "do not have an app access token")

		resp, err := client.RequestAppAccessToken(nil)
		if err != nil {
			return nil, fmt.Errorf("unable to get app access token: %w", err)
		}
		if resp.ErrorStatus != 0 {
			return nil, fmt.Errorf("unable to get app access token (the response contains an error): %d %v: %v", resp.ErrorStatus, resp.Error, resp.ErrorMessage)
		}
		logger.Debugf(ctx, "setting the app access token")
		client.SetAppAccessToken(resp.Data.AccessToken)
		cfg.Config.AppAccessToken = resp.Data.AccessToken
		err = safeCfgFn(cfg)
		errmon.ObserveErrorCtx(ctx, err)
	default:
		return nil, fmt.Errorf("invalid AuthType: <%s>", cfg.Config.AuthType)
	}

	return client, nil
}

func (t *Twitch) GetAllCategories(
	ctx context.Context,
) ([]helix.Game, error) {
	var allCategories []helix.Game
	var pagination *helix.Pagination
	for {
		params := &helix.TopGamesParams{
			First: 100,
		}
		if pagination != nil {
			params.After = pagination.Cursor
		}

		resp, err := t.client.GetTopGames(params)
		if err != nil {
			return nil, fmt.Errorf("unable to get the list of games: %w", err)
		}

		if len(resp.Data.Games) == 0 {
			break
		}

		allCategories = append(allCategories, resp.Data.Games...)
		pagination = &resp.Data.Pagination
	}
	return allCategories, nil
}
