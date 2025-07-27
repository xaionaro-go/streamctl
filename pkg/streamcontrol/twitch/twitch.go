package twitch

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/davecgh/go-spew/spew"
	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/experimental/errmon"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/hashicorp/go-multierror"
	"github.com/nicklaw5/helix/v2"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/streamctl/pkg/buildvars"
	"github.com/xaionaro-go/streamctl/pkg/ringbuffer"
	"github.com/xaionaro-go/streamctl/pkg/secret"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/twitch/auth"
	"github.com/xaionaro-go/xsync"
)

type Twitch struct {
	closeCtx       context.Context
	closeFn        context.CancelFunc
	chatHandlerSub *ChatHandlerSub
	chatHandlerIRC *ChatHandlerIRC
	client         client
	config         Config
	broadcasterID  string
	lazyInitOnce   sync.Once
	saveCfgFn      func(Config) error
	tokenLocker    xsync.Mutex
	prepareLocker  xsync.Mutex
	clientID       string
	clientSecret   secret.String
}

const (
	twitchDebug        = false
	debugUseMockClient = false
)

var _ streamcontrol.StreamController[StreamProfile] = (*Twitch)(nil)

func New(
	ctx context.Context,
	cfg Config,
	saveCfgFn func(Config) error,
) (*Twitch, error) {
	ctx = belt.WithField(ctx, "controller", ID)
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

	h, err := NewChatHandlerIRC(ctx, cfg.Config.Channel)
	if err != nil {
		return nil, fmt.Errorf("unable to initialize a chat handler for channel '%s': %w", cfg.Config.Channel, err)
	}
	t.chatHandlerIRC = h

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

func GetUserID(
	_ context.Context,
	client client,
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
	defer func() { logger.Tracef(ctx, "/prepare") }()
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
		t.broadcasterID, err = GetUserID(ctx, t.client, t.config.Config.Channel)
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

	t.prepareChatListenerNoLock(ctx)
	return err
}

func (t *Twitch) prepareChatListenerNoLock(ctx context.Context) {
	if t.chatHandlerSub != nil {
		return
	}

	var err error
	t.chatHandlerSub, err = NewChatHandlerSub(
		t.closeCtx, t.client, t.broadcasterID,
		func(ctx context.Context) {
			t.prepareLocker.Do(ctx, func() {
				t.chatHandlerSub = nil
			})
		},
	)
	if err != nil {
		logger.Errorf(ctx, "unable to initialize websockets based chat listener: %v", err)
	}
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
	defer func() { logger.Debugf(ctx, "/editChannelInfo(ctx, %#+v)", params) }()

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
	defer func() { logger.Debugf(ctx, "/ApplyProfile") }()

	if err := t.prepare(ctx); err != nil {
		return fmt.Errorf("unable to get a prepared client: %w", err)
	}

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
) (_ret string, _err error) {
	logger.Debugf(ctx, "getCategoryID")
	defer func() { logger.Debugf(ctx, "/getCategoryID: %s %v") }()

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
	if err := t.prepare(ctx); err != nil {
		return fmt.Errorf("unable to get a prepared client: %w", err)
	}
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

	if err := t.prepare(ctx); err != nil {
		return fmt.Errorf("unable to get a prepared client: %w", err)
	}
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
	defer func() { logger.Debugf(ctx, "/GetStreamStatus") }()

	if err := t.prepare(ctx); err != nil {
		return nil, fmt.Errorf("unable to get a prepared client: %w", err)
	}
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
	defer func() { logger.Debugf(ctx, "/getNewToken") }()

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

func (t *Twitch) getNewClientCode(
	ctx context.Context,
) (_err error) {
	return auth.NewClientCode(
		ctx,
		t.clientID,
		t.config.Config.CustomOAuthHandler,
		t.config.Config.GetOAuthListenPorts,
		func(code string) {
			t.config.Config.ClientCode.Set(code)
			err := t.saveCfgFn(t.config)
			errmon.ObserveErrorCtx(ctx, err)
		},
	)
}

func (t *Twitch) getNewTokenByUser(
	ctx context.Context,
) error {
	if t.config.Config.ClientCode.Get() == "" {
		err := t.getNewClientCode(ctx)
		if err != nil {
			return fmt.Errorf("unable to get client code: %w", err)
		}
	}

	accessToken, refreshToken, err := auth.NewTokenByUser(ctx, t.client, t.config.Config.ClientCode)
	if err != nil {
		return fmt.Errorf("unable to get an access token: %w", err)
	}

	logger.Debugf(ctx, "setting the user access token")
	t.client.SetUserAccessToken(accessToken.Get())
	t.client.SetRefreshToken(refreshToken.Get())
	t.config.Config.ClientCode.Set("")
	t.config.Config.UserAccessToken = accessToken
	t.config.Config.RefreshToken = refreshToken
	err = t.saveCfgFn(t.config)
	errmon.ObserveErrorCtx(ctx, err)
	return nil
}

func (t *Twitch) getNewTokenByApp(
	ctx context.Context,
) error {
	accessToken, err := auth.NewTokenByApp(ctx, t.client)
	if err != nil {
		return err
	}
	logger.Debugf(ctx, "setting the app access token")
	t.client.SetAppAccessToken(accessToken.Get())
	t.config.Config.AppAccessToken = accessToken
	err = t.saveCfgFn(t.config)
	errmon.ObserveErrorCtx(ctx, err)
	return nil
}

func (t *Twitch) getClient(
	ctx context.Context,
	oauthListenPort uint16,
) (client, error) {
	logger.Debugf(ctx, "getClient(ctx, %#+v, %v)", t.config, oauthListenPort)
	defer func() { logger.Debugf(ctx, "/getClient") }()

	if debugUseMockClient {
		return newClientMock(), nil
	}
	options := &helix.Options{
		ClientID:     t.clientID,
		ClientSecret: t.clientSecret.Get(),
		RedirectURI:  auth.RedirectURI(oauthListenPort), // TODO: delete this hardcode
	}
	client, err := helix.NewClientWithContext(ctx, options)
	if err != nil {
		return nil, fmt.Errorf("unable to create a helix client object: %w", err)
	}
	return client, nil
}

func (t *Twitch) GetAllCategories(
	ctx context.Context,
) ([]helix.Game, error) {
	logger.Debugf(ctx, "GetAllCategories")
	defer func() { logger.Debugf(ctx, "/GetAllCategories") }()

	if err := t.prepare(ctx); err != nil {
		return nil, fmt.Errorf("unable to get a prepared client: %w", err)
	}
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
	defer func() { logger.Debugf(ctx, "/GetChatMessagesChan") }()

	if err := t.prepare(ctx); err != nil {
		logger.Errorf(ctx, "unable to prepare the client: %v", err)
	}

	outCh := make(chan streamcontrol.ChatMessage)
	recentMsgIDs := ringbuffer.New[streamcontrol.ChatMessageID](10)

	sendEvent := func(ev streamcontrol.ChatMessage) {
		recentMsgIDs.Add(ev.MessageID)
		select {
		case outCh <- ev:
		default:
			logger.Warnf(ctx, "the queue is full, dropping message %#+v", ev)
		}
	}

	alreadySeen := func(msgID streamcontrol.ChatMessageID) bool {
		return recentMsgIDs.Contains(msgID)
	}

	observability.Go(ctx, func(ctx context.Context) {
		defer func() {
			logger.Debugf(ctx, "closing the messages channel")
			close(outCh)
		}()
		var (
			chSub <-chan streamcontrol.ChatMessage
			chIRC <-chan streamcontrol.ChatMessage
		)
		t.prepareLocker.Do(ctx, func() {
			if t.chatHandlerSub != nil {
				chSub = t.chatHandlerSub.MessagesChan()
			}
			if t.chatHandlerIRC != nil {
				chIRC = t.chatHandlerIRC.MessagesChan()
			}
		})
		logger.Debugf(ctx, "chSub == %p; chIRC == %p", chSub, chIRC)
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			if chSub == nil {
				t.prepareLocker.Do(ctx, func() {
					if t.chatHandlerSub == nil {
						logger.Debugf(ctx, "the chat listener is closed, trying to reopen it")
						t.prepareChatListenerNoLock(ctx)
					}
					if t.chatHandlerSub != nil {
						chSub = t.chatHandlerSub.MessagesChan()
					}
				})
			}
			if chSub == nil && chIRC == nil {
				logger.Debugf(ctx, "both channels are closed")
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				continue
			case ev, ok := <-chSub:
				if !ok {
					chSub = nil
					logger.Debugf(ctx, "the API receiver channel closed")
					continue
				}
				logger.Tracef(ctx, "received a message from API: %#+v", ev)
				if alreadySeen(ev.MessageID) {
					logger.Tracef(ctx, "already seen message %s", ev.MessageID)
					continue
				}
				sendEvent(ev)
			case evIRC, ok := <-chIRC:
				if !ok {
					chIRC = nil
					logger.Debugf(ctx, "the IRC receiver channel closed")
					continue
				}
				logger.Tracef(ctx, "received a message from IRC: %#+v", evIRC)
				if alreadySeen(evIRC.MessageID) {
					logger.Tracef(ctx, "already seen message %s", evIRC.MessageID)
					continue
				}

				// not previously seen message:
				select {
				case evSub, ok := <-chSub:
					if !ok {
						chSub = nil
						break
					}
					logger.Tracef(ctx, "received a message from API: %#+v", evIRC)
					sendEvent(evSub)
					if alreadySeen(evIRC.MessageID) {
						logger.Tracef(ctx, "the same message")
						continue
					}
				case <-time.After(time.Second):
					logger.Warnf(ctx, "received a message from IRC, but not from API")
				}
				sendEvent(evIRC)
			}
		}
	})

	return outCh, nil
}

func (t *Twitch) SendChatMessage(ctx context.Context, message string) (_ret error) {
	logger.Debugf(ctx, "SendChatMessage(ctx, '%s')", message)
	defer func() { logger.Debugf(ctx, "/SendChatMessage(ctx, '%s'): %v", message, _ret) }()

	if err := t.prepare(ctx); err != nil {
		return fmt.Errorf("unable to get a prepared client: %w", err)
	}

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

	if err := t.prepare(ctx); err != nil {
		return fmt.Errorf("unable to get a prepared client: %w", err)
	}

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

	if err := t.prepare(ctx); err != nil {
		return fmt.Errorf("unable to get a prepared client: %w", err)
	}

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
	case streamcontrol.CapabilityIsChannelStreaming:
		return true
	case streamcontrol.CapabilityShoutout:
		return true
	case streamcontrol.CapabilityRaid:
		return true
	}
	return false
}

