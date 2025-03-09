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
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/streamctl/pkg/buildvars"
	"github.com/xaionaro-go/streamctl/pkg/oauthhandler"
	"github.com/xaionaro-go/streamctl/pkg/secret"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/xsync"
)

type Twitch struct {
	closeCtx      context.Context
	closeFn       context.CancelFunc
	chatHandler   *ChatHandler
	client        *helix.Client
	config        Config
	broadcasterID string
	lazyInitOnce  sync.Once
	saveCfgFn     func(Config) error
	tokenLocker   xsync.Mutex
	prepareLocker xsync.Mutex
	clientID      string
	clientSecret  secret.String
}

const twitchDebug = false

var _ streamcontrol.StreamController[StreamProfile] = (*Twitch)(nil)

func New(
	ctx context.Context,
	cfg Config,
	saveCfgFn func(Config) error,
) (*Twitch, error) {
	if cfg.Config.Channel == "" {
		return nil, fmt.Errorf("'channel' is not set")
	}
	clientID := valueOrDefault(cfg.Config.ClientID, buildvars.TwitchClientID)
	clientSecret := valueOrDefault(cfg.Config.ClientSecret.Get(), buildvars.TwitchClientID)
	if clientID == "" || clientSecret == "" {
		return nil, fmt.Errorf(
			"'clientid' or/and 'clientsecret' is/are not set; go to https://dev.twitch.tv/console/apps/create and create an app if it not created, yet",
		)
	}

	getPortsFn := cfg.Config.GetOAuthListenPorts
	if getPortsFn == nil {
		// TODO: find a way to adjust the OAuth ports dynamically without re-creating the Twitch client.
		return nil, fmt.Errorf("the function GetOAuthListenPorts is not set")
	}
	var oauthPorts []uint16
	for {
		oauthPorts = getPortsFn()
		if len(oauthPorts) != 0 {
			break
		}
		logger.Debugf(ctx, "waiting for somebody to provide an OAuthListenerPort")
		time.Sleep(time.Second)
	}
	if len(oauthPorts) == 0 {
		return nil, fmt.Errorf("the function GetOAuthListenPorts returned zero ports")
	}

	ctx, closeFn := context.WithCancel(ctx)
	t := &Twitch{
		closeCtx:     ctx,
		closeFn:      closeFn,
		config:       cfg,
		saveCfgFn:    saveCfgFn,
		clientID:     clientID,
		clientSecret: secret.New(clientSecret),
	}

	h, err := NewChatHandler(ctx, cfg.Config.Channel)
	if err != nil {
		return nil, fmt.Errorf("unable to initialize a chat handler for channel '%s': %w", cfg.Config.Channel, err)
	}
	t.chatHandler = h

	client, err := t.getClient(ctx, oauthPorts[0])
	if err != nil {
		return nil, err
	}
	t.client = client
	var prevTokenUpdate time.Time
	client.OnUserAccessTokenRefreshed(func(newAccessToken, newRefreshToken string) {
		if twitchDebug == true {
			logger.Debugf(ctx, "got new tokens, verifying")
			_, err := client.GetChannelInformation(&helix.GetChannelInformationParams{
				BroadcasterIDs: []string{
					cfg.Config.Channel,
				},
			})
			if err != nil {
				logger.Errorf(ctx, "the token is apparently invalid: %v", err)
			}
		}
		logger.Debugf(ctx, "saving the new tokens")
		cfg.Config.UserAccessToken.Set(newAccessToken)
		cfg.Config.RefreshToken.Set(newRefreshToken)
		err = saveCfgFn(cfg)
		errmon.ObserveErrorCtx(ctx, err)
		now := time.Now()
		if now.Sub(prevTokenUpdate) < time.Second*30 {
			logger.Errorf(
				ctx,
				"updating the token too often, most likely it won't help, so asking to re-authenticate",
			)
			t.prepareLocker.Do(ctx, func() {
				t.client.SetAppAccessToken("")
				t.client.SetUserAccessToken("")
				t.client.SetRefreshToken("")
				prevTokenUpdate = time.Time{}
				err := t.getNewToken(ctx)
				errmon.ObserveErrorCtx(ctx, err)
			})
			return
		}
		prevTokenUpdate = now
	})
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
		return "", fmt.Errorf(
			"expected 1 user with login, but received %d users",
			len(resp.Data.Users),
		)
	}
	return resp.Data.Users[0].ID, nil
}

