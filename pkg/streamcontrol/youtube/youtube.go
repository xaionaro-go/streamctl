package youtube

import (
	"context"
	"fmt"
	"time"

	"github.com/facebookincubator/go-belt/tool/experimental/errmon"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/oauthhandler"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/option"
	"google.golang.org/api/youtube/v3"
)

type StreamProfile struct {
	Tags []string
}

type YouTube struct {
	YouTubeService *youtube.Service
}

var _ streamctl.StreamController[StreamProfile] = (*YouTube)(nil)

func New(
	ctx context.Context,
	cfg Config,
	safeCfgFn func(Config) error,
) (*YouTube, error) {
	if cfg.Config.ClientID == "" || cfg.Config.ClientSecret == "" {
		return nil, fmt.Errorf("'clientid' or/and 'clientsecret' is/are not set; go to https://console.cloud.google.com/apis/credentials and create an app if it not created, yet")
	}

	if cfg.Config.Token == nil {
		t, err := getToken(ctx, cfg)
		if err != nil {
			return nil, fmt.Errorf("unable to get an access token: %w", err)
		}
		cfg.Config.Token = t
		err = safeCfgFn(cfg)
		errmon.ObserveErrorCtx(ctx, err)
	}

	tokenSource := getAuthCfg(cfg).TokenSource(ctx, cfg.Config.Token)
	youtubeService, err := youtube.NewService(ctx, option.WithTokenSource(tokenSource))
	if err != nil {
		return nil, err
	}

	yt := &YouTube{
		YouTubeService: youtubeService,
	}

	go func() {
		ticker := time.NewTicker(time.Minute)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				logger.Debugf(ctx, "checking if the token changed")
				token, err := tokenSource.Token()
				if err != nil {
					logger.Errorf(ctx, "unable to get a token: %v", err)
					continue
				}
				if token.AccessToken == cfg.Config.Token.AccessToken {
					logger.Debugf(ctx, "the token have not change")
					continue
				}
				logger.Debugf(ctx, "the token have changed")
				cfg.Config.Token = token
				err = safeCfgFn(cfg)
				errmon.ObserveErrorCtx(ctx, err)
			}
		}
	}()

	return yt, nil
}

func getAuthCfg(cfg Config) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     cfg.Config.ClientID,
		ClientSecret: cfg.Config.ClientSecret,
		Endpoint:     google.Endpoint,
		RedirectURL:  "http://localhost:8090",
		Scopes: []string{
			"https://www.googleapis.com/auth/youtube",
		},
	}
}

func getToken(ctx context.Context, cfg Config) (*oauth2.Token, error) {
	googleAuthCfg := getAuthCfg(cfg)
	authURL := googleAuthCfg.AuthCodeURL("state-token", oauth2.AccessTypeOffline)

	var tok *oauth2.Token
	oauthHandler := oauthhandler.NewOAuth2Handler(authURL, func(code string) error {
		_tok, err := googleAuthCfg.Exchange(ctx, code)
		if err != nil {
			return fmt.Errorf("unable to get a token: %w", err)
		}
		tok = _tok
		return nil
	}, "")
	err := oauthHandler.Handle(ctx)
	if err != nil {
		return nil, err
	}

	return tok, nil
}

func (yt *YouTube) iterateActiveBroadcasts(
	ctx context.Context,
	callback func(broadcast *youtube.LiveBroadcast) error,
	parts ...string,
) error {
	broadcasts, err := yt.YouTubeService.LiveBroadcasts.
		List(append([]string{"id"}, parts...)).
		BroadcastStatus("active").
		Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("unable to get the list of active broadcasts: %w", err)
	}

	for _, broadcast := range broadcasts.Items {
		if err := callback(broadcast); err != nil {
			if err != nil {
				return fmt.Errorf("got an error with broadcast %v: %w", broadcast.Id, err)
			}
		}
	}
	return nil
}

func (yt *YouTube) updateActiveBroadcasts(
	ctx context.Context,
	updateBroadcast func(broadcast *youtube.LiveBroadcast) error,
	parts ...string,
) error {
	return yt.iterateActiveBroadcasts(ctx, func(broadcast *youtube.LiveBroadcast) error {
		if err := updateBroadcast(broadcast); err != nil {
			return fmt.Errorf("unable to update broadcast %v: %w", broadcast.Id, err)
		}
		_, err := yt.YouTubeService.LiveBroadcasts.Update(parts, broadcast).Context(ctx).Do()
		if err != nil {
			return fmt.Errorf("unable to update broadcast %v: %w", broadcast.Id, err)
		}
		return nil
	}, parts...)
}

