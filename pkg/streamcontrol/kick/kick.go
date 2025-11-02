package kick

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/google/uuid"
	"github.com/scorfly/gokick"
	"github.com/xaionaro-go/kickcom"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/streamctl/pkg/oauthhandler"
	"github.com/xaionaro-go/streamctl/pkg/secret"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/kick/kickclientobsolete"
	"github.com/xaionaro-go/xsync"
)

const (
	debugUseMockClient = false
)

type Kick struct {
	CloseCtx               context.Context
	CloseFn                context.CancelFunc
	Channel                *kickcom.ChannelV1
	Client                 *Client
	ClientOBSOLETE         *kickclientobsolete.KickClientOBSOLETE
	ChatHandler            ChatHandlerAbstract
	ChatHandlerInitStarted bool
	ChatHandlerLocker      xsync.CtxLocker
	CurrentConfig          Config
	CurrentConfigLocker    xsync.Mutex
	SaveCfgFn              func(Config) error
	PrepareLocker          xsync.Mutex

	lazyInitOnce         sync.Once
	getAccessTokenLocker xsync.Mutex
}

var _ streamcontrol.StreamController[StreamProfile] = (*Kick)(nil)

func New(
	ctx context.Context,
	cfg Config,
	saveCfgFn func(Config) error,
) (*Kick, error) {
	ctx = belt.WithField(ctx, "controller", ID)
	if cfg.Config.Channel == "" {
		return nil, fmt.Errorf("channel is not set")
	}

	clientOld, err := kickclientobsolete.New()
	if err != nil {
		return nil, fmt.Errorf("unable to initialize the old client: %w", err)
	}

	client, err := getClient(cfg.Config)
	if err != nil {
		return nil, fmt.Errorf("unable to initialize the client: %w", err)
	}

	ctx, closeFn := context.WithCancel(ctx)
	k := &Kick{
		CloseCtx:          ctx,
		CloseFn:           closeFn,
		ChatHandlerLocker: make(xsync.CtxLocker, 1),
		CurrentConfig:     cfg,
		ClientOBSOLETE:    clientOld,
		SaveCfgFn:         saveCfgFn,
	}
	k.SetClient(client)
	client.OnUserAccessTokenRefreshed(k.onUserAccessTokenRefreshed)
	observability.Go(ctx, func(ctx context.Context) {
		k.keepAliveLoop(ctx)
	})
	return k, nil
}

func getClient(
	cfg PlatformSpecificConfig,
) (Client, error) {
	if debugUseMockClient {
		return newClientMock(), nil
	}

	client, err := newClient(&gokick.ClientOptions{
		UserAccessToken:  cfg.UserAccessToken.Get(),
		UserRefreshToken: cfg.RefreshToken.Get(),
		ClientID:         cfg.ClientID,
		ClientSecret:     cfg.ClientSecret.Get(),
	})
	if err != nil {
		return nil, err
	}

	return client, nil
}

func (k *Kick) onUserAccessTokenRefreshed(
	userAccessToken string,
	refreshToken string,
) {
	ctx := context.TODO()
	logger.Debugf(ctx, "onUserAccessTokenRefreshed")
	defer logger.Debugf(ctx, "/onUserAccessTokenRefreshed")
	k.CurrentConfigLocker.Do(ctx, func() {
		logger.Infof(ctx, "UserAccessToken had been refreshed")
		k.CurrentConfig.Config.UserAccessToken.Set(userAccessToken)
		k.CurrentConfig.Config.RefreshToken.Set(refreshToken)
		err := k.SaveCfgFn(k.CurrentConfig)
		if err != nil {
			logger.Errorf(ctx, "unable to save the config: %v", err)
		}
	})
}

func (k *Kick) keepAliveLoop(
	ctx context.Context,
) {
	logger.Debugf(ctx, "keepAliveLoop")
	defer func() { logger.Debugf(ctx, "/keepAliveLoop") }()

	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		if k.Channel == nil { // TODO: fix non-atomicity
			logger.Warnf(ctx, "channel info is not set, yet")
			time.Sleep(time.Second)
			continue
		}
		_, err := k.getLivestreams(
			k.CloseCtx,
			gokick.NewLivestreamListFilter().SetBroadcasterUserIDs(int(k.Channel.UserID)),
		)
		if err != nil {
			logger.Errorf(ctx, "unable to get my stream status: %v", err)
			time.Sleep(time.Second)
			continue
		}
		select {
		case <-k.CloseCtx.Done():
			return
		case <-t.C:
		}
	}
}