func (t *Twitch) IsChannelStreaming(
	ctx context.Context,
	chanID streamcontrol.ChatUserID,
) (_ret bool, _err error) {
	logger.Debugf(ctx, "IsChannelStreaming")
	defer func() { logger.Debugf(ctx, "/IsChannelStreaming: %v %v", _ret, _err) }()

	reply, err := t.client.GetStreams(&helix.StreamsParams{
		UserIDs: []string{string(chanID)},
	})
	if err != nil {
		return false, fmt.Errorf("unable to check if '%s' is streaming: %w", chanID, err)
	}
	if len(reply.Data.Streams) == 0 {
		return false, nil
	}
	if len(reply.Data.Streams) > 1 {
		return false, fmt.Errorf("received %d channels instead of 1", len(reply.Data.Streams))
	}
	return true, nil
}

func (t *Twitch) RaidTo(
	ctx context.Context,
	idOrLogin streamcontrol.ChatUserID,
) (_err error) {
	logger.Debugf(ctx, "RaidTo(ctx, '%s')", idOrLogin)
	defer func() { logger.Debugf(ctx, "/RaidTo(ctx, '%s'): %v", idOrLogin, _err) }()
	user, err := t.GetUser(string(idOrLogin))
	if err != nil {
		return fmt.Errorf("unable to get user '%s': %w", idOrLogin, err)
	}
	params := &helix.StartRaidParams{
		FromBroadcasterID: t.broadcasterID,
		ToBroadcasterID:   string(user.ID),
	}
	logger.Debugf(ctx, "RaidTo(ctx, '%s'): %#+v", idOrLogin, params)
	resp, err := t.client.StartRaid(params)
	if err != nil {
		return fmt.Errorf("unable to raid %#+v: %v", params, err)
	}
	logger.Debugf(ctx, "raid results: %#+v", resp)
	return nil
}

