package twitch

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/facebookincubator/go-belt/tool/experimental/errmon"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/hashicorp/go-multierror"
	"github.com/nicklaw5/helix/v2"
	"github.com/xaionaro-go/streamctl/pkg/oauthhandler"
	"github.com/xaionaro-go/streamctl/pkg/observability"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
)

type Twitch struct {
	client        *helix.Client
	config        Config
	broadcasterID string
	lazyInitOnce  sync.Once
	saveCfgFn     func(Config) error
	tokenLocker   sync.Mutex
}

var _ streamcontrol.StreamController[StreamProfile] = (*Twitch)(nil)

func New(
	ctx context.Context,
	cfg Config,
	saveCfgFn func(Config) error,
) (*Twitch, error) {
	if cfg.Config.Channel == "" {
		return nil, fmt.Errorf("'channel' is not set")
	}
	if cfg.Config.ClientID == "" || cfg.Config.ClientSecret == "" {
		return nil, fmt.Errorf("'clientid' or/and 'clientsecret' is/are not set; go to https://dev.twitch.tv/console/apps/create and create an app if it not created, yet")
	}
	client, err := getClient(ctx, cfg, saveCfgFn)
	if err != nil {
		return nil, err
	}
	t := &Twitch{
		client:    client,
		config:    cfg,
		saveCfgFn: saveCfgFn,
	}
	logger.Debugf(ctx, "initialized a client")
	return t, nil
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

func (t *Twitch) prepare(ctx context.Context) error {
	err := t.getTokenIfNeeded(ctx)
	if err != nil {
		return err
	}

	t.lazyInitOnce.Do(func() {
		if t.broadcasterID != "" {
			return
		}
		t.broadcasterID, err = getUserID(ctx, t.client, t.config.Config.Channel)
		if err != nil {
			return
		}
		logger.Debugf(ctx, "broadcaster_id: %s (login: %s)", t.broadcasterID, t.config.Config.Channel)
	})
	return err
}

func (t *Twitch) Close() error {
	return nil
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

func removeNonAlphanumeric(input string) string {
	var builder strings.Builder
	for _, r := range input {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			builder.WriteRune(r)
		}
	}
	return builder.String()
}

// truncateStringByByteLength
func truncateStringByByteLength(input string, byteLength int) string {
	byteSlice := []byte(input)

	if len(byteSlice) <= byteLength {
		return input
	}

	truncationPoint := byteLength
	for !utf8.Valid(byteSlice[:truncationPoint]) {
		truncationPoint--
	}

	return string(byteSlice[:truncationPoint])
}

func (t *Twitch) ApplyProfile(
	ctx context.Context,
	profile StreamProfile,
	customArgs ...any,
) error {
	t.prepare(ctx)

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

	tags := make([]string, 0, len(profile.Tags))
	for _, tag := range profile.Tags {
		tag = removeNonAlphanumeric(tag)
		if tag == "" {
			continue
		}
		tag = truncateStringByByteLength(tag, 25) // see also: https://github.com/twitchdev/issues/issues/789
		tags = append(tags, tag)
	}

	params := &helix.EditChannelInformationParams{
		Tags: tags,
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
	t.prepare(ctx)
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
	t.prepare(ctx)
	var result error
	if err := t.SetTitle(ctx, title); err != nil {
		result = multierror.Append(result, fmt.Errorf("unable to set title: %w", err))
	}
	if err := t.SetDescription(ctx, description); err != nil {
		result = multierror.Append(result, fmt.Errorf("unable to set description: %w", err))
	}
	if err := t.ApplyProfile(ctx, profile, customArgs...); err != nil {
		result = multierror.Append(result, fmt.Errorf("unable to apply the stream-specific profile: %w", err))
	}
	return multierror.Append(result).ErrorOrNil()
}

func (t *Twitch) EndStream(
	ctx context.Context,
) error {
	// Twitch ends a stream automatically, nothing to do:
	return nil
}

func (t *Twitch) GetStreamStatus(
	ctx context.Context,
) (*streamcontrol.StreamStatus, error) {
	t.prepare(ctx)
	// Twitch ends a stream automatically, nothing to do:
	reply, err := t.client.GetStreams(&helix.StreamsParams{
		UserIDs: []string{t.broadcasterID},
	})
	if err != nil {
		return nil, fmt.Errorf("unable to request streams: %w", err)
	}

	if len(reply.Data.Streams) == 0 {
		return &streamcontrol.StreamStatus{
			IsActive: false,
		}, nil
	}

	if len(reply.Data.Streams) > 1 {
		return nil, fmt.Errorf("received %d streams instead of 1", len(reply.Data.Streams))
	}
	stream := reply.Data.Streams[0]

	return &streamcontrol.StreamStatus{
		IsActive:  true,
		StartedAt: &stream.StartedAt,
	}, nil
}

func (t *Twitch) getTokenIfNeeded(
	ctx context.Context,
) error {
	switch t.config.Config.AuthType {
	case "user":
		t.tokenLocker.Lock()
		t.client.SetUserAccessToken(t.config.Config.UserAccessToken)
		t.client.SetRefreshToken(t.config.Config.RefreshToken)
		t.tokenLocker.Unlock()
		if t.config.Config.UserAccessToken != "" {
			return nil
		}
	case "app":
		t.tokenLocker.Lock()
		t.client.SetUserAccessToken(t.config.Config.AppAccessToken) // shouldn't it be "SetAppAccessToken"?
		t.tokenLocker.Unlock()
		if t.config.Config.AppAccessToken != "" {
			logger.Debugf(ctx, "already have an app access token")
			return nil
		}
		logger.Debugf(ctx, "do not have an app access token")
	}

	return t.getNewToken(ctx)
}

func (t *Twitch) getNewToken(
	ctx context.Context,
) error {
	t.tokenLocker.Lock()
	defer t.tokenLocker.Unlock()

	switch t.config.Config.AuthType {
	case "user":
		if t.config.Config.ClientCode == "" {
			getPortsFn := t.config.Config.GetOAuthListenPorts
			if getPortsFn == nil {
				return fmt.Errorf("the function GetOAuthListenPorts is not set")
			}

			oauthHandler := t.config.Config.CustomOAuthHandler
			if oauthHandler == nil {
				oauthHandler = oauthhandler.OAuth2HandlerViaCLI
			}

			ctx, cancelFunc := context.WithCancel(ctx)

			var errWg sync.WaitGroup
			var resultErr error
			errCh := make(chan error)
			errWg.Add(1)
			observability.Go(ctx, func() {
				errWg.Done()
				for err := range errCh {
					errmon.ObserveErrorCtx(ctx, err)
					resultErr = multierror.Append(resultErr, err)
				}
			})

			alreadyListening := map[uint16]struct{}{}
			var wg sync.WaitGroup
			success := false

			startHandlerForPort := func(listenPort uint16) {
				if _, ok := alreadyListening[listenPort]; ok {
					return
				}
				logger.Tracef(ctx, "starting the oauth handler at port %d", listenPort)
				wg.Add(1)
				go func(listenPort uint16) {
					defer wg.Done()
					authURL := GetAuthorizationURL(
						&helix.AuthorizationURLParams{
							ResponseType: "code", // or "token"
							Scopes:       []string{"channel:manage:broadcast"},
						},
						t.config.Config.ClientID,
						fmt.Sprintf("127.0.0.1:%d", listenPort),
					)

					arg := oauthhandler.OAuthHandlerArgument{
						AuthURL: authURL,
						ExchangeFn: func(code string) error {
							t.config.Config.ClientCode = code
							err := t.saveCfgFn(t.config)
							errmon.ObserveErrorCtx(ctx, err)
							return nil
						},
					}

					err := oauthHandler(ctx, arg)
					if err != nil {
						errCh <- fmt.Errorf("unable to get or exchange the oauth code to a token: %w", err)
						return
					}
					cancelFunc()
					success = true
				}(listenPort)
			}

			for _, listenPort := range getPortsFn() {
				startHandlerForPort(listenPort)
			}

			wg.Add(1)
			observability.Go(ctx, func() {
				defer wg.Done()
				t := time.NewTicker(time.Second)
				for {
					select {
					case <-ctx.Done():
						return
					case <-t.C:
					}
					ports := getPortsFn()
					logger.Tracef(ctx, "oauth listener ports: %#+v", ports)

					alreadyListeningNext := map[uint16]struct{}{}
					for _, listenPort := range ports {
						startHandlerForPort(listenPort)
						alreadyListeningNext[listenPort] = struct{}{}
					}
					alreadyListening = alreadyListeningNext
				}
			})

			observability.Go(ctx, func() {
				wg.Wait()
				close(errCh)
			})
			<-ctx.Done()
			if !success {
				errWg.Wait()
				return resultErr
			}
		}

		resp, err := t.client.RequestUserAccessToken(t.config.Config.ClientCode)
		if err != nil {
			return fmt.Errorf("unable to get user access token: %w", err)
		}
		if resp.ErrorStatus != 0 {
			return fmt.Errorf("unable to query: %d %v: %v", resp.ErrorStatus, resp.Error, resp.ErrorMessage)
		}
		t.client.SetUserAccessToken(resp.Data.AccessToken)
		t.client.SetRefreshToken(resp.Data.RefreshToken)
		t.config.Config.ClientCode = ""
		t.config.Config.UserAccessToken = resp.Data.AccessToken
		t.config.Config.RefreshToken = resp.Data.RefreshToken
		err = t.saveCfgFn(t.config)
		errmon.ObserveErrorCtx(ctx, err)
	case "app":
		resp, err := t.client.RequestAppAccessToken(nil)
		if err != nil {
			return fmt.Errorf("unable to get app access token: %w", err)
		}
		if resp.ErrorStatus != 0 {
			return fmt.Errorf("unable to get app access token (the response contains an error): %d %v: %v", resp.ErrorStatus, resp.Error, resp.ErrorMessage)
		}
		logger.Debugf(ctx, "setting the app access token")
		t.client.SetAppAccessToken(resp.Data.AccessToken)
		t.config.Config.AppAccessToken = resp.Data.AccessToken
		err = t.saveCfgFn(t.config)
		errmon.ObserveErrorCtx(ctx, err)
	default:
		return fmt.Errorf("invalid AuthType: <%s>", t.config.Config.AuthType)
	}

	return nil
}

func getClient(
	ctx context.Context,
	cfg Config,
	saveCfgFn func(Config) error,
) (*helix.Client, error) {
	options := &helix.Options{
		ClientID:     cfg.Config.ClientID,
		ClientSecret: cfg.Config.ClientSecret,
	}
	client, err := helix.NewClient(options)
	if err != nil {
		return nil, fmt.Errorf("unable to create a helix client object: %w", err)
	}
	client.OnUserAccessTokenRefreshed(func(newAccessToken, newRefreshToken string) {
		logger.Debugf(ctx, "updated tokens")
		cfg.Config.UserAccessToken = newAccessToken
		cfg.Config.RefreshToken = newRefreshToken
		err := saveCfgFn(cfg)
		errmon.ObserveErrorCtx(ctx, err)
	})
	return client, nil
}

func GetAuthorizationURL(
	params *helix.AuthorizationURLParams,
	clientID string,
	redirectURI string,
) string {
	url := helix.AuthBaseURL + "/authorize"
	url += "?response_type=" + params.ResponseType
	url += "&client_id=" + clientID
	url += "&redirect_uri=" + redirectURI

	if params.State != "" {
		url += "&state=" + params.State
	}

	if params.ForceVerify {
		url += "&force_verify=true"
	}

	if len(params.Scopes) != 0 {
		url += "&scope=" + strings.Join(params.Scopes, "%20")
	}

	return url
}

func (t *Twitch) GetAllCategories(
	ctx context.Context,
) ([]helix.Game, error) {
	t.prepare(ctx)
	categoriesMap := map[string]helix.Game{}
	var pagination *helix.Pagination
	for {
		params := &helix.TopGamesParams{
			First: 100,
		}
		if pagination != nil {
			params.After = pagination.Cursor
		}

		logger.FromCtx(ctx).Tracef("requesting top games with params: %#+v", *params)
		resp, err := t.client.GetTopGames(params)
		logger.FromCtx(ctx).Tracef("requesting top games result: e:%v; resp:%#+v", err, resp)
		if err != nil {
			return nil, fmt.Errorf("unable to get the list of games: %w", err)
		}

		if len(resp.Data.Games) == 0 {
			break
		}

		newCategoriesCount := 0
		for _, g := range resp.Data.Games {
			if oldG, ok := categoriesMap[g.ID]; ok {
				logger.Tracef(ctx, "got a duplicate game: %#+v == %#+v", g, oldG)
				continue
			}
			categoriesMap[g.ID] = g
			newCategoriesCount++
		}

		if newCategoriesCount == 0 {
			break
		}

		pagination = &resp.Data.Pagination
		logger.FromCtx(ctx).Tracef("I have %d categories now; new categories: %d", len(categoriesMap), newCategoriesCount)
	}
	logger.FromCtx(ctx).Tracef("%d categories in total")

	allCategories := make([]helix.Game, 0, len(categoriesMap))
	for _, c := range categoriesMap {
		allCategories = append(allCategories, c)
	}

	return allCategories, nil
}