func (k *Kick) initChatHandlerNoLock(
	ctx context.Context,
) error {
	chatHandler, err := k.newChatHandlerOBSOLETE(ctx, k.CurrentConfig.Config.Channel)
	if err == nil {
		k.ChatHandler = chatHandler
		return nil
	}

	for {
		logger.Errorf(ctx, "unable to initialize chat handler: %v", err)
		time.Sleep(time.Second)
		select {
		case <-k.CloseCtx.Done():
			logger.Debugf(ctx, "initChatHandler: cancelled (case #1)")
			return fmt.Errorf("k.CloseCtx is closed: %w", k.CloseCtx.Err())
		case <-ctx.Done():
			logger.Debugf(ctx, "initChatHandler: cancelled (case #2)")
			return fmt.Errorf("ctx is closed: %w", ctx.Err())
		default:
		}
		chatHandler, err = k.newChatHandlerOBSOLETE(ctx, k.CurrentConfig.Config.Channel)
		if err != nil {
			logger.Debugf(ctx, "initChatHandler: unable to create a new chat handler: %v", err)
			continue
		}
		k.ChatHandler = chatHandler
		return nil
	}
}

func authRedirectURI(listenPort uint16) string {
	return fmt.Sprintf("http://localhost:%d/oauth/kick/callback", listenPort)
}

func (k *Kick) getAccessToken(
	ctx context.Context,
) (_err error) {
	logger.Tracef(ctx, "getAccessToken")
	defer func() { logger.Tracef(ctx, "/getAccessToken: %v", _err) }()
	return xsync.DoR1(ctx, &k.getAccessTokenLocker, func() error {
		return k.getAccessTokenNoLock(ctx)
	})
}

func (k *Kick) getAccessTokenNoLock(
	ctx context.Context,
) (_err error) {
	logger.Tracef(ctx, "getAccessTokenNoLock")
	defer func() { logger.Tracef(ctx, "/getAccessTokenNoLock: %v", _err) }()

	getPortsFn := k.CurrentConfig.Config.GetOAuthListenPorts
	if getPortsFn == nil {
		// TODO: find a way to adjust the OAuth ports dynamically without re-creating the Kick client.
		return fmt.Errorf("the function GetOAuthListenPorts is not set")
	}
	var oauthPorts []uint16
	for {
		oauthPorts = getPortsFn()
		if len(oauthPorts) != 0 {
			break
		}
		logger.Debugf(ctx, "waiting for somebody to provide OAuthListenerPorts")
		time.Sleep(time.Second)
	}
	if len(oauthPorts) < 2 {
		// we require two ports, because the first port is used by Twitch
		// TODO: remove all this ugly hardcodes
		return fmt.Errorf("the function GetOAuthListenPorts returned less than 2 ports (%d)", len(oauthPorts))
	}

	listenPort := oauthPorts[1] // TODO: remove this hardcode [1]; it currently exists to use the different port from what we use for Twitch authentication

	redirectURL := authRedirectURI(listenPort)

	codeVerifier := uuid.New().String() // random string
	codeVerifierSHA256 := sha256.Sum256([]byte(codeVerifier))
	codeChallenge := base64.URLEncoding.EncodeToString(codeVerifierSHA256[:])

	scopes := []gokick.Scope{
		gokick.ScopeUserRead,
		gokick.ScopeChannelRead,
		gokick.ScopeChannelWrite,
		gokick.ScopeChatWrite,
		gokick.ScopeStremkeyRead,
		gokick.ScopeEventSubscribe,
		gokick.ScopeModerationBan,
	}
	logger.Debugf(ctx, "scopes: %v", scopes)
	authURL, err := k.GetClient().GetAuthorize(
		redirectURL,
		"EMPTY",
		codeChallenge,
		scopes,
	)
	if err != nil {
		return fmt.Errorf("unable to get an authorization endpoint URL: %w", err)
	}

	err = k.CurrentConfig.Config.CustomOAuthHandler(ctx, oauthhandler.OAuthHandlerArgument{
		AuthURL:    authURL,
		ListenPort: listenPort,
		ExchangeFn: func(
			ctx context.Context,
			code string,
		) error {
			now := time.Now()
			token, err := k.GetClient().GetToken(ctx, redirectURL, code, codeVerifier)
			if err != nil {
				return fmt.Errorf("unable to get an access token: %w", err)
			}

			return k.setToken(ctx, token, now)
		},
	})
	if err != nil {
		return fmt.Errorf("an error occurred during the authorization procedure: %w", err)
	}

	return nil
}