func (t *Twitch) GetUser(idOrLogin string) (*helix.User, error) {
	users, err := t.client.GetUsers(&helix.UsersParams{
		IDs: []string{string(idOrLogin)},
	})
	if err != nil {
		return nil, fmt.Errorf("unable to get user info for userID '%s': %w", idOrLogin, err)
	}
	if len(users.Data.Users) == 0 {
		users, err = t.client.GetUsers(&helix.UsersParams{
			Logins: []string{string(idOrLogin)},
		})
		if err != nil {
			return nil, fmt.Errorf("unable to get user info for login '%s': %w", idOrLogin, err)
		}
	}
	if len(users.Data.Users) == 0 {
		return nil, fmt.Errorf("user with ID-or-login '%s' not found", idOrLogin)
	}
	return &users.Data.Users[0], nil
}

func (t *Twitch) Shoutout(
	ctx context.Context,
	userIDOrLogin streamcontrol.ChatUserID,
) (_err error) {
	logger.Debugf(ctx, "Shoutout(ctx, '%s')", userIDOrLogin)
	defer func() { logger.Debugf(ctx, "/Shoutout(ctx, '%s'): %v", userIDOrLogin, _err) }()
	params := &helix.SendShoutoutParams{
		FromBroadcasterID: t.broadcasterID,
		ToBroadcasterID:   string(userIDOrLogin),
		ModeratorID:       t.broadcasterID,
	}
	logger.Debugf(ctx, "Shoutout(ctx, '%s'): %#+v", userIDOrLogin, params)
	_, err := t.client.SendShoutout(params)
	if err != nil {
		return fmt.Errorf("unable to send the shoutout (%#+v): %w", params, err)
	}

	user, err := t.GetUser(string(userIDOrLogin))
	if err != nil {
		return fmt.Errorf("unable to get user '%s': %w", userIDOrLogin, err)
	}
	reply, err := t.client.GetStreams(&helix.StreamsParams{
		UserIDs: []string{string(user.ID)},
	})
	if err != nil {
		logger.Errorf(ctx, "unable to get streams info (userID: %v): %w", user.ID, err)
		return t.sendShoutoutMessageWithoutChanInfo(ctx, *user)
	}
	if len(reply.Data.Streams) == 0 {
		return t.sendShoutoutMessageWithoutChanInfo(ctx, *user)
	}
	return t.sendShoutoutMessage(ctx, *user, reply.Data.Streams[0])
}

