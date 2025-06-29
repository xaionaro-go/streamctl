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

	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/experimental/errmon"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/go-yaml/yaml"
	"github.com/hashicorp/go-multierror"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/streamctl/pkg/oauthhandler"
	"github.com/xaionaro-go/streamctl/pkg/secret"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/timeapiio"
	"github.com/xaionaro-go/xcontext"
	"github.com/xaionaro-go/xsync"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"
	"google.golang.org/api/youtube/v3"
)

const (
	LimitTagsLength = 500
)

type YouTube struct {
	locker         xsync.Mutex
	Config         Config
	YouTubeClient  *YouTubeClientCalcPoints
	CancelFunc     context.CancelFunc
	SaveConfigFunc func(Config) error

	currentLiveBroadcastsLocker xsync.Mutex
	currentLiveBroadcasts       []*youtube.LiveBroadcast

	messagesOutChan chan streamcontrol.ChatMessage
}

var _ streamcontrol.StreamController[StreamProfile] = (*YouTube)(nil)

const (
	copyThumbnail      = false
	debugUseMockClient = false
)

func New(
	ctx context.Context,
	cfg Config,
	saveCfgFn func(Config) error,
) (*YouTube, error) {
	if cfg.Config.ClientID == "" || cfg.Config.ClientSecret.Get() == "" {
		return nil, fmt.Errorf(
			"'clientid' or/and 'clientsecret' is/are not set; go to https://console.cloud.google.com/apis/credentials and create an app if it not created, yet",
		)
	}

	ctx, cancelFn := context.WithCancel(ctx)

	yt := &YouTube{
		Config:         cfg,
		SaveConfigFunc: saveCfgFn,
		CancelFunc:     cancelFn,

		messagesOutChan: make(chan streamcontrol.ChatMessage, 100),
	}

	err := yt.init(ctx)
	if err != nil {
		return nil, fmt.Errorf("initialization failed: %w", err)
	}

	err = yt.YouTubeClient.Ping(ctx)
	if err != nil {
		return nil, fmt.Errorf("connection verification failed: %w", err)
	}

	observability.Go(ctx, func(ctx context.Context) {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				err := yt.checkToken(ctx)
				if err != nil {
					logger.Debugf(ctx, "got an error from checkToken: %v", err)
					if strings.Contains(fmt.Sprintf("%v", err), "expired or revoked") {
						_, err := yt.getNewToken(ctx)
						errmon.ObserveErrorCtx(ctx, err)
					}
				}
			}
		}
	})

	return yt, nil
}

func (yt *YouTube) checkToken(ctx context.Context) (_err error) {
	logger.Tracef(ctx, "YouTube.checkToken")
	defer func() { logger.Tracef(ctx, "/YouTube.checkToken: %v", _err) }()

	return xsync.DoA1R1(ctx, &yt.locker, yt.checkTokenNoLock, ctx)
}

func (yt *YouTube) checkTokenNoLock(ctx context.Context) (_err error) {
	logger.Tracef(ctx, "YouTube.checkTokenNoLock")
	defer func() { logger.Tracef(ctx, "/YouTube.checkTokenNoLock: %v", _err) }()

	cfgToken := yt.Config.Config.Token.GetPointer()
	tokenSource := getAuthCfgBase(yt.Config).TokenSource(ctx, cfgToken)
	counter := 0
	for {
		logger.Tracef(ctx, "checking if the token changed")
		token, err := tokenSource.Token()
		if err != nil {
			if yt.fixError(ctx, err, &counter) {
				continue
			}
			return fmt.Errorf("unable to get a token: %w", err)
		}
		if token.AccessToken == cfgToken.AccessToken {
			logger.Tracef(ctx, "the token have not changed")
			return nil
		}
		logger.Debugf(ctx, "the token have changed")
		yt.Config.Config.Token = ptr(secret.New(*token))
		err = yt.SaveConfigFunc(yt.Config)
		logger.Debugf(ctx, "saved the new token token; %v", err)
		return err
	}
}

func (yt *YouTube) getNewToken(ctx context.Context) (_ret *oauth2.Token, _err error) {
	logger.Debugf(ctx, "YouTube.getNewToken")
	defer func() { logger.Debugf(ctx, "/YouTube.getNewToken: %v", _err) }()
	t, err := getToken(ctx, yt.Config)
	if err != nil {
		return nil, fmt.Errorf("unable to get an access token: %w", err)
	}
	yt.Config.Config.Token = ptr(secret.New(*t))
	err = yt.SaveConfigFunc(yt.Config)
	errmon.ObserveErrorCtx(ctx, err)
	return t, nil
}