func (k *Kick) setToken(
	ctx context.Context,
	token gokick.TokenResponse,
	now time.Time,
) (_err error) {
	logger.Tracef(ctx, "setToken")
	defer func() { logger.Tracef(ctx, "/setToken: %v", _err) }()
	logger.Infof(ctx, "new UserAccessToken was set")
	if client := k.GetClient(); client != nil {
		client.SetUserAccessToken(token.AccessToken)
		client.SetUserRefreshToken(token.AccessToken)
	}
	k.CurrentConfig.Config.UserAccessToken.Set(token.AccessToken)
	k.CurrentConfig.Config.UserAccessTokenExpiresAt = now.Add(time.Second * time.Duration(token.ExpiresIn))
	k.CurrentConfig.Config.RefreshToken.Set(token.RefreshToken)
	logger.Tracef(ctx, "'%v' '%v' '%v'", k.CurrentConfig.Config.UserAccessToken.Get(), k.CurrentConfig.Config.UserAccessTokenExpiresAt, k.CurrentConfig.Config.RefreshToken.Get())
	err := k.SaveCfgFn(k.CurrentConfig)
	if err != nil {
		return fmt.Errorf("unable to save the config: %w", err)
	}
	return nil
}

func (k *Kick) Close() (_err error) {
	ctx := context.Background()
	logger.Debugf(ctx, "Close(ctx)")
	defer func() { logger.Debugf(ctx, "/Close(ctx): %v", _err) }()
	k.CloseFn()
	return nil
}

func (k *Kick) SetTitle(ctx context.Context, title string) (err error) {
	logger.Debugf(ctx, "SetTitle(ctx, '%s')", title)
	defer func() { logger.Debugf(ctx, "/SetTitle(ctx, '%s'): %v", title, err) }()

	if err := k.prepare(ctx); err != nil {
		return fmt.Errorf("unable to get a prepared client: %w", err)
	}

	_, err = k.GetClient().UpdateStreamTitle(ctx, title)
	return
}

func (k *Kick) SetDescription(ctx context.Context, description string) error {
	logger.Warnf(ctx, "not implemented yet")
	return nil
}

func (k *Kick) InsertAdsCuePoint(ctx context.Context, ts time.Time, duration time.Duration) error {
	logger.Warnf(ctx, "not implemented yet")
	return nil
}

func (k *Kick) Flush(ctx context.Context) error {
	return nil
}

func (k *Kick) EndStream(ctx context.Context) error {
	// Kick ends a stream automatically, nothing to do:
	return nil
}

func (k *Kick) getLivestreams(
	ctx context.Context,
	filter gokick.LivestreamListFilter,
) (_ret *gokick.LivestreamsResponseWrapper, _err error) {
	logger.Debugf(ctx, "getLivestreams")
	defer func() { logger.Debugf(ctx, "/getLivestreams: %v, %v", _ret, _err) }()

	if err := k.prepare(ctx); err != nil {
		return nil, fmt.Errorf("unable to get a prepared client: %w", err)
	}

	client := k.GetClient()
	if client == nil {
		return nil, fmt.Errorf("client is not initialized")
	}

	resp, err := client.GetLivestreams(k.CloseCtx, filter)
	if err != nil {
		return nil, fmt.Errorf("unable to get my stream status: %w", err)
	}

	return &resp, nil
}

func (k *Kick) GetStreamStatus(
	ctx context.Context,
) (_ret *streamcontrol.StreamStatus, _err error) {
	logger.Debugf(ctx, "GetStreamStatus")
	defer func() { logger.Debugf(ctx, "/GetStreamStatus: %v, %v", _ret, _err) }()

	if err := k.prepare(ctx); err != nil {
		return nil, fmt.Errorf("unable to get a prepared client: %w", err)
	}

	//resp, err := k.Client.GetLivestreams(ctx, gokick.NewLivestreamListFilter().SetBroadcasterUserIDs(k.Channel.BroadcasterUserID))
	info, err := k.ClientOBSOLETE.GetLivestreamV2(ctx, k.Channel.Slug)
	if err != nil {
		err := fmt.Errorf("unable to request stream status using the reverse-engineering lib: %w", err)
		logger.Errorf(ctx, "%v", err)
		streamStatus, err2 := k.getStreamStatusUsingNormalClient(ctx)
		if err2 == nil {
			return streamStatus, nil
		}
		return nil, errors.Join(
			err,
			fmt.Errorf("unable to request stream status using the normal lib: %w", err),
		)
	}
	/*if len(resp.Result) > 1 {
		return nil, fmt.Errorf("expected livestream status of one channel (or no channels), but received for %d channels", len(resp.Result))
	}*/

	logger.Tracef(ctx, "the received livestream status is: %s", spew.Sdump(info))

	if info.Data == nil {
		return &streamcontrol.StreamStatus{
			IsActive:     false,
			ViewersCount: nil,
			StartedAt:    nil,
			CustomData:   nil,
		}, nil
	}

	/*var startedAtPtr *time.Time
	startedAt, err := ParseTimestamp(info.StartedAt)
	if err == nil {
		startedAtPtr = &startedAt
	} else {
		logger.Errorf(ctx, "unable to parse '%s' as a timestamp: %v", info.StartedAt, err)
	}*/
	return &streamcontrol.StreamStatus{
		IsActive:     true,
		ViewersCount: ptr(uint(info.Data.Viewers)),
		StartedAt:    &info.Data.CreatedAt,
		CustomData:   info,
	}, nil
}

