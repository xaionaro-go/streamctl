package kick

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"sync"
	"time"

	http "github.com/Danny-Dasilva/fhttp"
	"github.com/davecgh/go-spew/spew"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/google/uuid"
	"github.com/scorfly/gokick"
	"github.com/xaionaro-go/kickcom"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/streamctl/pkg/oauthhandler"
	"github.com/xaionaro-go/streamctl/pkg/secret"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/xsync"
)

type ReverseEngClient interface {
	ChatClient
	LivestreamInfoClient
}

type LivestreamInfoClient interface {
	GetLivestreamV2(
		ctx context.Context,
		channelSlug string,
	) (*kickcom.LivestreamV2Reply, error)
}

type Kick struct {
	CloseCtx         context.Context
	CloseFn          context.CancelFunc
	Channel          *kickcom.ChannelV1
	Client           *gokick.Client
	ReverseEngClient ReverseEngClient
	ChatHandler      *ChatHandler
	CurrentConfig    Config
	SaveCfgFn        func(Config) error
	PrepareLocker    xsync.Mutex

	lazyInitOnce sync.Once
}

var _ streamcontrol.StreamController[StreamProfile] = (*Kick)(nil)

func New(
	ctx context.Context,
	cfg Config,
	saveCfgFn func(Config) error,
) (*Kick, error) {
	if cfg.Config.Channel == "" {
		return nil, fmt.Errorf("channel is not set")
	}

	reverseEngClient, err := kickcom.New()
	if err != nil {
		return nil, fmt.Errorf("unable to initialize a client to Kick: %w", err)
	}

	client, err := gokick.NewClient(&gokick.ClientOptions{
		UserAccessToken: cfg.Config.UserAccessToken.Get(),
		ClientID:        cfg.Config.ClientID,
		ClientSecret:    cfg.Config.ClientSecret.Get(),
	})
	if err != nil {
		return nil, fmt.Errorf("unable to initialize a client to Kick: %w", err)
	}

	var channel *kickcom.ChannelV1
	cache := CacheFromCtx(ctx)
	if chanInfo := cache.GetChanInfo(); chanInfo != nil && chanInfo.Slug == cfg.Config.Channel {
		channel = cache.ChanInfo
		logger.Debugf(ctx, "reuse the cache, instead of querying channel info")
	} else {
		for i := 0; i < 10; i++ {
			channel, err = reverseEngClient.GetChannelV1(ctx, cfg.Config.Channel)
			if err != nil {
				err = fmt.Errorf("unable to obtain channel info: %w", err)
				time.Sleep(time.Second)
				continue
			}
		}
		if err != nil {
			return nil, err
		}
		if cache != nil {
			cache.SetChanInfo(channel)
		}
	}

	ctx, closeFn := context.WithCancel(ctx)
	k := &Kick{
		CloseCtx:         ctx,
		CloseFn:          closeFn,
		CurrentConfig:    cfg,
		Client:           client,
		ReverseEngClient: reverseEngClient,
		Channel:          channel,
		SaveCfgFn:        saveCfgFn,
	}

	chatHandler, err := k.newChatHandler(ctx, channel.ID)
	if err != nil {
		return nil, fmt.Errorf("unable to initialize a chat handler: %w", err)
	}
	k.ChatHandler = chatHandler

	return k, nil
}

func authRedirectURI(listenPort uint16) string {
	return fmt.Sprintf("http://localhost:%d/", listenPort)
}

func (k *Kick) oauthHandlerFunc() OAuthHandler {
	oauthHandler := k.CurrentConfig.Config.CustomOAuthHandler
	if oauthHandler == nil {
		return oauthhandler.OAuth2HandlerViaCLI
	}
	return oauthHandler
}

