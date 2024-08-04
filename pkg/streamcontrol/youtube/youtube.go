package youtube

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/facebookincubator/go-belt/tool/experimental/errmon"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/go-yaml/yaml"
	"github.com/hashicorp/go-multierror"
	"github.com/xaionaro-go/streamctl/pkg/oauthhandler"
	"github.com/xaionaro-go/streamctl/pkg/observability"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"
	"google.golang.org/api/youtube/v3"
)

const copyThumbnail = false

type YouTube struct {
	locker         sync.Mutex
	Config         Config
	YouTubeService *youtube.Service
	CancelFunc     context.CancelFunc
	SaveConfigFunc func(Config) error
}

var _ streamcontrol.StreamController[StreamProfile] = (*YouTube)(nil)

func New(
	ctx context.Context,
	cfg Config,
	saveCfgFn func(Config) error,
) (*YouTube, error) {
	if cfg.Config.ClientID == "" || cfg.Config.ClientSecret == "" {
		return nil, fmt.Errorf("'clientid' or/and 'clientsecret' is/are not set; go to https://console.cloud.google.com/apis/credentials and create an app if it not created, yet")
	}

	ctx, cancelFn := context.WithCancel(ctx)

	yt := &YouTube{
		Config:         cfg,
		SaveConfigFunc: saveCfgFn,
		CancelFunc:     cancelFn,
	}

	err := yt.init(ctx)
	if err != nil {
		return nil, fmt.Errorf("initialization failed: %w", err)
	}

	err = yt.Ping(ctx)
	if err != nil {
		return nil, fmt.Errorf("connection verification failed: %w", err)
	}

	observability.Go(ctx, func() {
		ticker := time.NewTicker(time.Minute)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				err := yt.checkToken(ctx)
				errmon.ObserveErrorCtx(ctx, err)
				if err != nil && strings.Contains(err.Error(), "expired or revoked") {
					_, err := yt.getNewToken(ctx)
					errmon.ObserveErrorCtx(ctx, err)
				}
			}
		}
	})

	return yt, nil
}

func (yt *YouTube) checkToken(ctx context.Context) (_err error) {
	logger.Tracef(ctx, "YouTube.checkToken")
	defer func() { logger.Tracef(ctx, "/YouTube.checkToken: %v", _err) }()

	yt.locker.Lock()
	defer yt.locker.Unlock()
	return yt.checkTokenNoLock(ctx)
}

func (yt *YouTube) checkTokenNoLock(ctx context.Context) (_err error) {
	logger.Tracef(ctx, "YouTube.checkTokenNoLock")
	defer func() { logger.Tracef(ctx, "/YouTube.checkTokenNoLock: %v", _err) }()

	tokenSource := getAuthCfgBase(yt.Config).TokenSource(ctx, yt.Config.Config.Token)
	counter := 0
	for {
		logger.Tracef(ctx, "checking if the token changed")
		token, err := tokenSource.Token()
		if err != nil {
			if yt.fixError(ctx, err, &counter) {
				continue
			}
			return fmt.Errorf("unable to get a token: %v", err)
		}
		if token.AccessToken == yt.Config.Config.Token.AccessToken {
			logger.Tracef(ctx, "the token have not changed")
			return nil
		}
		logger.Tracef(ctx, "the token have changed")
		yt.Config.Config.Token = token
		return yt.SaveConfigFunc(yt.Config)
	}
}

func (yt *YouTube) getNewToken(ctx context.Context) (_ret *oauth2.Token, _err error) {
	logger.Debugf(ctx, "YouTube.getNewToken")
	defer func() { logger.Debugf(ctx, "/YouTube.getNewToken: %v", _err) }()
	t, err := getToken(ctx, yt.Config)
	if err != nil {
		return nil, fmt.Errorf("unable to get an access token: %w", err)
	}
	yt.Config.Config.Token = t
	err = yt.SaveConfigFunc(yt.Config)
	errmon.ObserveErrorCtx(ctx, err)
	return t, nil
}