func (yt *YouTube) init(ctx context.Context) (_err error) {
	logger.Debugf(ctx, "YouTube.init")
	defer func() { logger.Debugf(ctx, "/YouTube.init: %v", _err) }()

	return xsync.DoA1R1(xsync.WithEnableDeadlock(ctx, false), &yt.locker, yt.initNoLock, ctx)
}

func (yt *YouTube) initNoLock(ctx context.Context) (_err error) {
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

	tokenSource := authCfg.TokenSource(ctx, yt.Config.Config.Token.GetPointer())

	if !isNewToken {
		if err := yt.checkTokenNoLock(ctx); err != nil {
			logger.Errorf(ctx, "unable to get a token: %v", err)
			_, err := yt.getNewToken(ctx)
			if err != nil {
				yt.CancelFunc()
				return err
			}
			isNewToken = true
			tokenSource = authCfg.TokenSource(ctx, yt.Config.Config.Token.GetPointer())
		}
	}

	if err := yt.checkTokenNoLock(ctx); err != nil {
		yt.CancelFunc()
		return fmt.Errorf("the token is invalid: %w", err)
	}

	var youtubeClient YouTubeClient
	if debugUseMockClient {
		youtubeClient = NewYouTubeClientMock()
	} else {
		var err error
		youtubeClient, err = NewYouTubeClientV3(
			ctx,
			yt.wrapRequest,
			option.WithTokenSource(tokenSource),
		)
		if err != nil {
			yt.CancelFunc()
			return err
		}
	}

	yt.YouTubeClient = NewYouTubeClientCalcPoints(youtubeClient) // TODO: make this atomic
	return nil
}