func (k *Kick) getAccessToken(
	ctx context.Context,
) (_err error) {
	logger.Tracef(ctx, "getAccessToken")
	defer logger.Tracef(ctx, "/getAccessToken: %v", _err)

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
		return fmt.Errorf("the function GetOAuthListenPorts returned less than 2 ports")
	}

	listenPort := oauthPorts[1] // TODO: remove this hardcode [1]; it currently exists to use the different port from what we use for Twitch authentication

	codeVerifier := uuid.New().String() // random string
	codeVerifierSHA256 := sha256.Sum256([]byte(codeVerifier))
	codeChallenge := base64.URLEncoding.EncodeToString(codeVerifierSHA256[:])

	authURL, err := k.Client.GetAuthorizeEndpoint(
		authRedirectURI(listenPort),
		"",
		codeChallenge,
		[]gokick.Scope{
			gokick.ScopeUserRead,
			gokick.ScopeChannelRead,
			gokick.ScopeChannelWrite,
			gokick.ScopeChatWrite,
			gokick.ScopeStremkeyRead,
			gokick.ScopeEventSubscribe,
		},
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
			token, err := k.Client.GetToken(ctx, authRedirectURI(listenPort), code, codeVerifier)
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
	defer logger.Tracef(ctx, "/setToken: %v", _err)
	k.CurrentConfig.Config.UserAccessToken.Set(token.AccessToken)
	k.CurrentConfig.Config.UserAccessTokenExpiresAt = now.Add(time.Second * time.Duration(token.ExpiresIn))
	k.CurrentConfig.Config.RefreshToken.Set(token.RefreshToken)
	err := k.SaveCfgFn(k.CurrentConfig)
	if err != nil {
		return fmt.Errorf("unable to save the config: %w", err)
	}
	return nil
}

func (k *Kick) refreshAccessToken(
	ctx context.Context,
) (_err error) {
	logger.Tracef(ctx, "refreshAccessToken")
	defer logger.Tracef(ctx, "/refreshAccessToken: %v", _err)
	if k.CurrentConfig.Config.UserAccessTokenExpiresAt.After(time.Now()) {
		logger.Debugf(ctx, "the refresh token is expired; requesting a new token from scratch")
		return k.getAccessToken(ctx)
	}
	now := time.Now()
	token, err := k.Client.RefreshToken(ctx, k.CurrentConfig.Config.RefreshToken.Get())
	if err != nil {
		logger.Errorf(ctx, "unable to refresh the token: %v; so requesting a new token from scratch", err)
		return k.getAccessToken(ctx)
	}
	return k.setToken(ctx, token, now)
}

func (k *Kick) Close() (_err error) {
	ctx := context.Background()
	logger.Debugf(ctx, "Close(ctx)")
	defer logger.Debugf(ctx, "/Close(ctx): %v", _err)
	k.CloseFn()
	return nil
}

func (k *Kick) SetTitle(ctx context.Context, title string) (err error) {
	logger.Debugf(ctx, "SetTitle(ctx, '%s')", title)
	defer logger.Debugf(ctx, "/SetTitle(ctx, '%s'): %v", title, err)

	if err := k.prepare(ctx); err != nil {
		return fmt.Errorf("unable to get a prepared client: %w", err)
	}

	_, err = k.Client.UpdateStreamTitle(ctx, title)
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
	logger.Warnf(ctx, "not implemented yet")
	return nil
}

func (k *Kick) GetStreamStatus(ctx context.Context) (_ret *streamcontrol.StreamStatus, _err error) {
	logger.Debugf(ctx, "GetStreamStatus")
	defer logger.Debugf(ctx, "/GetStreamStatus: %v, %v", _ret, _err)

	info, err := k.ReverseEngClient.GetLivestreamV2(ctx, k.Channel.Slug)
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
	logger.Tracef(ctx, "the received livestream status is: %s", spew.Sdump(info))

	if info.Data == nil {
		return &streamcontrol.StreamStatus{
			IsActive:     false,
			ViewersCount: nil,
			StartedAt:    nil,
			CustomData:   nil,
		}, nil
	}

	return &streamcontrol.StreamStatus{
		IsActive:     true,
		ViewersCount: ptr(uint(info.Data.Viewers)),
		StartedAt:    &info.Data.CreatedAt,
		CustomData:   nil,
	}, nil
}