func (t *Twitch) prepare(ctx context.Context) error {
	logger.Tracef(ctx, "prepare")
	defer logger.Tracef(ctx, "/prepare")
	return xsync.DoA1R1(ctx, &t.prepareLocker, t.prepareNoLock, ctx)
}

func (t *Twitch) prepareNoLock(ctx context.Context) error {
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
			logger.Errorf(ctx, "unable to get broadcaster ID: %v", err)
			return
		}
		logger.Debugf(
			ctx,
			"broadcaster_id: %s (login: %s)",
			t.broadcasterID,
			t.config.Config.Channel,
		)
	})
	return err
}

func (t *Twitch) Close() error {
	t.closeFn()
	return nil
}

func (t *Twitch) editChannelInfo(
	ctx context.Context,
	params *helix.EditChannelInformationParams,
) error {
	logger.Debugf(ctx, "editChannelInfo(ctx, %#+v)", params)
	defer logger.Debugf(ctx, "/editChannelInfo(ctx, %#+v)", params)

	if params == nil {
		return fmt.Errorf("params == nil")
	}
	params.BroadcasterID = t.broadcasterID
	resp, err := t.client.EditChannelInformation(params)
	if err != nil {
		return fmt.Errorf("unable to update the channel info (%#+v): %w", *params, err)
	}
	if resp.ErrorStatus != 0 {
		return fmt.Errorf(
			"unable to update the channel info (%#+v), the response reported an error: %d %v: %v",
			*params,
			resp.ErrorStatus,
			resp.Error,
			resp.ErrorMessage,
		)
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
	logger.Debugf(ctx, "ApplyProfile")
	defer logger.Debugf(ctx, "/ApplyProfile")

	t.prepare(ctx)

	if profile.CategoryName != nil {
		if profile.CategoryID != nil {
			logger.Warnf(
				ctx,
				"both category name and ID are set; these are contradicting stream profile settings; prioritizing the name",
			)
		}
		categoryID, err := t.getCategoryID(ctx, *profile.CategoryName)
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
		tag = truncateStringByByteLength(
			tag,
			25,
		) // see also: https://github.com/twitchdev/issues/issues/789
		tags = append(tags, tag)
	}

	params := &helix.EditChannelInformationParams{}

	hasParams := false
	if tags != nil {
		logger.Debugf(ctx, "has tags")
		if len(tags) == 0 {
			logger.Warnf(
				ctx,
				"unfortunately, there is a bug in the helix lib, which does not allow to set zero tags, so adding tag 'stream' to the list of tags as a placeholder",
			)
			params.Tags = []string{"English"}
		} else {
			params.Tags = tags
		}
		hasParams = true
	}
	if profile.Language != nil {
		logger.Debugf(ctx, "has language")
		params.BroadcasterLanguage = *profile.Language
		hasParams = true
	}
	if profile.CategoryID != nil {
		logger.Debugf(ctx, "has CategoryID")
		params.GameID = *profile.CategoryID
		hasParams = true
	}
	if !hasParams {
		logger.Debugf(ctx, "no parameters, so skipping")
		return nil
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
	ctx context.Context,
	categoryName string,
) (string, error) {
	logger.Debugf(ctx, "getCategoryID")
	defer logger.Debugf(ctx, "/getCategoryID")

	resp, err := t.client.GetGames(&helix.GamesParams{
		Names: []string{categoryName},
	})
	if err != nil {
		return "", fmt.Errorf(
			"unable to query the category info (of name '%s'): %w",
			categoryName,
			err,
		)
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
) (_err error) {
	logger.Debugf(ctx, "StartStream")
	defer func() { logger.Debugf(ctx, "/StartStream: %v", _err) }()

	t.prepare(ctx)
	var result error
	if err := t.SetTitle(ctx, title); err != nil {
		result = multierror.Append(result, fmt.Errorf("unable to set title: %w", err))
	}
	if err := t.SetDescription(ctx, description); err != nil {
		result = multierror.Append(result, fmt.Errorf("unable to set description: %w", err))
	}
	if err := t.ApplyProfile(ctx, profile, customArgs...); err != nil {
		result = multierror.Append(
			result,
			fmt.Errorf("unable to apply the stream-specific profile: %w", err),
		)
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
	logger.Debugf(ctx, "GetStreamStatus")
	defer logger.Debugf(ctx, "/GetStreamStatus")

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
		IsActive:     true,
		StartedAt:    &stream.StartedAt,
		ViewersCount: ptr(uint(stream.ViewerCount)),
	}, nil
}

func (t *Twitch) getTokenIfNeeded(
	ctx context.Context,
) error {
	switch t.config.Config.AuthType {
	case "user":
		t.tokenLocker.Do(ctx, func() {
			if t.client.GetUserAccessToken() == "" {
				t.client.SetUserAccessToken(t.config.Config.UserAccessToken.Get())
			}
			if t.client.GetRefreshToken() == "" {
				t.client.SetRefreshToken(t.config.Config.RefreshToken.Get())
			}
		})
		if t.client.GetUserAccessToken() != "" {
			return nil
		}
	case "app":
		t.tokenLocker.Do(ctx, func() {
			t.client.SetAppAccessToken(t.config.Config.AppAccessToken.Get())
		})
		if t.client.GetAppAccessToken() != "" {
			logger.Debugf(ctx, "already have an app access token")
			return nil
		}
		logger.Debugf(ctx, "do not have an app access token")
	}

	logger.Infof(ctx, "getting a new token")
	return t.getNewToken(ctx)
}

func (t *Twitch) getNewToken(
	ctx context.Context,
) error {
	logger.Debugf(ctx, "getNewToken")
	defer logger.Debugf(ctx, "/getNewToken")

	return xsync.DoR1(ctx, &t.tokenLocker, func() error {
		switch t.config.Config.AuthType {
		case "user":
			err := t.getNewTokenByUser(ctx)
			if err != nil {
				return fmt.Errorf("getting user-token error: %w", err)
			}
			return nil
		case "app":
			err := t.getNewTokenByApp(ctx)
			if err != nil {
				return fmt.Errorf("getting app-token error: %w", err)
			}
			return nil
		default:
			return fmt.Errorf("invalid AuthType: <%s>", t.config.Config.AuthType)
		}
	})
}

func authRedirectURI(listenPort uint16) string {
	return fmt.Sprintf("http://localhost:%d/", listenPort)
}

func (t *Twitch) getNewClientCode(
	ctx context.Context,
) (_err error) {
	logger.Debugf(ctx, "getNewClientCode")
	defer func() { logger.Debugf(ctx, "/getNewClientCode: %v", _err) }()

	oauthHandler := t.config.Config.CustomOAuthHandler
	if oauthHandler == nil {
		oauthHandler = oauthhandler.OAuth2HandlerViaCLI
	}

	ctx, ctxCancelFunc := context.WithCancel(ctx)
	cancelFunc := func() {
		logger.Debugf(ctx, "cancelling the context")
		ctxCancelFunc()
	}

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
		alreadyListening[listenPort] = struct{}{}

		logger.Debugf(ctx, "starting the oauth handler at port %d", listenPort)
		wg.Add(1)
		{
			listenPort := listenPort
			observability.Go(ctx, func() {
				defer logger.Debugf(ctx, "ended the oauth handler at port %d", listenPort)
				defer wg.Done()
				authURL := GetAuthorizationURL(
					&helix.AuthorizationURLParams{
						ResponseType: "code", // or "token"
						Scopes: []string{
							"channel:manage:broadcast",
							"moderator:manage:chat_messages",
							"moderator:manage:banned_users",
						},
					},
					t.clientID,
					authRedirectURI(listenPort),
				)

				arg := oauthhandler.OAuthHandlerArgument{
					AuthURL:    authURL,
					ListenPort: listenPort,
					ExchangeFn: func(code string) (_err error) {
						logger.Debugf(ctx, "ExchangeFn()")
						defer func() { logger.Debugf(ctx, "/ExchangeFn(): %v", _err) }()
						if code == "" {
							return fmt.Errorf("code is empty")
						}
						t.config.Config.ClientCode.Set(code)
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
			})
		}
	}

	// TODO: either support only one port as in New, or support multiple
	//       ports as we do below
	getPortsFn := t.config.Config.GetOAuthListenPorts
	if getPortsFn == nil {
		return fmt.Errorf("the function GetOAuthListenPorts is not set")
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

			for _, listenPort := range ports {
				startHandlerForPort(listenPort)
			}
		}
	})

	observability.Go(ctx, func() {
		wg.Wait()
		close(errCh)
	})
	<-ctx.Done()
	logger.Debugf(ctx, "did successfully took a new client code? -- %v", success)
	if !success {
		errWg.Wait()
		return resultErr
	}
	return nil
}

func (t *Twitch) getNewTokenByUser(
	ctx context.Context,
) error {
	logger.Debugf(ctx, "getNewTokenByUser")
	defer logger.Debugf(ctx, "/getNewTokenByUser")

	if t.config.Config.ClientCode.Get() == "" {
		err := t.getNewClientCode(ctx)
		if err != nil {
			return fmt.Errorf("unable to get client code: %w", err)
		}
	}

	if t.config.Config.ClientCode.Get() == "" {
		return fmt.Errorf("internal error: ClientCode is empty")
	}

	logger.Debugf(ctx, "requesting user access token...")
	resp, err := t.client.RequestUserAccessToken(t.config.Config.ClientCode.Get())
	logger.Debugf(ctx, "requesting user access token result: %#+v %v", resp, err)
	if err != nil {
		return fmt.Errorf("unable to get user access token: %w", err)
	}
	if resp.ErrorStatus != 0 {
		return fmt.Errorf(
			"unable to query: %d %v: %v",
			resp.ErrorStatus,
			resp.Error,
			resp.ErrorMessage,
		)
	}
	t.client.SetUserAccessToken(resp.Data.AccessToken)
	t.client.SetRefreshToken(resp.Data.RefreshToken)
	t.config.Config.ClientCode.Set("")
	t.config.Config.UserAccessToken.Set(resp.Data.AccessToken)
	t.config.Config.RefreshToken.Set(resp.Data.RefreshToken)
	err = t.saveCfgFn(t.config)
	errmon.ObserveErrorCtx(ctx, err)
	return nil
}

func (t *Twitch) getNewTokenByApp(
	ctx context.Context,
) error {
	logger.Debugf(ctx, "getNewTokenByApp")
	defer logger.Debugf(ctx, "/getNewTokenByApp")

	resp, err := t.client.RequestAppAccessToken(nil)
	if err != nil {
		return fmt.Errorf("unable to get app access token: %w", err)
	}
	if resp.ErrorStatus != 0 {
		return fmt.Errorf(
			"unable to get app access token (the response contains an error): %d %v: %v",
			resp.ErrorStatus,
			resp.Error,
			resp.ErrorMessage,
		)
	}
	logger.Debugf(ctx, "setting the app access token")
	t.client.SetAppAccessToken(resp.Data.AccessToken)
	t.config.Config.AppAccessToken.Set(resp.Data.AccessToken)
	err = t.saveCfgFn(t.config)
	errmon.ObserveErrorCtx(ctx, err)
	return nil
}

func (t *Twitch) getClient(
	ctx context.Context,
	oauthListenPort uint16,
) (*helix.Client, error) {
	logger.Debugf(ctx, "getClient(ctx, %#+v, %v)", t.config, oauthListenPort)
	defer logger.Debugf(ctx, "/getClient")

	options := &helix.Options{
		ClientID:     t.clientID,
		ClientSecret: t.clientSecret.Get(),
		RedirectURI:  authRedirectURI(oauthListenPort), // TODO: delete this hardcode
	}
	client, err := helix.NewClientWithContext(ctx, options)
	if err != nil {
		return nil, fmt.Errorf("unable to create a helix client object: %w", err)
	}
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
	logger.Debugf(ctx, "GetAllCategories")
	defer logger.Debugf(ctx, "/GetAllCategories")

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
		logger.FromCtx(ctx).
			Tracef("I have %d categories now; new categories: %d", len(categoriesMap), newCategoriesCount)
	}
	logger.FromCtx(ctx).Tracef("%d categories in total")

	allCategories := make([]helix.Game, 0, len(categoriesMap))
	for _, c := range categoriesMap {
		allCategories = append(allCategories, c)
	}

	return allCategories, nil
}

func (t *Twitch) GetChatMessagesChan(
	ctx context.Context,
) (<-chan streamcontrol.ChatMessage, error) {
	logger.Debugf(ctx, "GetChatMessagesChan")
	defer logger.Debugf(ctx, "/GetChatMessagesChan")

	outCh := make(chan streamcontrol.ChatMessage)
	observability.Go(ctx, func() {
		defer func() {
			logger.Debugf(ctx, "closing the messages channel")
			close(outCh)
		}()
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-t.chatHandler.MessagesChan():
				if !ok {
					logger.Debugf(ctx, "the input channel is closed")
					return
				}
				outCh <- ev
			}
		}
	})

	return outCh, nil
}