func (t *Twitch) sendShoutoutMessageWithoutChanInfo(
	ctx context.Context,
	user helix.User,
) (_err error) {
	logger.Debugf(ctx, "sendShoutoutMessageWithoutChanInfo(ctx, '%s')", spew.Sdump(user))
	defer func() {
		logger.Debugf(ctx, "/sendShoutoutMessageWithoutChanInfo(ctx, '%s'): %v", spew.Sdump(user), _err)
	}()
	yearsExists := float64(int(time.Since(user.CreatedAt.Time).Hours()/24/364*10)) / 10
	err := t.SendChatMessage(ctx, fmt.Sprintf("Shoutout to %s! A great creator (%.1f years on Twitch)! Their self-description: '%s'. Take a look at their channel and click that follow button! https://www.twitch.tv/%s", user.DisplayName, yearsExists, user.Description, user.Login))
	if err != nil {
		return fmt.Errorf("unable to send the message (case #0): %w", err)
	}
	return nil
}

func (t *Twitch) sendShoutoutMessage(
	ctx context.Context,
	user helix.User,
	stream helix.Stream,
) (_err error) {
	logger.Debugf(ctx, "sendShoutoutMessage(ctx, '%s')", spew.Sdump(user))
	defer func() { logger.Debugf(ctx, "/sendShoutoutMessage(ctx, '%s'): %v", spew.Sdump(user), _err) }()
	yearsExists := float64(int(time.Since(user.CreatedAt.Time).Hours()/24/364*10)) / 10
	err := t.SendChatMessage(ctx, fmt.Sprintf("Shoutout to %s! A great creator (%.1f years on Twitch)! Their last stream: '%s'. Their self-description: '%s'. Take a look at their channel and click that follow button! https://www.twitch.tv/%s", user.DisplayName, yearsExists, stream.Title, user.Description, user.Login))
	if err != nil {
		return fmt.Errorf("unable to send the message (case #1): %w", err)
	}
	return nil
}