func (k *Kick) getStreamStatusUsingNormalClient(
	ctx context.Context,
) (_ret *streamcontrol.StreamStatus, _err error) {
	logger.Debugf(ctx, "getStreamStatusUsingNormalClient")
	defer logger.Debugf(ctx, "/getStreamStatusUsingNormalClient: %v %v", _ret, _err)

	resp, err := k.Client.GetChannels(
		ctx,
		gokick.NewChannelListFilter().SetBroadcasterUserIDs([]int{int(k.Channel.ID)}),
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
) (_ret []gokick.CategoryResponse, _err error) {
	logger.Debugf(ctx, "GetAllCategories")
	defer logger.Debugf(ctx, "/GetAllCategories: len:%d, %v", len(_ret), _err)

	if err := k.prepare(ctx); err != nil {
		return nil, fmt.Errorf("unable to get a prepared client: %w", err)
	}

	categories, err := k.Client.GetCategories(ctx, gokick.NewCategoryListFilter())
	if err != nil {
		return nil, fmt.Errorf("unable to get categories: %w", err)
	}

	return categories.Result, nil
}

func (k *Kick) GetChatMessagesChan(
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
			case ev, ok := <-k.ChatHandler.MessagesChan():
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

func (k *Kick) SendChatMessage(ctx context.Context, message string) error {
	logger.Warnf(ctx, "not implemented yet")
	return nil
}

func (k *Kick) RemoveChatMessage(ctx context.Context, messageID streamcontrol.ChatMessageID) error {
	logger.Warnf(ctx, "not implemented yet")
	return nil
}

func (k *Kick) BanUser(
	ctx context.Context,
	userID streamcontrol.ChatUserID,
	reason string,
	deadline time.Time,
) error {
	logger.Warnf(ctx, "not implemented yet")
	return nil
}

func (k *Kick) ApplyProfile(
	ctx context.Context,
	profile StreamProfile,
	customArgs ...any,
) (_err error) {
	logger.Debugf(ctx, "ApplyProfile")
	defer logger.Debugf(ctx, "/ApplyProfile: %v", _err)

	if err := k.prepare(ctx); err != nil {
		return fmt.Errorf("unable to get a prepared client: %w", err)
	}

	var result []error

	if profile.CategoryID != nil {
		logger.Debugf(ctx, "has a CategoryID")
		_, err := k.Client.UpdateStreamCategory(ctx, *profile.CategoryID)
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

func (k *Kick) prepare(ctx context.Context) error {
	logger.Tracef(ctx, "prepare")
	defer logger.Tracef(ctx, "/prepare")
	return xsync.DoA1R1(ctx, &k.PrepareLocker, k.prepareNoLock, ctx)
}

func (k *Kick) prepareNoLock(ctx context.Context) error {
	err := k.getTokenIfNeeded(ctx)
	if err != nil {
		return err
	}

	k.lazyInitOnce.Do(func() {
		// do what's needed
	})
	return err
}

func (k *Kick) getTokenIfNeeded(
	ctx context.Context,
) error {
	if k.CurrentConfig.Config.UserAccessToken.Get() != "" {
		return nil
	}

	err := k.getAccessToken(ctx)
	if err != nil {
		return fmt.Errorf("unable to get access token: %w", err)
	}

	return nil
}

func (k *Kick) IsCapable(
	ctx context.Context,
	cap streamcontrol.Capability,
) bool {
	switch cap {
	case streamcontrol.CapabilitySendChatMessage:
		return false
	case streamcontrol.CapabilityDeleteChatMessage:
		return false
	case streamcontrol.CapabilityBanUser:
		return false
	}
	return false
}

func doRequest[REQ, RESP any](
	ctx context.Context,
	k *Kick,
	fn func(ctx context.Context, req REQ) (RESP, error),
	req REQ,
) (RESP, error) {
	for {
		resp, err := fn(ctx, req)
		if !isInvalidTokenErr(ctx, err) {
			return resp, err
		}

		logger.Infof(ctx, "the token is invalid (%v), re-getting it", err)
		tokenErr := k.getAccessToken(ctx)
		if tokenErr != nil {
			var zeroValue RESP
			return zeroValue, fmt.Errorf("unable to perform the request (%w), because the token is not valid, and was unable to get a new token: %w", err, tokenErr)
		}
		continue
	}
}

func isInvalidTokenErr(
	ctx context.Context,
	errI error,
) (_result bool) {
	logger.Tracef(ctx, "isInvalidTokenErr(ctx, %#+v)", errI)
	defer func() { logger.Debugf(ctx, "/isInvalidTokenErr(ctx, %#+v): %v", errI, _result) }()

	if errI == nil {
		return false
	}

	err, ok := errI.(gokick.Error)
	if !ok {
		return false
	}

	if err.Code() == http.StatusUnauthorized {
		return true
	}

	return false
}