const timeLayout = "2006-01-02T15:04:05-0700"
const timeLayoutFallback = time.RFC3339

func ParseTimestamp(s string) (time.Time, error) {
	ts, err0 := time.Parse(timeLayout, s)
	if err0 == nil {
		return ts, nil
	}
	ts, err1 := time.Parse(timeLayoutFallback, s)
	if err1 == nil {
		return ts, nil
	}
	return time.Now(), errors.Join(err0, err1)
}

func (k *Kick) getStreamStatusUsingNormalClient(
	ctx context.Context,
) (_ret *streamcontrol.StreamStatus, _err error) {
	logger.Debugf(ctx, "getStreamStatusUsingNormalClient")
	defer func() { logger.Debugf(ctx, "/getStreamStatusUsingNormalClient: %v %v", _ret, _err) }()

	if err := k.prepare(ctx); err != nil {
		return nil, fmt.Errorf("unable to get a prepared client: %w", err)
	}

	resp, err := k.GetClient().GetChannels(
		ctx,
		gokick.NewChannelListFilter().SetBroadcasterUserIDs([]int{int(k.Channel.UserID)}),
	)
	if err != nil {
		return nil, fmt.Errorf("unable to get channels info")
	}
	if len(resp.Result) != 1 {
		return nil, fmt.Errorf("expected to get info about one channel, but received about %d channels", len(resp.Result))
	}
	chanInfo := resp.Result[0]

	if !chanInfo.Stream.IsLive {
		return &streamcontrol.StreamStatus{
			IsActive:     false,
			ViewersCount: nil,
			StartedAt:    nil,
			CustomData:   nil,
		}, nil
	}

	var startedAt *time.Time
	if chanInfo.Stream.StartTime != "" {
		v, err := time.Parse(time.RFC3339Nano, chanInfo.Stream.StartTime)
		if err != nil {
			logger.Errorf(ctx, "unable to parse date '%s': %w", chanInfo.Stream.StartTime, err)
		}
		startedAt = &v
	}

	return &streamcontrol.StreamStatus{
		IsActive:     true,
		ViewersCount: ptr(uint(chanInfo.Stream.ViewerCount)),
		StartedAt:    startedAt,
		CustomData: CustomData{
			Key:      secret.New(chanInfo.Stream.Key),
			URL:      chanInfo.Stream.URL,
			IsMature: chanInfo.Stream.IsMature,
			Language: chanInfo.Stream.Language,
		},
	}, nil
}

func (k *Kick) GetAllCategories(
	ctx context.Context,
) (_ret []kickcom.CategoryV1Short, _err error) {
	logger.Debugf(ctx, "GetAllCategories")
	defer func() { logger.Debugf(ctx, "/GetAllCategories: len:%d, %v", len(_ret), _err) }()

	//reply, err := k.Client.GetCategories(ctx, gokick.NewCategoryListFilter())
	reply, err := k.ClientOBSOLETE.GetSubcategoriesV1(ctx)
	if err != nil {
		return nil, fmt.Errorf("unable to get subcategories: %w", err)
	}

	return *reply, nil
}

func (k *Kick) tryGetChatHandler(
	ctx context.Context,
) (ChatHandlerAbstract, error) {
	return xsync.DoR2(ctx, &k.ChatHandlerLocker, func() (ChatHandlerAbstract, error) {
		if k.ChatHandler != nil {
			return k.ChatHandler, nil
		}
		k.startInitChatHandlerNoLock(ctx)
		return nil, fmt.Errorf("chat handler is being initialized")
	})
}