func (yt *YouTube) ApplyProfile(
	ctx context.Context,
	profile StreamProfile,
) error {
	return yt.updateActiveBroadcasts(ctx, func(broadcast *youtube.LiveBroadcast) error {
		if broadcast.Snippet == nil {
			return fmt.Errorf("YouTube have not provided the current snippet of broadcast %v", broadcast.Id)
		}
		setProfile(broadcast, profile)
		return nil
	}, "snippet")
}

func (yt *YouTube) SetTitle(
	ctx context.Context,
	title string,
) error {
	return yt.updateActiveBroadcasts(ctx, func(broadcast *youtube.LiveBroadcast) error {
		if broadcast.Snippet == nil {
			return fmt.Errorf("YouTube have not provided the current snippet of broadcast %v", broadcast.Id)
		}
		setTitle(broadcast, title)
		return nil
	}, "snippet")
}

func (yt *YouTube) SetDescription(
	ctx context.Context,
	description string,
) error {
	return yt.updateActiveBroadcasts(ctx, func(broadcast *youtube.LiveBroadcast) error {
		if broadcast.Snippet == nil {
			return fmt.Errorf("YouTube have not provided the current snippet of broadcast %v", broadcast.Id)
		}
		setDescription(broadcast, description)
		return nil
	}, "snippet")
}

func (yt *YouTube) InsertAdsCuePoint(
	ctx context.Context,
	ts time.Time,
	duration time.Duration,
) error {
	return yt.iterateActiveBroadcasts(ctx, func(broadcast *youtube.LiveBroadcast) error {
		_, err := yt.YouTubeService.LiveBroadcasts.InsertCuepoint(&youtube.Cuepoint{
			CueType:      "cueTypeAd",
			DurationSecs: int64(duration.Seconds()),
			WalltimeMs:   uint64(ts.UnixMilli()),
		}).Context(ctx).Do()
		return err
	})
}

type FlagBroadcastTemplateIDs []string

func (yt *YouTube) StartStream(
	ctx context.Context,
	title string,
	description string,
	profile StreamProfile,
	customArgs ...any,
) error {
	var templateBroadcastIDs []string
	for _, templateBroadcastIDCandidate := range customArgs {
		_templateBroadcastIDs, ok := templateBroadcastIDCandidate.(FlagBroadcastTemplateIDs)
		if ok {
			templateBroadcastIDs = _templateBroadcastIDs
			break
		}
	}

	var broadcasts []*youtube.LiveBroadcast
	for _, templateBroadcastID := range templateBroadcastIDs {
		response, err := yt.YouTubeService.LiveBroadcasts.
			List([]string{"id", "snippet", "contentDetails", "monetizationDetails", "status"}).
			Id(templateBroadcastID).
			Context(ctx).Do()
		if err != nil {
			return fmt.Errorf("unable to get the list of active broadcasts: %w", err)
		}
		if len(response.Items) != 1 {
			return fmt.Errorf("expected 1 broadcast with id %v, but found %d", templateBroadcastID, len(response.Items))
		}
		broadcasts = append(broadcasts, response.Items...)
	}

	for _, broadcast := range broadcasts {
		broadcast.ContentDetails.EnableAutoStart = true
		broadcast.ContentDetails.EnableAutoStop = false
		broadcast.Snippet.ScheduledStartTime = time.Now().UTC().Format("2006-01-02T15:04:05") + ".00Z"
		broadcast.Snippet.ScheduledEndTime = time.Now().Add(time.Hour*12).UTC().Format("2006-01-02T15:04:05") + ".00Z"

		setTitle(broadcast, title)
		setDescription(broadcast, description)
		setProfile(broadcast, profile)
		_, err := yt.YouTubeService.LiveBroadcasts.Insert([]string{"snippet", "contentDetails"}, broadcast).Context(ctx).Do()
		if err != nil {
			return fmt.Errorf("unable to create a broadcast: %w", err)
		}
	}

	return nil
}

func setTitle(broadcast *youtube.LiveBroadcast, title string) {
	broadcast.Snippet.Title = title
}

func setDescription(broadcast *youtube.LiveBroadcast, description string) {
	broadcast.Snippet.Description = description
}

func setProfile(broadcast *youtube.LiveBroadcast, profile StreamProfile) {
	// Don't know how to set the tags :(
}

func (yt *YouTube) EndStream(
	ctx context.Context,
) error {
	return yt.updateActiveBroadcasts(ctx, func(broadcast *youtube.LiveBroadcast) error {
		broadcast.ContentDetails.EnableAutoStop = true
		return nil
	}, "contentDetails")
}

func (yt *YouTube) Flush(
	ctx context.Context,
) error {
	// Unfortunately, we do not support sending accumulated changes, and we change things immediately right away.
	// So nothing to do here:
	return nil
}