func (yt *YouTube) init(ctx context.Context) (_err error) {
	logger.Debugf(ctx, "YouTube.init")
	defer func() { logger.Debugf(ctx, "/YouTube.init: %v", _err) }()

	yt.locker.Lock()
	defer yt.locker.Unlock()

	logger.Debugf(ctx, "YouTube.init: Lock()-ed")
	defer func() { logger.Debugf(ctx, "/YouTube.init: UnLock()-ed") }()

	isNewToken := false

	if yt.Config.Config.Token == nil {
		_, err := yt.getNewToken(ctx)
		if err != nil {
			yt.CancelFunc()
			return err
		}
		isNewToken = true
	}

	authCfg := getAuthCfgBase(yt.Config)

	tokenSource := authCfg.TokenSource(ctx, yt.Config.Config.Token)

	if !isNewToken {
		if err := yt.checkTokenNoLock(ctx); err != nil {
			logger.Errorf(ctx, "unable to get a token: %v", err)
			_, err := yt.getNewToken(ctx)
			if err != nil {
				yt.CancelFunc()
				return err
			}
			isNewToken = true
			tokenSource = authCfg.TokenSource(ctx, yt.Config.Config.Token)
		}
	}

	if err := yt.checkTokenNoLock(ctx); err != nil {
		yt.CancelFunc()
		return fmt.Errorf("the token is invalid: %w", err)
	}

	youtubeService, err := youtube.NewService(ctx, option.WithTokenSource(tokenSource))
	if err != nil {
		yt.CancelFunc()
		return err
	}

	yt.YouTubeService = youtubeService // TODO: make this atomic

	return nil
}

func getAuthCfgBase(cfg Config) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     cfg.Config.ClientID,
		ClientSecret: cfg.Config.ClientSecret,
		Endpoint:     google.Endpoint,
		Scopes: []string{
			"https://www.googleapis.com/auth/youtube",
		},
	}
}