func (k *Kick) startInitChatHandlerNoLock(
	ctx context.Context,
) {
	if k.ChatHandlerInitStarted {
		return
	}
	k.ChatHandlerInitStarted = true
	go func() {
		defer func() { k.ChatHandlerInitStarted = false }()
		err := k.initChatHandlerNoLock(ctx)
		if err != nil {
			logger.Errorf(ctx, "unable to initialize chat handler: %v", err)
		}
	}()
}

func (k *Kick) getChatHandler(
	ctx context.Context,
) ChatHandlerAbstract {
	for {
		chatHandler, err := k.tryGetChatHandler(ctx)
		if err != nil {
			logger.Errorf(ctx, "unable to get chat handler: %v", err)
			time.Sleep(time.Second)
			continue
		}
		if chatHandler != nil {
			return chatHandler
		}
		logger.Warnf(ctx, "unable to get chat handler")
		select {
		case <-k.CloseCtx.Done():
			return nil
		case <-ctx.Done():
			return nil
		}
	}
}

func (k *Kick) GetChatMessagesChan(
	ctx context.Context,
) (<-chan streamcontrol.Event, error) {
	logger.Debugf(ctx, "GetChatMessagesChan")
	defer func() { logger.Debugf(ctx, "/GetChatMessagesChan") }()

	if err := k.prepare(ctx); err != nil {
		return nil, fmt.Errorf("unable to get a prepared client: %w", err)
	}

	outCh := make(chan streamcontrol.Event)
	observability.Go(ctx, func(ctx context.Context) {
		defer func() {
			logger.Debugf(ctx, "closing the messages channel")
			close(outCh)
		}()
		for {
			err := k.prepare(ctx)
			if err != nil {
				logger.Errorf(ctx, "unable to get a prepared client: %w", err)
				time.Sleep(time.Second)
				continue
			}
			break
		}
		logger.Debugf(ctx, "GetChatMessagesChan: client is ready")
		for {
			chatHandler := k.getChatHandler(ctx)
			if chatHandler == nil {
				logger.Debugf(ctx, "getting of chat handler was cancelled: %v %v", ctx.Err(), k.CloseCtx.Err())
				return
			}
			msgCh, err := chatHandler.GetMessagesChan(ctx)
			if err != nil {
				logger.Errorf(ctx, "unable to get messages channel from chat handler: %v", err)
				k.resetChatHandler(ctx)
				time.Sleep(time.Second)
				continue
			}
			logger.Tracef(ctx, "GetChatMessagesChan: waiting for a message")
			select {
			case <-k.CloseCtx.Done():
				return
			case <-ctx.Done():
				return
			case ev, ok := <-msgCh:
				if !ok {
					logger.Debugf(ctx, "the input channel is closed")
					k.resetChatHandler(ctx)
					continue
				}
				logger.Tracef(ctx, "GetChatMessagesChan: received a message")
				outCh <- ev
			}
		}
	})

	return outCh, nil
}

func (k *Kick) resetChatHandler(ctx context.Context) {
	logger.Debugf(ctx, "resetChatHandler")
	defer logger.Debugf(ctx, "/resetChatHandler")
	k.ChatHandlerLocker.Do(ctx, func() {
		if k.ChatHandler == nil {
			return
		}
		h, ok := k.ChatHandler.(*ChatHandlerOBSOLETE)
		if ok {
			logger.Debugf(ctx, "closing existing chat handler")
			if err := h.Close(ctx); err != nil {
				logger.Errorf(ctx, "unable to close the chat handler: %v", err)
			}
			k.ChatHandler = nil
		} else {
			logger.Debugf(ctx, "chat handler does not require resetting")
		}
	})
}

func (k *Kick) SendChatMessage(ctx context.Context, message string) (_err error) {
	logger.Debugf(ctx, "SendChatMessage(ctx, '%s')", message)
	defer func() { logger.Debugf(ctx, "/SendChatMessage(ctx, '%s'): %v", message, _err) }()

	if err := k.prepare(ctx); err != nil {
		return fmt.Errorf("unable to get a prepared client: %w", err)
	}

	resp, err := k.GetClient().SendChatMessage(ctx, ptr(int(k.Channel.UserID)), message, nil, gokick.MessageTypeUser)
	logger.Debugf(ctx, "SendChatMessage(ctx, '%s'): %#+v", message, resp)
	return err
}

func (k *Kick) RemoveChatMessage(ctx context.Context, messageID streamcontrol.EventID) error {
	logger.Warnf(ctx, "not implemented yet")
	return nil
}