func (t *Twitch) SendChatMessage(ctx context.Context, message string) (_ret error) {
	logger.Debugf(ctx, "SendChatMessage(ctx, '%s')", message)
	defer func() { logger.Debugf(ctx, "/SendChatMessage(ctx, '%s'): %v", message, _ret) }()

	t.prepare(ctx)

	_, err := t.client.SendChatMessage(&helix.SendChatMessageParams{
		BroadcasterID: t.broadcasterID,
		SenderID:      t.broadcasterID,
		Message:       message,
	})
	return err
}
func (t *Twitch) RemoveChatMessage(
	ctx context.Context,
	messageID streamcontrol.ChatMessageID,
) (_ret error) {
	logger.Debugf(ctx, "RemoveChatMessage(ctx, '%s')", messageID)
	defer func() { logger.Debugf(ctx, "/RemoveChatMessage(ctx, '%s'): %v", messageID, _ret) }()

	t.prepare(ctx)

	_, err := t.client.DeleteChatMessage(&helix.DeleteChatMessageParams{
		BroadcasterID: t.broadcasterID,
		ModeratorID:   t.broadcasterID,
		MessageID:     string(messageID),
	})
	return err
}
func (t *Twitch) BanUser(
	ctx context.Context,
	userID streamcontrol.ChatUserID,
	reason string,
	deadline time.Time,
) (_err error) {
	logger.Debugf(ctx, "BanUser(ctx, '%s', '%s', %v)", userID, reason, deadline)
	defer func() { logger.Debugf(ctx, "/BanUser(ctx, '%s', '%s', %v): %v", userID, reason, deadline, _err) }()

	t.prepare(ctx)

	duration := 0
	if !deadline.IsZero() {
		duration = int(time.Until(deadline).Seconds())
	}
	_, err := t.client.BanUser(&helix.BanUserParams{
		BroadcasterID: t.broadcasterID,
		ModeratorId:   t.broadcasterID,
		Body: helix.BanUserRequestBody{
			Duration: duration,
			Reason:   reason,
			UserId:   string(userID),
		},
	})
	return err
}

func (t *Twitch) IsCapable(
	ctx context.Context,
	cap streamcontrol.Capability,
) bool {
	switch cap {
	case streamcontrol.CapabilitySendChatMessage:
		return true
	case streamcontrol.CapabilityDeleteChatMessage:
		return true
	case streamcontrol.CapabilityBanUser:
		return true
	}
	return false
}