func getToken(ctx context.Context, cfg Config) (*oauth2.Token, error) {
	if cfg.Config.GetOAuthListenPorts == nil {
		return nil, fmt.Errorf("function GetOAuthListenPorts is not set")
	}

	ctx, cancelFn := context.WithCancel(ctx)
	defer cancelFn()

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
	var tok *oauth2.Token

	startHandlerForPort := func(listenPort uint16) {
		if _, ok := alreadyListening[listenPort]; ok {
			return
		}
		logger.Tracef(ctx, "starting the oauth handler at port %d", listenPort)
		alreadyListening[listenPort] = struct{}{}
		oauthCfg := getAuthCfgBase(cfg)
		oauthCfg.RedirectURL = fmt.Sprintf("http://127.0.0.1:%d", listenPort)
		wg.Add(1)
		{
			oauthCfg := oauthCfg
			observability.Go(ctx, func() {
				defer wg.Done()
				oauthHandlerArg := oauthhandler.OAuthHandlerArgument{
					AuthURL:    oauthCfg.AuthCodeURL("state-token", oauth2.AccessTypeOffline),
					ListenPort: listenPort,
					ExchangeFn: func(code string) error {
						_tok, err := oauthCfg.Exchange(ctx, code)
						if err != nil {
							return fmt.Errorf("unable to get a token: %w", err)
						}
						tok = _tok
						cancelFn()
						return nil
					},
				}

				oauthHandler := cfg.Config.CustomOAuthHandler
				if oauthHandler == nil {
					oauthHandler = oauthhandler.OAuth2HandlerViaCLI
				}
				logger.Tracef(ctx, "calling oauthHandler for %d", listenPort)
				err := oauthHandler(ctx, oauthHandlerArg)
				logger.Tracef(ctx, "called oauthHandler for %d: %v", listenPort, err)
				if err != nil {
					errCh <- err
					return
				}
			})
		}
	}

	for _, listenPort := range cfg.Config.GetOAuthListenPorts() {
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
			ports := cfg.Config.GetOAuthListenPorts()
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

	if tok == nil {
		errWg.Wait()
		return nil, resultErr
	}

	return tok, nil
}

func (yt *YouTube) Ping(ctx context.Context) error {
	counter := 0
	for {
		if yt == nil {
			return fmt.Errorf("yt is nil")
		}
		if yt.YouTubeService == nil {
			return fmt.Errorf("yt.YouTubeService == nil")
		}
		if yt.YouTubeService.I18nLanguages == nil {
			return fmt.Errorf("yt.YouTubeService.I18nLanguages == nil")
		}
		_, err := yt.YouTubeService.I18nLanguages.List(nil).Context(ctx).Do()
		logger.Debugf(ctx, "YouTube.I18nLanguages result: %v", err)
		if err != nil {
			if yt.fixError(ctx, err, &counter) {
				continue
			}
		}
		return err
	}
}

func (yt *YouTube) Close() error {
	yt.CancelFunc()
	return nil
}

func (yt *YouTube) IterateUpcomingBroadcasts(
	ctx context.Context,
	callback func(broadcast *youtube.LiveBroadcast) error,
	parts ...string,
) error {
	var broadcasts *youtube.LiveBroadcastListResponse
	counter := 0
	for {
		var err error
		broadcasts, err = yt.YouTubeService.LiveBroadcasts.
			List(append([]string{"id"}, parts...)).
			BroadcastStatus("upcoming").
			Context(ctx).Do()
		logger.Debugf(ctx, "YouTube.LiveBroadcasts result: %v", err)
		if err != nil {
			if yt.fixError(ctx, err, &counter) {
				continue
			}
			return fmt.Errorf("unable to get the list of active broadcasts: %w", err)
		}
		break
	}

	for _, broadcast := range broadcasts.Items {
		if err := callback(broadcast); err != nil {
			return fmt.Errorf("got an error with broadcast %v: %w", broadcast.Id, err)
		}
	}
	return nil
}

func (yt *YouTube) IterateActiveBroadcasts(
	ctx context.Context,
	callback func(broadcast *youtube.LiveBroadcast) error,
	parts ...string,
) error {
	var broadcasts *youtube.LiveBroadcastListResponse
	counter := 0
	for {
		var err error
		broadcasts, err = yt.YouTubeService.LiveBroadcasts.
			List(append([]string{"id"}, parts...)).
			BroadcastStatus("active").
			Context(ctx).Do()
		logger.Debugf(ctx, "YouTube.LiveBroadcasts result: %v", err)
		if err != nil {
			if yt.fixError(ctx, err, &counter) {
				continue
			}
			return fmt.Errorf("unable to get the list of active broadcasts: %w", err)
		}
		break
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
	return yt.IterateActiveBroadcasts(ctx, func(broadcast *youtube.LiveBroadcast) error {
		if err := updateBroadcast(broadcast); err != nil {
			return fmt.Errorf("unable to update broadcast %v: %w", broadcast.Id, err)
		}
		_, err := yt.YouTubeService.LiveBroadcasts.Update(parts, broadcast).Context(ctx).Do()
		logger.Debugf(ctx, "YouTube.LiveBroadcasts result: %v", err)
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
	return yt.IterateActiveBroadcasts(ctx, func(broadcast *youtube.LiveBroadcast) error {
		_, err := yt.YouTubeService.LiveBroadcasts.InsertCuepoint(&youtube.Cuepoint{
			CueType:      "cueTypeAd",
			DurationSecs: int64(duration.Seconds()),
			WalltimeMs:   uint64(ts.UnixMilli()),
		}).Context(ctx).Do()
		logger.Debugf(ctx, "YouTube.LiveBroadcasts result: %v", err)
		return err
	})
}

func (yt *YouTube) DeleteActiveBroadcasts(
	ctx context.Context,
) error {
	return yt.IterateActiveBroadcasts(ctx, func(broadcast *youtube.LiveBroadcast) error {
		logger.Debugf(ctx, "deleting broadcast %v", broadcast.Id)
		err := yt.YouTubeService.LiveBroadcasts.Delete(broadcast.Id).Context(ctx).Do()
		logger.Debugf(ctx, "YouTube.LiveBroadcasts result: %v", err)
		return err
	})
}

type FlagBroadcastTemplateIDs []string

var liveBroadcastParts = []string{
	"id",
	"snippet",
	"contentDetails",
	"monetizationDetails",
	"status",
}

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

var playlistParts = []string{
	"contentDetails",
	"id",
	"localizations",
	"player",
	"snippet",
	"status",
}

var playlistItemParts = []string{
	"contentDetails",
	"id",
	"snippet",
	"status",
}

var streamNumInTitleRegex = regexp.MustCompile(`\[#([0-9]*)(\.[0-9]*)*\]`)

func (yt *YouTube) StartStream(
	ctx context.Context,
	title string,
	description string,
	profile StreamProfile,
	customArgs ...any,
) error {
	if err := yt.Ping(ctx); err != nil {
		return fmt.Errorf("connection to YouTube is broken: %w", err)
	}
	var templateBroadcastIDs []string
	for _, templateBroadcastIDCandidate := range customArgs {
		_templateBroadcastIDs, ok := templateBroadcastIDCandidate.(FlagBroadcastTemplateIDs)
		if ok {
			templateBroadcastIDs = _templateBroadcastIDs
			break
		}
	}

	logger.Debugf(ctx, "profile == %#+v", profile)

	templateBroadcastIDs = append(templateBroadcastIDs, profile.TemplateBroadcastIDs...)
	logger.Debugf(ctx, "templateBroadcastIDs == %v; customArgs == %v", templateBroadcastIDs, customArgs)

	templateBroadcastIDMap := map[string]struct{}{}
	for _, broadcastID := range templateBroadcastIDs {
		templateBroadcastIDMap[broadcastID] = struct{}{}
	}

	err := yt.IterateUpcomingBroadcasts(ctx, func(broadcast *youtube.LiveBroadcast) error {
		if _, ok := templateBroadcastIDMap[broadcast.Id]; ok {
			return nil
		}
		logger.Debugf(ctx, "deleting broadcast %v", broadcast.Id)
		err := yt.YouTubeService.LiveBroadcasts.Delete(broadcast.Id).Context(ctx).Do()
		logger.Debugf(ctx, "YouTube.LiveBroadcasts result: %v", err)
		return err
	})
	if err != nil {
		logger.Error(ctx, "unable to delete other upcoming streams: %w", err)
	}

	var broadcasts []*youtube.LiveBroadcast
	var videos []*youtube.Video
	{
		logger.Debugf(ctx, "getting broadcast info of %v", templateBroadcastIDs)

		response, err := yt.YouTubeService.LiveBroadcasts.
			List(liveBroadcastParts).
			Id(templateBroadcastIDs...).
			Context(ctx).Do()
		logger.Debugf(ctx, "YouTube.LiveBroadcasts result: %v", err)
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
		logger.Debugf(ctx, "YouTube.Video result: %v", err)
		if err != nil {
			return fmt.Errorf("unable to get the list of active broadcasts: %w", err)
		}
		if len(response.Items) != len(templateBroadcastIDs) {
			return fmt.Errorf("expected %d videos, but found %d", len(templateBroadcastIDs), len(response.Items))
		}
		videos = append(videos, response.Items...)
	}

	playlistsResponse, err := yt.YouTubeService.Playlists.List(playlistParts).MaxResults(1000).Mine(true).Context(ctx).Do()
	logger.Debugf(ctx, "YouTube.Playlists result: %v", err)
	if err != nil {
		return fmt.Errorf("unable to get the list of playlists: %w", err)
	}

	playlistIDMap := map[string]map[string]struct{}{}
	for _, templateBroadcastID := range templateBroadcastIDs {
		logger.Debugf(ctx, "getting playlist items for %s", templateBroadcastID)

		for _, playlist := range playlistsResponse.Items {
			playlistItemsResponse, err := yt.YouTubeService.PlaylistItems.List(playlistItemParts).MaxResults(1000).PlaylistId(playlist.Id).VideoId(templateBroadcastID).Context(ctx).Do()
			logger.Debugf(ctx, "YouTube.PlaylistItems result: %v", err)
			if err != nil {
				return fmt.Errorf("unable to get the list of playlist items: %w", err)
			}

			m := playlistIDMap[templateBroadcastID]
			if m == nil {
				m = map[string]struct{}{}
				playlistIDMap[templateBroadcastID] = m
			}

			for _, playlistItem := range playlistItemsResponse.Items {
				m[playlistItem.Snippet.PlaylistId] = struct{}{}
			}
		}

		logger.Debugf(ctx, "found %d playlists for %s", len(playlistIDMap[templateBroadcastID]), templateBroadcastID)
	}

	var highestStreamNum uint64
	if profile.AutoNumerate {
		resp, err := yt.YouTubeService.LiveBroadcasts.List(liveBroadcastParts).Context(ctx).Mine(true).MaxResults(100).Fields().Do(googleapi.QueryParameter("order", "date"))
		logger.Debugf(ctx, "YouTube.LiveBroadcasts result: %v", err)
		if err != nil {
			return fmt.Errorf("unable to request previous streams to figure out the next stream number for auto-numeration: %w", err)
		}

		for _, b := range resp.Items {
			matches := streamNumInTitleRegex.FindStringSubmatch(b.Snippet.Title)
			if len(matches) < 2 {
				continue
			}
			match := matches[1]
			streamNum, err := strconv.ParseUint(match, 10, 64)
			if err != nil {
				return fmt.Errorf("unable to parse '%s' as uint: %w", match, err)
			}
			if streamNum > highestStreamNum {
				highestStreamNum = streamNum
			}
		}
	}

	for idx, broadcast := range broadcasts {
		video := videos[idx]

		templateBroadcastID := broadcast.Id

		if video.Id != broadcast.Id {
			return fmt.Errorf("internal error: the orders of videos and broadcasts do not match: %s != %s", video.Id, broadcast.Id)
		}
		now := time.Now().UTC()
		broadcast.Id = ""
		broadcast.Etag = ""
		broadcast.ContentDetails.EnableAutoStop = false
		broadcast.ContentDetails.BoundStreamLastUpdateTimeMs = ""
		broadcast.ContentDetails.BoundStreamId = ""
		broadcast.ContentDetails.MonitorStream = nil
		broadcast.ContentDetails.ForceSendFields = []string{"EnableAutoStop"}
		broadcast.Snippet.ScheduledStartTime = now.Format("2006-01-02T15:04:05") + ".00Z"
		broadcast.Snippet.ScheduledEndTime = now.Add(time.Hour*12).Format("2006-01-02T15:04:05") + ".00Z"
		broadcast.Snippet.LiveChatId = ""
		broadcast.Status.SelfDeclaredMadeForKids = broadcast.Status.MadeForKids
		broadcast.Status.ForceSendFields = []string{"SelfDeclaredMadeForKids"}

		title := title
		if profile.AutoNumerate {
			title += fmt.Sprintf(" [#%d]", highestStreamNum+1)
		}
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
		logger.Debugf(ctx, "YouTube.LiveBroadcasts result: %v", err)
		if err != nil {
			return fmt.Errorf("unable to create a broadcast: %w", err)
		}

		video.Id = newBroadcast.Id
		video.Snippet.Title = broadcast.Snippet.Title
		video.Snippet.Description = broadcast.Snippet.Description
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
		logger.Debugf(ctx, "YouTube.Update result: %v", err)
		if err != nil {
			return fmt.Errorf("unable to update video data: %w", err)
		}

		playlistIDs := make([]string, 0, len(playlistIDMap[templateBroadcastID]))
		for playlistID := range playlistIDMap[templateBroadcastID] {
			playlistIDs = append(playlistIDs, playlistID)
		}
		sort.Strings(playlistIDs)
		for _, playlistID := range playlistIDs {
			newPlaylistItem := &youtube.PlaylistItem{
				Snippet: &youtube.PlaylistItemSnippet{
					PlaylistId: playlistID,
					ResourceId: &youtube.ResourceId{
						Kind:    "youtube#video",
						VideoId: video.Id,
					},
				},
			}
			b, err := yaml.Marshal(newPlaylistItem)
			if err == nil {
				logger.Tracef(ctx, "adding the video to playlist %s", b)
			} else {
				logger.Tracef(ctx, "adding the video to playlist %#+v", newPlaylistItem)
			}

			_, err = yt.YouTubeService.PlaylistItems.Insert(playlistItemParts, newPlaylistItem).Context(ctx).Do()
			logger.Debugf(ctx, "YouTube.PlaylistItems result: %v", err)
			if err != nil {
				return fmt.Errorf("unable to add video to playlist %#+v: %w", playlistID, err)
			}
		}

		if copyThumbnail && broadcast.Snippet.Thumbnails.Standard.Url != "" {
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
			logger.Debugf(ctx, "YouTube.Thumbnails result: %v", err)
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
		broadcast.ContentDetails.MonitorStream.ForceSendFields = []string{"BroadcastStreamDelayMs"}
		return nil
	}, "contentDetails")
}

const timeLayout = "2006-01-02T15:04:05-0700"
const timeLayoutFallback = time.RFC3339

func (yt *YouTube) GetStreamStatus(
	ctx context.Context,
) (_ret *streamcontrol.StreamStatus, _err error) {
	logger.Tracef(ctx, "GetStreamStatus")
	defer func() {
		logger.Tracef(ctx, "/GetStreamStatus: err:%v; ret:%#+v", _err, _ret)
	}()
	if err := yt.Ping(ctx); err != nil {
		return nil, fmt.Errorf("connection to YouTube is broken: %w", err)
	}
	var activeBroadcasts []*youtube.LiveBroadcast
	var startedAt *time.Time
	isActive := false
	err := yt.IterateActiveBroadcasts(ctx, func(broadcast *youtube.LiveBroadcast) error {
		ts := broadcast.Snippet.ActualStartTime
		_startedAt, err := time.Parse(timeLayout, ts)
		if err != nil {
			_startedAt, err = time.Parse(timeLayoutFallback, ts)
			if err != nil {
				return fmt.Errorf("unable to parse '%s' with layouts '%s' and '%s': %w", ts, timeLayout, timeLayoutFallback, err)
			}
		}
		startedAt = &_startedAt
		activeBroadcasts = append(activeBroadcasts, broadcast)
		isActive = true
		return nil
	}, liveBroadcastParts...)
	if err != nil {
		return nil, fmt.Errorf("unable to get active broadcasts info: %w", err)
	}

	var upcomingBroadcasts []*youtube.LiveBroadcast
	err = yt.IterateUpcomingBroadcasts(ctx, func(broadcast *youtube.LiveBroadcast) error {
		upcomingBroadcasts = append(upcomingBroadcasts, broadcast)
		return nil
	}, liveBroadcastParts...)
	if err != nil {
		return nil, fmt.Errorf("unable to get upcoming broadcasts info: %w", err)
	}

	streams, err := yt.ListStreams(ctx)
	if err != nil {
		return nil, fmt.Errorf("unable to get streams info: %w", err)
	}

	customData := StreamStatusCustomData{
		ActiveBroadcasts:   activeBroadcasts,
		UpcomingBroadcasts: upcomingBroadcasts,
		Streams:            streams,
	}
	if observability.LogLevelFilter.GetLevel() >= logger.LevelTrace {
		logger.Tracef(ctx, "len(customData.UpcomingBroadcasts) == %d; len(customData.Streams) == %d", len(customData.UpcomingBroadcasts), len(customData.Streams))
		for idx, broadcast := range customData.UpcomingBroadcasts {
			b, err := json.Marshal(broadcast)
			if err != nil {
				logger.Tracef(ctx, "UpcomingBroadcasts[%3d] == %#+v", idx, *broadcast)
			} else {
				logger.Tracef(ctx, "UpcomingBroadcasts[%3d] == %s", idx, b)
			}
		}
		logger.Tracef(ctx, "len(customData.ActiveBroadcasts) == %d", len(customData.ActiveBroadcasts))
		for idx, bc := range customData.ActiveBroadcasts {
			b, err := json.Marshal(bc)
			if err != nil {
				logger.Tracef(ctx, "ActiveBroadcasts[%3d] == %#+v", idx, *bc)
			} else {
				logger.Tracef(ctx, "ActiveBroadcasts[%3d] == %s", idx, b)
			}
		}
	}

	if !isActive {
		return &streamcontrol.StreamStatus{
			IsActive:   false,
			CustomData: customData,
		}, nil
	}

	return &streamcontrol.StreamStatus{
		IsActive:   true,
		StartedAt:  startedAt,
		CustomData: customData,
	}, nil
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
	counter := 0
	for {
		response, err := yt.YouTubeService.
			LiveStreams.
			List([]string{"id", "snippet", "cdn", "status"}).
			Mine(true).
			MaxResults(20).
			Context(ctx).Do()
		logger.Debugf(ctx, "YouTube.LiveStreams result: %v", err)
		if err != nil {
			if yt.fixError(ctx, err, &counter) {
				continue
			}
			return nil, fmt.Errorf("unable to query the list of streams: %w", err)
		}
		return response.Items, nil
	}
}

type LiveBroadcast = youtube.LiveBroadcast

func (yt *YouTube) ListBroadcasts(
	ctx context.Context,
) ([]*youtube.LiveBroadcast, error) {
	counter := 0
	for {
		response, err := yt.YouTubeService.
			LiveBroadcasts.
			List([]string{"id", "snippet", "contentDetails", "monetizationDetails", "status"}).
			Mine(true).
			MaxResults(1000).
			Context(ctx).Do()
		logger.Debugf(ctx, "YouTube.LiveBroadcasts result: %v", err)
		if err != nil {
			if yt.fixError(ctx, err, &counter) {
				continue
			}
			return nil, fmt.Errorf("unable to query the list of broadcasts: %w", err)
		}
		return response.Items, nil
	}
}

func (yt *YouTube) fixError(ctx context.Context, err error, counterPtr *int) bool {
	if err == nil {
		return false
	}
	if *counterPtr > 2 {
		return false
	}
	*counterPtr++

	tryGetNewToken := func() bool {
		logger.Debugf(ctx, "trying to get a new token")
		_, tErr := yt.getNewToken(ctx)
		if tErr != nil {
			logger.Errorf(ctx, "unable to get a new token: %w", err)
			return false
		}
		iErr := yt.init(ctx)
		if iErr != nil {
			logger.Errorf(ctx, "unable to re-initialize the YouTube client: %w", err)
			return false
		}
		return true
	}

	if strings.Contains(err.Error(), "token expired") {
		logger.Debugf(ctx, "token expired")
		return tryGetNewToken()
	}

	gErr := &googleapi.Error{}
	if !errors.As(err, &gErr) {
		return false
	}

	if gErr.Code == 401 {
		logger.Debugf(ctx, "error 401")
		return tryGetNewToken()
	}

	return false
}