func (k *Kick) BanUser(
	ctx context.Context,
	userID streamcontrol.UserID,
	reason string,
	deadline time.Time,
) (_err error) {
	logger.Debugf(ctx, "BanUser(ctx, %d, '%s', %v): %#+v", userID, reason, deadline)
	defer func() { logger.Debugf(ctx, "/BanUser(ctx, %d, '%s', %v): %#+v", userID, reason, deadline, _err) }()

	if err := k.prepare(ctx); err != nil {
		return fmt.Errorf("unable to get a prepared client: %w", err)
	}

	userIDInt, err := strconv.ParseInt(string(userID), 10, 64)
	if err != nil {
		return fmt.Errorf("unable to convert the user ID: %w", err)
	}
	var reasonPtr *string
	if reason != "" {
		reasonPtr = &reason
	}
	var duration *int
	if !deadline.IsZero() {
		duration = ptr(int(time.Until(deadline).Minutes() + 0.5))
		if *duration <= 0 {
			logger.Warnf(ctx, "the requested ban interval is not greater than zero: %d", *duration)
			return nil
		}
	}
	resp, err := k.GetClient().BanUser(ctx, int(k.Channel.UserID), int(userIDInt), duration, reasonPtr)
	logger.Debugf(ctx, "BanUser(ctx, %d, '%s', %v): %#+v", userID, reason, deadline, resp)
	return err
}

func (k *Kick) ApplyProfile(
	ctx context.Context,
	profile StreamProfile,
	customArgs ...any,
) (_err error) {
	logger.Debugf(ctx, "ApplyProfile")
	defer func() { logger.Debugf(ctx, "/ApplyProfile: %v", _err) }()

	if err := k.prepare(ctx); err != nil {
		return fmt.Errorf("unable to get a prepared client: %w", err)
	}

	var result []error

	if profile.CategoryID != nil {
		logger.Debugf(ctx, "has a CategoryID")
		_, err := k.GetClient().UpdateStreamCategory(ctx, int(*profile.CategoryID))
		if err != nil {
			result = append(result, fmt.Errorf("unable to update the category: %w", err))
		}
	}

	return errors.Join(result...)
}

func (k *Kick) StartStream(
	ctx context.Context,
	title string,
	description string,
	profile StreamProfile,
	customArgs ...any,
) (_err error) {
	logger.Debugf(ctx, "StartStream")
	defer func() { logger.Debugf(ctx, "/StartStream: %v", _err) }()

	if err := k.prepare(ctx); err != nil {
		return fmt.Errorf("unable to get a prepared client: %w", err)
	}

	var result []error
	if err := k.SetTitle(ctx, title); err != nil {
		result = append(result, fmt.Errorf("unable to set title: %w", err))
	}
	if err := k.SetDescription(ctx, description); err != nil {
		result = append(result, fmt.Errorf("unable to set description: %w", err))
	}
	if err := k.ApplyProfile(ctx, profile, customArgs...); err != nil {
		result = append(
			result,
			fmt.Errorf("unable to apply the stream-specific profile: %w", err),
		)
	}
	return errors.Join(result...)
}

func (k *Kick) prepare(ctx context.Context) (_err error) {
	logger.Tracef(ctx, "prepare")
	defer func() { logger.Tracef(ctx, "/prepare: %v", _err) }()
	return xsync.DoA1R1(ctx, &k.PrepareLocker, k.prepareNoLock, ctx)
}

func (k *Kick) prepareNoLock(ctx context.Context) error {
	if k == nil {
		return fmt.Errorf("k == nil")
	}
	if k.ClientOBSOLETE == nil {
		return fmt.Errorf("k.ClientOBSOLETE == nil")
	}

	err := k.getAccessTokenIfNeeded(ctx)
	if err != nil {
		return fmt.Errorf("getAccessTokenIfNeeded: %w", err)
	}

	k.lazyInitOnce.Do(func() {
		if err = k.initChannelInfo(ctx); err != nil {
			err = fmt.Errorf("initChannelInfo: %w", err)
			return
		}
		observability.Go(ctx, func(ctx context.Context) {
			k.subscribeToEvents(ctx)
		})
	})
	return err
}