func getAuthCfgBase(cfg Config) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     cfg.Config.ClientID,
		ClientSecret: cfg.Config.ClientSecret.Get(),
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
	observability.Go(ctx, func(ctx context.Context) {
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
		logger.Debugf(ctx, "starting the oauth handler at port %d", listenPort)
		alreadyListening[listenPort] = struct{}{}
		oauthCfg := getAuthCfgBase(cfg)
		oauthCfg.RedirectURL = fmt.Sprintf("http://127.0.0.1:%d", listenPort)
		wg.Add(1)
		{
			oauthCfg := oauthCfg
			observability.Go(ctx, func(ctx context.Context) {
				defer wg.Done()
				oauthHandlerArg := oauthhandler.OAuthHandlerArgument{
					AuthURL:    oauthCfg.AuthCodeURL("state-token", oauth2.AccessTypeOffline),
					ListenPort: listenPort,
					ExchangeFn: func(ctx context.Context, code string) error {
						_tok, err := oauthCfg.Exchange(ctx, code)
						if err != nil {
							return fmt.Errorf("unable to get a token: %w", err)
						}
						if _tok == nil {
							return fmt.Errorf("internal error (was supposed to be impossible): token == nil")
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
				logger.Debugf(ctx, "calling oauthHandler for %d", listenPort)
				err := oauthHandler(ctx, oauthHandlerArg)
				logger.Debugf(ctx, "called oauthHandler for %d: %v", listenPort, err)
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

	observability.Go(ctx, func(ctx context.Context) {
		wg.Wait()
		close(errCh)
	})
	<-ctx.Done()

	if tok == nil {
		errWg.Wait()
		return nil, fmt.Errorf("resulting token is nil, error: %w", resultErr)
	}

	return tok, nil
}

func (yt *YouTube) wrapRequest(
	ctx context.Context,
	doFn func(context.Context) error,
) (_err error) {
	logger.Tracef(ctx, "YouTube.wrapRequest")
	defer func() { logger.Tracef(ctx, "/YouTube.wrapRequest: %v", _err) }()

	counter := 0
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		err := doFn(ctx)
		logger.Tracef(ctx, "doFn result: %v", err)
		if err != nil {
			if yt.fixError(ctx, err, &counter) {
				continue
			}
			return fmt.Errorf("unable to query: %w", err)
		}
		return nil
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
	if err := checkCtx(ctx); err != nil {
		return err
	}

	broadcasts, err := yt.YouTubeClient.GetBroadcasts(ctx, BroadcastTypeUpcoming, nil, parts, "")
	logger.Debugf(ctx, "YouTube.LiveBroadcasts result: %v", err)
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

func (yt *YouTube) IterateActiveBroadcasts(
	ctx context.Context,
	callback func(broadcast *youtube.LiveBroadcast) error,
	parts ...string,
) error {
	if err := checkCtx(ctx); err != nil {
		return err
	}

	broadcasts, err := yt.YouTubeClient.GetBroadcasts(ctx, BroadcastTypeActive, nil, parts, "")
	logger.Debugf(ctx, "YouTube.LiveBroadcasts result: %v", err)
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
	if err := checkCtx(ctx); err != nil {
		return err
	}

	return yt.IterateActiveBroadcasts(ctx, func(broadcast *youtube.LiveBroadcast) error {
		if err := updateBroadcast(broadcast); err != nil {
			return fmt.Errorf("unable to update broadcast %v: %w", broadcast.Id, err)
		}
		err := yt.YouTubeClient.UpdateBroadcast(ctx, broadcast, parts)
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
			return fmt.Errorf(
				"YouTube have not provided the current snippet of broadcast %v",
				broadcast.Id,
			)
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
			return fmt.Errorf(
				"YouTube have not provided the current snippet of broadcast %v",
				broadcast.Id,
			)
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
			return fmt.Errorf(
				"YouTube have not provided the current snippet of broadcast %v",
				broadcast.Id,
			)
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
	if err := checkCtx(ctx); err != nil {
		return err
	}

	return yt.IterateActiveBroadcasts(ctx, func(broadcast *youtube.LiveBroadcast) error {
		err := yt.YouTubeClient.InsertCuepoint(ctx, &youtube.Cuepoint{
			CueType:      "cueTypeAd",
			DurationSecs: int64(duration.Seconds()),
			WalltimeMs:   uint64(ts.UnixMilli()),
		})
		logger.Debugf(ctx, "YouTube.LiveBroadcasts result: %v", err)
		return err
	})
}

func (yt *YouTube) DeleteActiveBroadcasts(
	ctx context.Context,
) error {
	if err := checkCtx(ctx); err != nil {
		return err
	}

	return yt.IterateActiveBroadcasts(ctx, func(broadcast *youtube.LiveBroadcast) error {
		logger.Debugf(ctx, "deleting broadcast %v", broadcast.Id)
		err := yt.YouTubeClient.DeleteBroadcast(ctx, broadcast.Id)
		logger.Debugf(ctx, "YouTube.LiveBroadcasts result: %v", err)
		return err
	})
}

func deduplicate[T comparable](slice []T) []T {
	deduped := make([]T, 0, len(slice))
	m := map[T]struct{}{}
	for _, item := range slice {
		if _, ok := m[item]; ok {
			continue
		}
		m[item] = struct{}{}
		deduped = append(deduped, item)
	}
	return deduped
}

func CalculateTagsLength(tags []string) int {
	length := 0
	for _, tag := range tags {
		length += len([]byte(tag))
		if strings.Contains(tag, " ") {
			length += 2
		}
	}
	length += len(tags)
	return length
}

func TruncateTags(tags []string) []string {
	for {
		curLength := CalculateTagsLength(tags)
		if curLength <= LimitTagsLength {
			break
		}
		tags = tags[:len(tags)-1]
	}
	return tags
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
) (_err error) {
	// TODO: split this function!

	if err := checkCtx(ctx); err != nil {
		return err
	}

	err := xsync.DoR1(ctx, &yt.currentLiveBroadcastsLocker, func() error {
		if len(yt.currentLiveBroadcasts) != 0 {
			return fmt.Errorf("streams are already started")
		}
		return nil
	})
	if err != nil {
		return err
	}

	logger.Debugf(ctx, "YouTube.StartStream")
	defer func() { logger.Debugf(ctx, "/YouTube.StartStream: %v", _err) }()

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
	logger.Debugf(
		ctx,
		"templateBroadcastIDs == %v; customArgs == %v",
		templateBroadcastIDs,
		customArgs,
	)
	if len(templateBroadcastIDs) == 0 {
		return fmt.Errorf("no template stream is selected")
	}

	templateBroadcastIDMap := map[string]struct{}{}
	for _, broadcastID := range templateBroadcastIDs {
		templateBroadcastIDMap[broadcastID] = struct{}{}
	}

	err = yt.IterateUpcomingBroadcasts(ctx, func(broadcast *youtube.LiveBroadcast) error {
		if _, ok := templateBroadcastIDMap[broadcast.Id]; ok {
			return nil
		}
		logger.Debugf(ctx, "deleting broadcast %v", broadcast.Id)
		err := yt.YouTubeClient.DeleteBroadcast(ctx, broadcast.Id)
		logger.Debugf(ctx, "YouTube.LiveBroadcasts result: %v", err)
		return err
	})
	if err != nil {
		logger.Error(ctx, "unable to delete other upcoming streams: %v", err)
	}

	var broadcasts []*youtube.LiveBroadcast
	var videos []*youtube.Video
	{
		logger.Debugf(ctx, "getting broadcast info of %v", templateBroadcastIDs)

		response, err := yt.YouTubeClient.GetBroadcasts(ctx, BroadcastTypeAll, templateBroadcastIDs, liveBroadcastParts, "")
		logger.Debugf(ctx, "YouTube.LiveBroadcasts result: %v", err)
		if err != nil {
			return fmt.Errorf("unable to get the list of active broadcasts: %w", err)
		}
		if len(response.Items) != len(templateBroadcastIDs) {
			return fmt.Errorf(
				"expected %d broadcasts, but found %d",
				len(templateBroadcastIDs),
				len(response.Items),
			)
		}
		broadcasts = append(broadcasts, response.Items...)
	}

	{
		logger.Debugf(ctx, "getting video info of %v", templateBroadcastIDs)

		response, err := yt.YouTubeClient.GetVideos(ctx, templateBroadcastIDs, videoParts)

		logger.Debugf(ctx, "YouTube.Video result: %v", err)
		if err != nil {
			return fmt.Errorf("unable to get the list of active broadcasts: %w", err)
		}
		if len(response.Items) != len(templateBroadcastIDs) {
			return fmt.Errorf(
				"expected %d videos, but found %d",
				len(templateBroadcastIDs),
				len(response.Items),
			)
		}
		videos = append(videos, response.Items...)
	}

	playlistsResponse, err := yt.YouTubeClient.GetPlaylists(ctx, playlistParts)
	logger.Debugf(ctx, "YouTube.Playlists result: %v", err)
	if err != nil {
		return fmt.Errorf("unable to get the list of playlists: %w", err)
	}

	playlistIDMap := map[string]map[string]struct{}{}
	for _, templateBroadcastID := range templateBroadcastIDs {
		logger.Debugf(ctx, "getting playlist items for %s", templateBroadcastID)

		for _, playlist := range playlistsResponse.Items {
			playlistItemsResponse, err := yt.YouTubeClient.GetPlaylistItems(ctx, playlist.Id, templateBroadcastID, playlistItemParts)
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

		logger.Debugf(
			ctx,
			"found %d playlists for %s",
			len(playlistIDMap[templateBroadcastID]),
			templateBroadcastID,
		)
	}

	var highestStreamNum uint64
	if profile.AutoNumerate {
		resp, err := yt.YouTubeClient.GetBroadcasts(ctx, BroadcastTypeAll, nil, liveBroadcastParts, "")
		logger.Debugf(ctx, "YouTube.LiveBroadcasts result: %v", err)
		if err != nil {
			return fmt.Errorf(
				"unable to request previous streams to figure out the next stream number for auto-numeration: %w",
				err,
			)
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

	return xsync.DoR1(ctx, &yt.currentLiveBroadcastsLocker, func() error {
		yt.currentLiveBroadcasts = yt.currentLiveBroadcasts[:0]
		for idx, broadcast := range broadcasts {
			video := videos[idx]

			templateBroadcastID := broadcast.Id

			if video.Id != broadcast.Id {
				return fmt.Errorf(
					"internal error: the orders of videos and broadcasts do not match: %s != %s",
					video.Id,
					broadcast.Id,
				)
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
			broadcast.Snippet.ScheduledEndTime = now.Add(time.Hour*12).
				Format("2006-01-02T15:04:05") +
				".00Z"
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
				logger.Debugf(ctx, "creating broadcast %s", b)
			} else {
				logger.Debugf(ctx, "creating broadcast %#+v", broadcast)
			}

			newBroadcast, err := yt.YouTubeClient.InsertBroadcast(ctx, broadcast,
				[]string{"snippet", "contentDetails", "monetizationDetails", "status"},
			)
			logger.Debugf(ctx, "YouTube.LiveBroadcasts result: %v", err)
			if err != nil {
				if strings.Contains(err.Error(), "invalidScheduledStartTime") {
					logger.Debugf(
						ctx,
						"it seems the local system clock is off, trying to fix the schedule time",
					)

					now, err = timeapiio.Now()
					if err != nil {
						logger.Errorf(ctx, "unable to get the actual time: %v", err)
						// guessing:
						// may be the error happened because of the know winter/summer time issue
						// on Windows?
						now = time.Now().Add(time.Hour)
					}
					broadcast.Snippet.ScheduledStartTime = now.Format("2006-01-02T15:04:05") + ".00Z"
					broadcast.Snippet.ScheduledEndTime = now.Add(time.Hour*12).
						Format("2006-01-02T15:04:05") +
						".00Z"
					newBroadcast, err = yt.YouTubeClient.InsertBroadcast(ctx, broadcast,
						[]string{"snippet", "contentDetails", "monetizationDetails", "status"},
					)
					logger.Debugf(ctx, "YouTube.LiveBroadcasts result: %v", err)
					if err != nil {
						err = fmt.Errorf("%w; is the system clock OK?", err)
					}
				}
				if err != nil {
					return fmt.Errorf("unable to create a broadcast: %w", err)
				}
			}

			video.Id = newBroadcast.Id
			video.Snippet.Title = broadcast.Snippet.Title
			video.Snippet.Description = broadcast.Snippet.Description
			video.Snippet.PublishedAt = ""
			video.Status.PublishAt = ""
			switch profile.TemplateTags {
			case TemplateTagsUndefined, TemplateTagsIgnore:
				video.Snippet.Tags = profile.Tags
			case TemplateTagsUseAsPrimary:
				video.Snippet.Tags = append(video.Snippet.Tags, profile.Tags...)
			case TemplateTagsUseAsAdditional:
				templateTags := video.Snippet.Tags
				video.Snippet.Tags = video.Snippet.Tags[:0]
				video.Snippet.Tags = append(video.Snippet.Tags, profile.Tags...)
				video.Snippet.Tags = append(video.Snippet.Tags, templateTags...)
			default:
				logger.Errorf(
					ctx,
					"unexpected value of the 'TemplateTags' setting: '%v'",
					profile.TemplateTags,
				)
				video.Snippet.Tags = profile.Tags
			}
			video.Snippet.Tags = deduplicate(video.Snippet.Tags)
			tagsTruncated := TruncateTags(video.Snippet.Tags)
			if len(tagsTruncated) != len(video.Snippet.Tags) {
				logger.Infof(
					ctx,
					"YouTube tags were truncated, the amount was reduced from %d to %d to satisfy the 500 characters limit",
					len(video.Snippet.Tags),
					len(tagsTruncated),
				)
				video.Snippet.Tags = tagsTruncated
			}
			b, err = yaml.Marshal(video)
			if err == nil {
				logger.Debugf(ctx, "updating video data to %s", b)
			} else {
				logger.Debugf(ctx, "updating video data to %#+v", broadcast)
			}
			err = yt.YouTubeClient.UpdateVideo(ctx, video, videoParts)
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
					logger.Debugf(ctx, "adding the video to playlist %s", b)
				} else {
					logger.Debugf(ctx, "adding the video to playlist %#+v", newPlaylistItem)
				}

				err = yt.YouTubeClient.InsertPlaylistItem(ctx, newPlaylistItem, playlistItemParts)
				logger.Debugf(ctx, "YouTube.PlaylistItems result: %v", err)
				if err != nil {
					return fmt.Errorf("unable to add video to playlist %#+v: %w", playlistID, err)
				}
			}

			if copyThumbnail && broadcast.Snippet.Thumbnails.Standard.Url != "" {
				logger.Debugf(ctx, "downloading the thumbnail")
				resp, err := http.Get(broadcast.Snippet.Thumbnails.Standard.Url)
				if err != nil {
					return fmt.Errorf(
						"unable to download the thumbnail from the template video: %w",
						err,
					)
				}
				logger.Debugf(ctx, "reading the thumbnail")
				thumbnail, err := io.ReadAll(resp.Body)
				resp.Body.Close()
				if err != nil {
					return fmt.Errorf(
						"unable to read the thumbnail from the response from the template video: %w",
						err,
					)
				}
				logger.Debugf(ctx, "setting the thumbnail")
				err = yt.YouTubeClient.SetThumbnail(ctx, newBroadcast.Id, bytes.NewReader(thumbnail))
				logger.Debugf(ctx, "YouTube.Thumbnails result: %v", err)
				if err != nil {
					return fmt.Errorf("unable to set the thumbnail: %w", err)
				}
			}
			yt.currentLiveBroadcasts = append(yt.currentLiveBroadcasts, newBroadcast)
			err = yt.startChatListener(ctx, newBroadcast.Id)
			if err != nil {
				logger.Errorf(ctx, "unable to start a chat listener for video '%s': %v", newBroadcast.Id, err)
			}
		}

		return nil
	})
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

func (yt *YouTube) startChatListener(
	ctx context.Context,
	videoID string,
) (_err error) {
	ctx = belt.WithField(ctx, "video_id", videoID)
	ctx = xcontext.DetachDone(ctx)

	logger.Debugf(ctx, "startChatListener(ctx, '%s')", videoID)
	defer func() { logger.Debugf(ctx, "/startChatListener(ctx, '%s'): %v", videoID, _err) }()

	chatListener, err := NewChatListener(ctx, videoID)
	if err != nil {
		return fmt.Errorf("unable to initialize the chat listener instance: %w", err)
	}

	observability.Go(ctx, func(ctx context.Context) {
		err := yt.processChatListener(ctx, chatListener)
		if err != nil && !errors.Is(err, context.Canceled) {
			logger.Errorf(ctx, "unable to process the chat listener for '%s': %v", videoID, err)
		}
	})
	return nil
}

func (yt *YouTube) processChatListener(
	ctx context.Context,
	chatListener *ChatListener,
) (_err error) {
	defer func() {
		err := chatListener.Close()
		if err != nil {
			logger.Errorf(ctx, "unable to close the chat listener for '%s': %v", chatListener.videoID, err)
		}
	}()
	defer func() {
		logger.Debugf(ctx, "stopped listening for chat messages in '%s': %v", chatListener.videoID, _err)
	}()
	inChan := chatListener.MessagesChan()
	for {
		msg, ok := <-inChan
		if !ok {
			logger.Debugf(ctx, "the input channel got closed")
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case yt.messagesOutChan <- msg:
		default:
			logger.Errorf(ctx, "chat messages queue overflow, dropping a message")
		}
	}
}

func (yt *YouTube) EndStream(
	ctx context.Context,
) error {
	expectedVideoIDs := map[string]struct{}{}
	yt.currentLiveBroadcastsLocker.Do(ctx, func() {
		for _, broadcast := range yt.currentLiveBroadcasts {
			expectedVideoIDs[broadcast.Id] = struct{}{}
		}
		yt.currentLiveBroadcasts = yt.currentLiveBroadcasts[:0]
	})

	return yt.updateActiveBroadcasts(ctx, func(broadcast *youtube.LiveBroadcast) error {
		broadcast.ContentDetails.EnableAutoStop = true
		broadcast.ContentDetails.MonitorStream.ForceSendFields = []string{"BroadcastStreamDelayMs"}
		if _, ok := expectedVideoIDs[broadcast.Id]; !ok {
			logger.Errorf(ctx, "video ID mismatch: received:%s, expected one of %v", broadcast.Id, expectedVideoIDs)
		}
		return nil
	}, "contentDetails")
}

const timeLayout = "2006-01-02T15:04:05-0700"
const timeLayoutFallback = time.RFC3339

func (yt *YouTube) GetStreamStatus(
	ctx context.Context,
) (_ret *streamcontrol.StreamStatus, _err error) {
	// TODO: try to use yt.currentLiveBroadcasts instead of re-requesting the list to
	//       save some API quota points.

	logger.Debugf(ctx, "GetStreamStatus")
	defer func() {
		logger.Debugf(ctx, "/GetStreamStatus: err:%v; ret:%#+v", _err, _ret)
	}()
	var activeBroadcasts []*youtube.LiveBroadcast
	var startedAt *time.Time
	isActive := false

	viewersCount := uint64(0)

	err := yt.IterateActiveBroadcasts(ctx, func(broadcast *youtube.LiveBroadcast) error {
		ts := broadcast.Snippet.ActualStartTime
		_startedAt, err := time.Parse(timeLayout, ts)
		if err != nil {
			_startedAt, err = time.Parse(timeLayoutFallback, ts)
			if err != nil {
				return fmt.Errorf(
					"unable to parse '%s' with layouts '%s' and '%s': %w",
					ts,
					timeLayout,
					timeLayoutFallback,
					err,
				)
			}
		}
		startedAt = &_startedAt
		if broadcast.Statistics != nil {
			viewersCount += broadcast.Statistics.ConcurrentViewers
		}
		activeBroadcasts = append(activeBroadcasts, broadcast)
		isActive = true
		return nil
	}, liveBroadcastParts...)
	if err != nil {
		return nil, fmt.Errorf("unable to get active broadcasts info: %w", err)
	}
	yt.currentLiveBroadcastsLocker.Do(ctx, func() {
		ids := map[string]struct{}{}
		for _, broadcast := range yt.currentLiveBroadcasts {
			ids[broadcast.Id] = struct{}{}
		}

		for _, newBroadcast := range activeBroadcasts {
			if _, ok := ids[newBroadcast.Id]; ok {
				continue
			}
			err = yt.startChatListener(ctx, newBroadcast.Id)
			if err != nil {
				logger.Errorf(ctx, "unable to start a chat listener for video '%s': %v", newBroadcast.Id, err)
			}
		}
		yt.currentLiveBroadcasts = activeBroadcasts
	})

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
		logger.Tracef(
			ctx,
			"len(customData.UpcomingBroadcasts) == %d; len(customData.Streams) == %d",
			len(customData.UpcomingBroadcasts),
			len(customData.Streams),
		)
		for idx, broadcast := range customData.UpcomingBroadcasts {
			b, err := json.Marshal(broadcast)
			if err != nil {
				logger.Tracef(ctx, "UpcomingBroadcasts[%3d] == %#+v", idx, *broadcast)
			} else {
				logger.Tracef(ctx, "UpcomingBroadcasts[%3d] == %s", idx, b)
			}
		}
		logger.Tracef(
			ctx,
			"len(customData.ActiveBroadcasts) == %d",
			len(customData.ActiveBroadcasts),
		)
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
		IsActive:     true,
		StartedAt:    startedAt,
		CustomData:   customData,
		ViewersCount: ptr(uint(viewersCount)),
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
	response, err := yt.YouTubeClient.GetStreams(ctx, []string{"id", "snippet", "cdn", "status"})
	logger.Debugf(ctx, "YouTube.LiveStreams result: %v", err)
	if err != nil {
		return nil, fmt.Errorf("unable to query the list of streams: %w", err)
	}
	return response.Items, nil
}

type LiveBroadcast = youtube.LiveBroadcast

func (yt *YouTube) ListBroadcasts(
	ctx context.Context,
	limit uint,
	continueFunc func(*youtube.LiveBroadcastListResponse) bool,
) (_ret []*youtube.LiveBroadcast, _err error) {
	logger.Debugf(ctx, "ListBroadcasts(ctx, %d)", limit)
	defer func() {
		logger.Debugf(ctx, "/ListBroadcasts(ctx, %d): len(result):%d; err:%v", limit, len(_ret), _err)
	}()

	var items []*youtube.LiveBroadcast
	var pageToken string
	for receivedCount := uint(0); receivedCount < limit; {
		maxResults := uint(limit - receivedCount)
		if maxResults > 50 {
			maxResults = 50
		}

		resp, err := yt.listBroadcastsPage(ctx, maxResults, pageToken)
		if err != nil {
			return nil, fmt.Errorf("listBroadcastsPage: %w", err)
		}

		if len(resp.Items) == 0 {
			break
		}

		oldCount := receivedCount
		pageToken = resp.NextPageToken
		receivedCount += uint(len(resp.Items))
		items = append(items, resp.Items...)

		if pageToken == "" {
			break
		}
		if uint(len(resp.Items)) < maxResults {
			logger.Errorf(ctx, "received less than expected: %d < %d; breaking the loop", resp.PageInfo.TotalResults, maxResults)
		}
		if continueFunc != nil && !continueFunc(resp) {
			break
		}
		logger.Debugf(ctx, "ListBroadcasts: count %d -> %d...", oldCount, receivedCount)
	}

	return items, nil
}

func (yt *YouTube) listBroadcastsPage(
	ctx context.Context,
	limit uint,
	pageToken string,
) (_ret *youtube.LiveBroadcastListResponse, _err error) {
	logger.Tracef(ctx, "listBroadcastsPage(ctx, %d, '%s')", limit, pageToken)
	defer func() { logger.Tracef(ctx, "YouTube.LiveBroadcasts result: %v", _err) }()
	if err := checkCtx(ctx); err != nil {
		return nil, err
	}
	response, err := yt.YouTubeClient.GetBroadcasts(ctx, BroadcastTypeAll, nil, []string{"id", "snippet", "contentDetails", "monetizationDetails", "status"}, pageToken)
	if err != nil {
		return nil, fmt.Errorf("unable to query the list of broadcasts: %w", err)
	}
	return response, nil
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
			logger.Errorf(ctx, "unable to get a new token: %v", err)
			return false
		}
		iErr := yt.init(ctx)
		if iErr != nil {
			logger.Errorf(ctx, "unable to re-initialize the YouTube client: %v", err)
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

func (yt *YouTube) GetChatMessagesChan(
	ctx context.Context,
) (<-chan streamcontrol.ChatMessage, error) {
	logger.Debugf(ctx, "GetChatMessagesChan")
	defer logger.Debugf(ctx, "/GetChatMessagesChan")

	outCh := make(chan streamcontrol.ChatMessage)
	observability.Go(ctx, func(ctx context.Context) {
		defer func() {
			logger.Debugf(ctx, "closing the messages channel")
			close(outCh)
		}()
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-yt.messagesOutChan:
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

func (yt *YouTube) SendChatMessage(
	ctx context.Context,
	message string,
) error {
	return xsync.DoR1(ctx, &yt.currentLiveBroadcastsLocker, func() error {
		var result *multierror.Error
		for _, broadcast := range yt.currentLiveBroadcasts {
			err := yt.YouTubeClient.InsertCommentThread(ctx, &youtube.CommentThread{
				Snippet: &youtube.CommentThreadSnippet{
					CanReply:  true,
					ChannelId: yt.Config.Config.ChannelID,
					IsPublic:  true,
					TopLevelComment: &youtube.Comment{
						Snippet: &youtube.CommentSnippet{
							TextOriginal: message,
						},
					},
					VideoId: broadcast.Id,
				},
			}, []string{"snippet"})
			if err != nil {
				result = multierror.Append(result, fmt.Errorf("unable to post the comment under video '%s': %w", broadcast.Id, err))
			}
		}
		return result.ErrorOrNil()
	})
}

func (yt *YouTube) RemoveChatMessage(
	ctx context.Context,
	messageID streamcontrol.ChatMessageID,
) error {
	// TODO: The `messageID` value below is not a message ID, unfortunately.
	//       It just contains the author and the message as a temporary solution.
	//       Find a way to extract the message ID.

	words := strings.SplitN(string(messageID), "/", 2)
	if len(words) != 2 {
		return fmt.Errorf("internal error: cannot split '%s' to author and message", messageID)
	}
	authorName := words[0]
	message := words[1]

	count := 0
	for _, broadcast := range yt.currentLiveBroadcasts {
		resp, err := yt.YouTubeClient.ListChatMessages(ctx, broadcast.Snippet.LiveChatId, []string{"snippet"})
		if err != nil {
			return fmt.Errorf("unable to get the list of current chat messages under livestream %s: %w", broadcast.Id, err)
		}

		for _, item := range resp.Items {
			msgText := item.Snippet.TextMessageDetails.MessageText
			msgAuthor := item.Snippet.AuthorChannelId
			logger.Debugf(ctx, "comparing <%s|%s> with <%s|%s>", msgAuthor, msgText, authorName, message)
			if msgText == message && msgAuthor == authorName {
				count++
				err := yt.YouTubeClient.DeleteChatMessage(ctx, string(messageID))
				if err != nil {
					return fmt.Errorf("unable to remove the message '%s': %w", messageID, err)
				}
			}
		}
	}

	if count == 0 {
		return fmt.Errorf("not found")
	}

	return nil
}
func (yt *YouTube) BanUser(
	ctx context.Context,
	userID streamcontrol.ChatUserID,
	reason string,
	deadline time.Time,
) error {
	return fmt.Errorf("not implemented, yet")
}

func (yt *YouTube) IsCapable(
	ctx context.Context,
	cap streamcontrol.Capability,
) bool {
	switch cap {
	case streamcontrol.CapabilitySendChatMessage:
		return true
	case streamcontrol.CapabilityDeleteChatMessage:
		return true
	case streamcontrol.CapabilityBanUser:
		return false
	}
	return false
}
