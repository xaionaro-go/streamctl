package youtube

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/facebookincubator/go-belt/tool/experimental/errmon"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/go-yaml/yaml"
	"github.com/xaionaro-go/streamctl/pkg/oauthhandler"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/option"
	"google.golang.org/api/youtube/v3"
)

type YouTube struct {
	YouTubeService *youtube.Service
}

var _ streamcontrol.StreamController[StreamProfile] = (*YouTube)(nil)

func New(
	ctx context.Context,
	cfg Config,
	safeCfgFn func(Config) error,
) (*YouTube, error) {
	if cfg.Config.ClientID == "" || cfg.Config.ClientSecret == "" {
		return nil, fmt.Errorf("'clientid' or/and 'clientsecret' is/are not set; go to https://console.cloud.google.com/apis/credentials and create an app if it not created, yet")
	}

	isNewToken := false
	getNewToken := func() error {
		t, err := getToken(ctx, cfg)
		if err != nil {
			return fmt.Errorf("unable to get an access token: %w", err)
		}
		cfg.Config.Token = t
		err = safeCfgFn(cfg)
		errmon.ObserveErrorCtx(ctx, err)
		isNewToken = true
		return nil
	}

	if cfg.Config.Token == nil {
		err := getNewToken()
		if err != nil {
			return nil, err
		}
	}

	tokenSource := getAuthCfg(cfg).TokenSource(ctx, cfg.Config.Token)

	checkToken := func() error {
		logger.Debugf(ctx, "checking if the token changed")
		token, err := tokenSource.Token()
		if err != nil {
			logger.Errorf(ctx, "unable to get a token: %v", err)
			return err
		}
		if token.AccessToken == cfg.Config.Token.AccessToken {
			logger.Debugf(ctx, "the token have not change")
			return err
		}
		logger.Debugf(ctx, "the token have changed")
		cfg.Config.Token = token
		return safeCfgFn(cfg)
	}

	if !isNewToken {
		if err := checkToken(); err != nil {
			logger.Errorf(ctx, "unable to get a token: %v", err)
			err := getNewToken()
			if err != nil {
				return nil, err
			}
			tokenSource = getAuthCfg(cfg).TokenSource(ctx, cfg.Config.Token)
		}
	}

	if err := checkToken(); err != nil {
		return nil, fmt.Errorf("the token is invalid: %w", err)
	}

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
				err := checkToken()
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

	var tok *oauth2.Token
	oauthHandlerArg := oauthhandler.OAuthHandlerArgument{
		AuthURL:     googleAuthCfg.AuthCodeURL("state-token", oauth2.AccessTypeOffline),
		RedirectURL: googleAuthCfg.RedirectURL,
		ExchangeFn: func(code string) error {
			_tok, err := googleAuthCfg.Exchange(ctx, code)
			if err != nil {
				return fmt.Errorf("unable to get a token: %w", err)
			}
			tok = _tok
			return nil
		},
	}

	oauthHandler := cfg.Config.CustomOAuthHandler
	if oauthHandler == nil {
		oauthHandler = oauthhandler.OAuth2HandlerViaCLI
	}
	err := oauthHandler(ctx, oauthHandlerArg)
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
			return fmt.Errorf("got an error with broadcast %v: %w", broadcast.Id, err)
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
	customArgs ...any,
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

var videoParts = []string{
	"contentDetails",
	"fileDetails",
	"id",
	"liveStreamingDetails",
	"localizations",
	"player",
	"processingDetails",
	"recordingDetails",
	"snippet",
	"statistics",
	"status",
	"suggestions",
	"topicDetails",
}

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

	templateBroadcastIDs = append(templateBroadcastIDs, profile.TemplateBroadcastIDs...)
	logger.Debugf(ctx, "templateBroadcastIDs == %v; customArgs == %v", templateBroadcastIDs, customArgs)

	var broadcasts []*youtube.LiveBroadcast
	var videos []*youtube.Video
	if len(templateBroadcastIDs) > 0 {
		{
			logger.Debugf(ctx, "getting broadcast info of %v", templateBroadcastIDs)

			response, err := yt.YouTubeService.LiveBroadcasts.
				List([]string{"id", "snippet", "contentDetails", "monetizationDetails", "status"}).
				Id(templateBroadcastIDs...).
				Context(ctx).Do()
			if err != nil {
				return fmt.Errorf("unable to get the list of active broadcasts: %w", err)
			}
			if len(response.Items) != len(templateBroadcastIDs) {
				return fmt.Errorf("expected %d broadcasts, but found %d", len(templateBroadcastIDs), len(response.Items))
			}
			broadcasts = append(broadcasts, response.Items...)
		}

		{
			logger.Debugf(ctx, "getting video info of %v", templateBroadcastIDs)

			response, err := yt.YouTubeService.Videos.List(videoParts).Id(templateBroadcastIDs...).Context(ctx).Do()
			if err != nil {
				return fmt.Errorf("unable to get the list of active broadcasts: %w", err)
			}
			if len(response.Items) != len(templateBroadcastIDs) {
				return fmt.Errorf("expected %d videos, but found %d", len(templateBroadcastIDs), len(response.Items))
			}
			videos = append(videos, response.Items...)
		}
	}

	for idx, broadcast := range broadcasts {
		video := videos[idx]

		if video.Id != broadcast.Id {
			return fmt.Errorf("internal error: the orders of videos and broadcasts do not match: %s != %s", video.Id, broadcast.Id)
		}
		now := time.Now().UTC()
		broadcast.Id = ""
		broadcast.Etag = ""
		broadcast.ContentDetails.BoundStreamLastUpdateTimeMs = ""
		broadcast.ContentDetails.BoundStreamId = ""
		broadcast.ContentDetails.MonitorStream = nil
		broadcast.Snippet.ScheduledStartTime = now.Format("2006-01-02T15:04:05") + ".00Z"
		broadcast.Snippet.ScheduledEndTime = now.Add(time.Hour*12).Format("2006-01-02T15:04:05") + ".00Z"
		broadcast.Snippet.LiveChatId = ""
		broadcast.Status.SelfDeclaredMadeForKids = broadcast.Status.MadeForKids
		broadcast.Status.ForceSendFields = []string{"SelfDeclaredMadeForKids"}

		setTitle(broadcast, title)
		setDescription(broadcast, description)
		setProfile(broadcast, profile)

		b, err := yaml.Marshal(broadcast)
		if err == nil {
			logger.Tracef(ctx, "creating broadcast %s", b)
		} else {
			logger.Tracef(ctx, "creating broadcast %#+v", broadcast)
		}

		newBroadcast, err := yt.YouTubeService.LiveBroadcasts.Insert(
			[]string{"snippet", "contentDetails", "monetizationDetails", "status"},
			broadcast,
		).Context(ctx).Do()
		if err != nil {
			return fmt.Errorf("unable to create a broadcast: %w", err)
		}

		video.Id = newBroadcast.Id
		video.Snippet.PublishedAt = ""
		video.Status.PublishAt = ""
		video.Snippet.Tags = append(video.Snippet.Tags, profile.Tags...)
		b, err = yaml.Marshal(video)
		if err == nil {
			logger.Tracef(ctx, "updating video data to %s", b)
		} else {
			logger.Tracef(ctx, "updating video data to %#+v", broadcast)
		}
		_, err = yt.YouTubeService.Videos.Update(videoParts, video).Context(ctx).Do()
		if err != nil {
			return fmt.Errorf("unable to update video data: %w", err)
		}

		if broadcast.Snippet.Thumbnails.Standard.Url != "" {
			logger.Debugf(ctx, "downloading the thumbnail")
			resp, err := http.Get(broadcast.Snippet.Thumbnails.Standard.Url)
			if err != nil {
				return fmt.Errorf("unable to download the thumbnail from the template video: %w", err)
			}
			logger.Debugf(ctx, "reading the thumbnail")
			thumbnail, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil {
				return fmt.Errorf("unable to read the thumbnail from the response from the template video: %w", err)
			}
			logger.Debugf(ctx, "setting the thumbnail")
			_, err = yt.YouTubeService.Thumbnails.Set(newBroadcast.Id).Media(bytes.NewReader(thumbnail)).Context(ctx).Do()
			if err != nil {
				return fmt.Errorf("unable to set the thumbnail: %w", err)
			}
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

func (yt *YouTube) ListStreams(
	ctx context.Context,
) ([]*youtube.LiveStream, error) {
	response, err := yt.YouTubeService.
		LiveStreams.
		List([]string{"id", "snippet", "cdn", "status"}).
		Mine(true).
		MaxResults(20).
		Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("unable to query the list of streams: %w", err)
	}
	return response.Items, nil
}

type LiveBroadcast = youtube.LiveBroadcast

func (yt *YouTube) ListBroadcasts(
	ctx context.Context,
) ([]*youtube.LiveBroadcast, error) {
	response, err := yt.YouTubeService.
		LiveBroadcasts.
		List([]string{"id", "snippet", "contentDetails", "monetizationDetails", "status"}).
		Mine(true).
		MaxResults(1000).
		Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("unable to query the list of broadcasts: %w", err)
	}
	return response.Items, nil
}