func (k *Kick) initChannelInfo(
	ctx context.Context,
) error {
	//var channel *gokick.ChannelResponse
	cache := CacheFromCtx(ctx)
	if chanInfo := cache.GetChanInfo(); chanInfo != nil && chanInfo.Slug == k.CurrentConfig.Config.Channel {
		logger.Debugf(ctx, "reuse the cache, instead of querying channel info")
		k.Channel = chanInfo
		return nil
	}

	for {
		slug := k.CurrentConfig.Config.Channel
		chanInfo, err := k.ClientOBSOLETE.GetChannelV1(ctx, slug)
		//channelResp, err := k.Client.GetChannels(ctx, gokick.NewChannelListFilter().SetSlug([]string{slug}))
		if err != nil {
			logger.Errorf(ctx, "unable to get the channel info (slug: '%s'): %v", slug, err)
			time.Sleep(time.Second)
			continue
		}
		/*if len(channelResp.Result) != 1 {
			logger.Errorf(ctx, "expected to find one channel with name '%s', but found %d", slug, len(channelResp.Result))
			time.Sleep(30 * time.Second)
			continue
		}
		channel = &channelResp.Result[0]*/
		if cache != nil {
			cache.SetChanInfo(chanInfo)
		}
		k.Channel = chanInfo
		return nil
	}
}

func (k *Kick) getAccessTokenIfNeeded(
	ctx context.Context,
) (_err error) {
	logger.Tracef(ctx, "getAccessTokenIfNeeded")
	defer func() { logger.Tracef(ctx, "/getAccessTokenIfNeeded: %v", _err) }()

	if time.Now().After(k.CurrentConfig.Config.UserAccessTokenExpiresAt.Add(-30 * time.Second)) {
		if k.CurrentConfig.Config.RefreshToken.Get() != "" {
			if err := k.refreshAccessToken(ctx); err != nil {
				return fmt.Errorf("unable to refresh the access token: %w", err)
			}
		}
	}

	if k.CurrentConfig.Config.UserAccessToken.Get() != "" {
		return nil
	}

	err := k.getAccessToken(ctx)
	if err != nil {
		return fmt.Errorf("unable to get access token: %w", err)
	}

	return nil
}

func (k *Kick) refreshAccessToken(
	ctx context.Context,
) (_err error) {
	logger.Tracef(ctx, "refreshAccessToken")
	defer func() { logger.Tracef(ctx, "/refreshAccessToken: %v", _err) }()

	resp, err := k.GetClient().RefreshToken(ctx, k.CurrentConfig.Config.RefreshToken.Get())
	if err != nil {
		logger.Errorf(ctx, "unable to refresh the token: %v", err)
		if getErr := k.getAccessToken(ctx); getErr != nil {
			return fmt.Errorf("unable to refresh access token (%w); and unable to get a new access token (%w)", err, getErr)
		}
		return nil
	}

	err = k.setToken(ctx, resp, time.Now())
	if err != nil {
		return fmt.Errorf("unable to set access token: %w", err)
	}

	return nil
}

func (k *Kick) IsCapable(
	ctx context.Context,
	cap streamcontrol.Capability,
) bool {
	switch cap {
	case streamcontrol.CapabilitySendChatMessage:
		return true
	case streamcontrol.CapabilityDeleteChatMessage:
		return false
	case streamcontrol.CapabilityBanUser:
		return true
	case streamcontrol.CapabilityShoutout:
		return true
	case streamcontrol.CapabilityIsChannelStreaming:
		return false
	case streamcontrol.CapabilityRaid:
		return false
	}
	return false
}

func (k *Kick) IsChannelStreaming(
	ctx context.Context,
	chanID streamcontrol.UserID,
) (_ret bool, _err error) {
	logger.Debugf(ctx, "IsChannelStreaming(ctx, '%s')", chanID)
	defer func() { logger.Debugf(ctx, "/IsChannelStreaming(ctx, '%s'): %v", chanID, _ret, _err) }()

	if err := k.prepare(ctx); err != nil {
		return false, fmt.Errorf("unable to get a prepared client: %w", err)
	}

	return false, fmt.Errorf("not implemented")
}

func (k *Kick) RaidTo(
	ctx context.Context,
	chanID streamcontrol.UserID,
) (_err error) {
	logger.Debugf(ctx, "RaidTo(ctx, '%s')", chanID)
	defer func() { logger.Debugf(ctx, "/RaidTo(ctx, '%s'): %v", chanID, _err) }()

	if err := k.prepare(ctx); err != nil {
		return fmt.Errorf("unable to get a prepared client: %w", err)
	}

	return fmt.Errorf("not implemented")
}

