package auth

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/facebookincubator/go-belt/tool/experimental/errmon"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/hashicorp/go-multierror"
	"github.com/nicklaw5/helix/v2"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/streamctl/pkg/oauthhandler"
)

type OAuthHandler func(context.Context, oauthhandler.OAuthHandlerArgument) error

func getScopes() []string {
	scopes := map[string]struct{}{
		"chat:read": {},
		"chat:edit": {},

		"analytics:read:extensions": {},
		"analytics:read:games":      {},

		"bits:read": {},

		"channel:bot":                {},
		"channel:manage:ads":         {},
		"channel:read:ads":           {},
		"channel:manage:broadcast":   {},
		"channel:read:charity":       {},
		"channel:edit:commercial":    {},
		"channel:read:editors":       {},
		"channel:manage:extensions":  {},
		"channel:read:goals":         {},
		"channel:read:guest_star":    {},
		"channel:manage:guest_star":  {},
		"channel:read:hype_train":    {},
		"channel:manage:moderators":  {},
		"channel:read:polls":         {},
		"channel:manage:polls":       {},
		"channel:read:predictions":   {},
		"channel:manage:predictions": {},
		"channel:manage:raids":       {},
		"channel:read:redemptions":   {},
		"channel:manage:redemptions": {},
		"channel:manage:schedule":    {},
		"channel:read:stream_key":    {},
		"channel:read:subscriptions": {},
		"channel:manage:videos":      {},
		"channel:read:vips":          {},
		"channel:manage:vips":        {},
		"channel:moderate":           {},

		"clips:edit": {},

		"moderation:read": {},

		"moderator:manage:announcements":    {},
		"moderator:manage:automod":          {},
		"moderator:read:automod_settings":   {},
		"moderator:manage:automod_settings": {},
		"moderator:read:banned_users":       {},
		"moderator:manage:banned_users":     {},
		"moderator:read:blocked_terms":      {},
		"moderator:read:chat_messages":      {},
		"moderator:manage:blocked_terms":    {},
		"moderator:manage:chat_messages":    {},
		"moderator:read:chat_settings":      {},
		"moderator:manage:chat_settings":    {},
		"moderator:read:chatters":           {},
		"moderator:read:followers":          {},
		"moderator:read:guest_star":         {},
		"moderator:manage:guest_star":       {},
		"moderator:read:moderators":         {},
		"moderator:read:shield_mode":        {},
		"moderator:manage:shield_mode":      {},
		"moderator:read:shoutouts":          {},
		"moderator:manage:shoutouts":        {},
		"moderator:read:suspicious_users":   {},
		"moderator:read:unban_requests":     {},
		"moderator:manage:unban_requests":   {},
		"moderator:read:vips":               {},
		"moderator:read:warnings":           {},
		"moderator:manage:warnings":         {},

		"user:bot":                     {},
		"user:edit":                    {},
		"user:edit:broadcast":          {},
		"user:read:blocked_users":      {},
		"user:manage:blocked_users":    {},
		"user:read:broadcast":          {},
		"user:read:chat":               {},
		"user:manage:chat_color":       {},
		"user:read:email":              {},
		"user:read:emotes":             {},
		"user:read:follows":            {},
		"user:read:moderated_channels": {},
		"user:read:subscriptions":      {},
		"user:read:whispers":           {},
		"user:manage:whispers":         {},
		"user:write:chat":              {},
	}

	scopesStrings := slices.Collect(maps.Keys(scopes))
	sort.Strings(scopesStrings)
	return scopesStrings
}

func NewClientCode(
	ctx context.Context,
	clientID string,
	oauthHandler OAuthHandler,
	getOAuthListenPortsFn func() []uint16,
	onNewClientCode func(string),
) (_err error) {
	logger.Debugf(ctx, "getNewClientCode")
	defer func() { logger.Debugf(ctx, "/getNewClientCode: %v", _err) }()

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
	observability.Go(ctx, func(ctx context.Context) {
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
			observability.Go(ctx, func(ctx context.Context) {
				defer func() { logger.Debugf(ctx, "ended the oauth handler at port %d", listenPort) }()
				defer wg.Done()
				authURL := GetAuthorizationURL(
					&helix.AuthorizationURLParams{
						ResponseType: "code", // or "token"
						Scopes:       getScopes(),
					},
					clientID,
					RedirectURI(listenPort),
				)

				arg := oauthhandler.OAuthHandlerArgument{
					AuthURL:    authURL,
					ListenPort: listenPort,
					ExchangeFn: func(ctx context.Context, code string) (_err error) {
						logger.Debugf(ctx, "ExchangeFn()")
						defer func() { logger.Debugf(ctx, "/ExchangeFn(): %v", _err) }()
						if code == "" {
							return fmt.Errorf("code is empty")
						}
						onNewClientCode(code)
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
	getPortsFn := getOAuthListenPortsFn
	if getPortsFn == nil {
		return fmt.Errorf("the function GetOAuthListenPorts is not set")
	}

	for _, listenPort := range getPortsFn() {
		startHandlerForPort(listenPort)
	}

	wg.Add(1)
	observability.Go(ctx, func(ctx context.Context) {
		defer wg.Done()
		t := time.NewTicker(time.Second)
		defer t.Stop()
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

	observability.Go(ctx, func(ctx context.Context) {
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

func RedirectURI(listenPort uint16) string {
	return fmt.Sprintf("http://localhost:%d/", listenPort)
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