func (k *Kick) getChanInfoViaOldClient(
	ctx context.Context,
	idOrLogin streamcontrol.UserID,
) (_ret *gokick.ChannelResponse, _err error) {
	logger.Debugf(ctx, "getChanInfoViaOldClient(ctx, '%s')")
	defer func() { logger.Debugf(ctx, "/getChanInfoViaOldClient(ctx, '%s'): %v %v", _ret, _err) }()
	chanInfo, err := k.ClientOBSOLETE.GetChannelV1(ctx, string(idOrLogin))
	if err != nil {
		return nil, fmt.Errorf("unable to get chan info of '%s': %w", idOrLogin, err)
	}

	result := &gokick.ChannelResponse{
		BannerPicture:     chanInfo.BannerImage.URL,
		BroadcasterUserID: int(chanInfo.UserID),
		Slug:              chanInfo.Slug,
		StreamTitle:       chanInfo.Livestream.SessionTitle,
	}
	if len(chanInfo.RecentCategories) > 0 {
		cat := chanInfo.RecentCategories[0]
		result.Category = gokick.CategoryResponse{
			ID:        int(cat.ID),
			Name:      cat.Name,
			Thumbnail: cat.Category.Icon,
		}
	}

	return result, nil
}

func (k *Kick) getChanInfo(
	ctx context.Context,
	idOrLogin streamcontrol.UserID,
) (_ret *gokick.ChannelResponse, _err error) {
	logger.Debugf(ctx, "getChanInfo(ctx, '%s')")
	defer func() { logger.Debugf(ctx, "/getChanInfo(ctx, '%s'): %v %v", _ret, _err) }()

	id, idConvErr := strconv.ParseInt(string(idOrLogin), 10, 64)

	client := k.GetClient()
	if client == nil {
		err := fmt.Errorf("kick client is not initialized")
		if idConvErr != nil {
			logger.Errorf(ctx, "%v", err)
			return k.getChanInfoViaOldClient(ctx, idOrLogin)
		}
		return nil, err
	}

	if idConvErr == nil {
		resp, err := client.GetChannels(ctx, gokick.NewChannelListFilter().SetBroadcasterUserIDs([]int{int(id)}))
		if err != nil {
			return nil, fmt.Errorf("unable to request channel info by id %d: %w", id, err)
		}
		if len(resp.Result) != 0 {
			return &resp.Result[0], nil
		}
	}

	resp, err := client.GetChannels(ctx, gokick.NewChannelListFilter().SetSlug([]string{string(idOrLogin)}))
	if err != nil {
		logger.Errorf(ctx, "unable to request channel info by slug '%s': %v", idOrLogin, err)
		return k.getChanInfoViaOldClient(ctx, idOrLogin) // TODO: use an multierror to combine errors from both variants
	}
	if len(resp.Result) == 0 {
		return nil, fmt.Errorf("user with slug or ID '%s' is not found", idOrLogin)
	}

	return &resp.Result[0], nil
}

func (k *Kick) Shoutout(
	ctx context.Context,
	idOrLogin streamcontrol.UserID,
) (_err error) {
	logger.Debugf(ctx, "Shoutout(ctx, '%s')", idOrLogin)
	defer func() { logger.Debugf(ctx, "/Shoutout(ctx, '%s'): %v", idOrLogin, _err) }()

	if err := k.prepare(ctx); err != nil {
		return fmt.Errorf("unable to get a prepared client: %w", err)
	}

	chanInfo, err := k.getChanInfo(ctx, idOrLogin)
	if err != nil {
		return fmt.Errorf("unable to get channel info ('%s'): %w", idOrLogin, err)
	}

	return k.sendShoutoutMessage(ctx, *chanInfo)
}

func (k *Kick) sendShoutoutMessage(
	ctx context.Context,
	chanInfo gokick.ChannelResponse,
) (_err error) {
	logger.Debugf(ctx, "sendShoutoutMessage(ctx, '%s')", spew.Sdump(chanInfo))
	defer func() { logger.Debugf(ctx, "/sendShoutoutMessage(ctx, '%s'): %v", spew.Sdump(chanInfo), _err) }()

	var message []string
	message = append(message, fmt.Sprintf("Shoutout to %s!", chanInfo.Slug))
	if chanInfo.StreamTitle != "" {
		message = append(message, fmt.Sprintf("Their latest stream: '%s'.", chanInfo.StreamTitle))
	}
	message = append(message, fmt.Sprintf("Take a look at their channel and click that follow button! https://kick.com/%s", chanInfo.Slug))

	err := k.SendChatMessage(ctx, strings.Join(message, " "))
	if err != nil {
		return fmt.Errorf("unable to send the message (case #1): %w", err)
	}

	return nil
}
