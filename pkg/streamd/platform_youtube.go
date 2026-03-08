// Package streamd provides the main daemon logic for streamctl. This file
// implements the YouTube platform backend integration.
package streamd

import (
	"context"
	"fmt"
	"net/url"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/experimental/errmon"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/goccy/go-yaml"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/streamctl/pkg/clock"
	"github.com/xaionaro-go/streamctl/pkg/oauthhandler"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/youtube"
	"github.com/xaionaro-go/streamctl/pkg/streamd/api"
	"github.com/xaionaro-go/streamctl/pkg/streamd/cache"
	"github.com/xaionaro-go/streamctl/pkg/streamd/memoize"
	"github.com/xaionaro-go/xsync"
)

func (d *StreamD) GetYouTubeCache(ctx context.Context, accountID streamcontrol.AccountID) *cache.YouTube {
	return xsync.DoA2R1(ctx, &d.CacheLock, d.getYouTubeCache, ctx, accountID)
}

func (d *StreamD) getYouTubeCache(ctx context.Context, accountID streamcontrol.AccountID) *cache.YouTube {
	if d.Cache.PerPlatform == nil {
		d.Cache.PerPlatform = make(map[string]any)
	}
	key := string(youtube.ID) + ":" + string(accountID)
	v, ok := d.Cache.PerPlatform[key]
	if !ok {
		c := &cache.YouTube{}
		d.Cache.PerPlatform[key] = c
		return c
	}
	if c, ok := v.(*cache.YouTube); ok {
		return c
	}
	if c, ok := v.(cache.YouTube); ok {
		ptr := &c
		d.Cache.PerPlatform[key] = ptr
		return ptr
	}
	if m, ok := v.(map[string]any); ok {
		b, _ := yaml.Marshal(m)
		var c cache.YouTube
		yaml.Unmarshal(b, &c)
		ptr := &c
		d.Cache.PerPlatform[key] = ptr
		return ptr
	}
	c := &cache.YouTube{}
	d.Cache.PerPlatform[key] = c
	return c
}

func init() {
	api.RegisterBackendDataType(youtube.ID, reflect.TypeOf(api.BackendDataYouTube{}))
	registerPlatformBackendHandler(youtube.ID, platformBackendHandler{
		InitBackend: func(ctx context.Context, d *StreamD) error {
			return d.initYouTubeBackend(ctx)
		},
		InitCache: func(ctx context.Context, d *StreamD) bool {
			changed := d.initYoutubeData(ctx)
			for accountID := range d.getControllersByPlatform(youtube.ID) {
				d.normalizeYoutubeData(ctx, accountID)
			}
			return changed
		},
		GetBackendData: func(ctx context.Context, d *StreamD) (any, error) {
			controllers := d.getControllersByPlatform(youtube.ID)
			var broadcasts []*youtube.LiveBroadcast
			for accountID := range controllers {
				bc := d.GetYouTubeCache(ctx, accountID).Broadcasts
				if len(bc) > 0 {
					broadcasts = bc
					break
				}
			}
			return api.BackendDataYouTube{Cache: &cache.YouTube{Broadcasts: broadcasts}}, nil
		},
		OnStartedStream: func(ctx context.Context, d *StreamD) {
			observability.Go(ctx, func(ctx context.Context) {
				now := clock.Get().Now()
				clock.Get().Sleep(10 * time.Second)
				for time.Since(now) < 5*time.Minute {
					d.StreamStatusCache.InvalidateCache(ctx)
					clock.Get().Sleep(20 * time.Second)
				}
			})
		},
		OnStartedStreamController: func(ctx context.Context, d *StreamD, accountID streamcontrol.AccountID, streamID streamcontrol.StreamID, controller streamcontrol.AbstractAccount) error {
			// I don't know why, but if we don't open the livestream control page on YouTube
			// in the browser, then the stream does not want to start.
			//
			// And this bug is exacerbated by the fact that sometimes even if you just created
			// a stream, YouTube may report that you don't have this stream (some kind of
			// race condition on their side), so sometimes we need to wait and retry. Right
			// now we assume that the race condition cannot take more than ~25 seconds.
			deadline := clock.Get().Now().Add(30 * time.Second)
			for {
				status, err := controller.GetStreamStatus(memoize.SetNoCache(ctx, true), streamID)
				if err != nil {
					return fmt.Errorf("unable to get YouTube stream status: %w", err)
				}
				data := youtube.GetStreamStatusCustomData(status)
				bcID := getYTBroadcastID(data)
				if bcID == "" {
					err = fmt.Errorf("unable to get the broadcast ID from YouTube")
					if clock.Get().Now().Before(deadline) {
						delay := time.Second * 5
						logger.Warnf(ctx, "%v... waiting %v and trying again", err)
						clock.Get().Sleep(delay)
						continue
					}
					return err
				}
				url := fmt.Sprintf("https://studio.youtube.com/video/%s/livestreaming", bcID)
				err = d.UI.OpenBrowser(ctx, url)
				if err != nil {
					return fmt.Errorf("unable to open '%s' in browser: %w", url, err)
				}
				break
			}
			return nil
		},
		IsPlatformURL: func(u *url.URL) bool {
			return strings.Contains(u.Hostname(), "youtube")
		},
		CheckStreamStarted: func(ctx context.Context, s *streamcontrol.StreamStatus) (bool, error) {
			data := youtube.GetStreamStatusCustomData(s)
			for _, s := range data.Streams {
				if s == nil {
					continue
				}
				logger.Debugf(ctx, "stream status: %#+v", *s)
				if s.Status.HealthStatus.Status == "good" {
					return true, nil
				}
			}
			return false, nil
		},
		StreamStatusCacheDuration: 5 * time.Minute,
	})
}

func (d *StreamD) initYouTubeBackend(ctx context.Context) error {
	platCfg := d.Config.Backends[youtube.ID]
	if platCfg == nil {
		return nil
	}

	ctx = belt.WithField(ctx, "controller", youtube.ID)
	platCfgConverted := streamcontrol.ConvertPlatformConfig[youtube.AccountConfig, youtube.StreamProfile](ctx, platCfg)
	for id, accountCfg := range platCfgConverted.Accounts {
		if id == "" {
			logger.Errorf(ctx, "account ID is empty for YouTube config (skipping): %#+v", accountCfg)
			continue
		}
		if accountCfg.Enable != nil && !*accountCfg.Enable {
			continue
		}

		accountCfg.CustomOAuthHandler = func(ctx context.Context, arg oauthhandler.OAuthHandlerArgument) error {
			return d.UI.OAuthHandler(ctx, youtube.ID, arg)
		}
		accountCfg.GetOAuthListenPorts = func() []uint16 { return []uint16{8091} } // TODO: replace with: d.GetOAuthListenPorts
		youTube, err := youtube.New(ctx, accountCfg, func(c youtube.AccountConfig) error {
			logger.Debugf(ctx, "saveCfgFunc")
			defer logger.Debugf(ctx, "saveCfgFunc")
			return d.setPlatformAccountConfig(ctx, youtube.ID, id, &streamcontrol.AbstractPlatformConfig{
				Accounts: map[streamcontrol.AccountID]streamcontrol.RawMessage{
					id: streamcontrol.ToRawMessage(c),
				},
				Custom: platCfgConverted.Custom,
			})
		})
		if err != nil {
			logger.Errorf(ctx, "unable to initialize YouTube account '%s': %v", id, err)
			continue
		}

		abstractAccount := streamcontrol.ToAbstractAccount(youTube)
		xsync.DoA2(ctx, &d.AccountsLocker, func(fqID streamcontrol.AccountIDFullyQualified, abstract streamcontrol.AbstractAccount) {
			d.AccountMap[fqID] = abstract
		}, streamcontrol.NewAccountIDFullyQualified(youtube.ID, id), abstractAccount)
		d.startListeningForChatMessages(ctx, youtube.ID, id, abstractAccount)
	}

	return nil
}

func (d *StreamD) initYoutubeData(ctx context.Context) bool {
	logger.FromCtx(ctx).Debugf("initializing Youtube data")
	defer logger.FromCtx(ctx).Debugf("endof initializing Youtube data")

	youtubeControllers := d.getControllersByPlatform(youtube.ID)
	if len(youtubeControllers) == 0 {
		logger.FromCtx(ctx).Debugf("youtube controller is not initialized")
		return false
	}

	anyChanged := false
	for accountID, c := range youtubeControllers {
		if c := len(d.GetYouTubeCache(ctx, accountID).Broadcasts); c != 0 {
			logger.FromCtx(ctx).Debugf("already have broadcasts for account '%s' (count: %d)", accountID, c)
			updated, err := d.updateYoutubeBroadcasts(ctx, accountID)
			if err != nil {
				d.UI.DisplayError(err)
			}
			if updated {
				anyChanged = true
			}
			continue
		}

		anyYoutube := c.GetImplementation().(*youtube.YouTube)
		broadcasts, err := anyYoutube.ListBroadcasts(d.ctxForController(ctx), 50000, nil)
		if err != nil {
			d.UI.DisplayError(err)
			continue
		}

		logger.FromCtx(ctx).Debugf("got broadcasts for account '%s': %#+v", accountID, broadcasts)
		d.CacheLock.Do(ctx, func() {
			d.getYouTubeCache(ctx, accountID).Broadcasts = broadcasts
		})
		anyChanged = true
	}

	if anyChanged {
		err := d.SaveConfig(ctx)
		errmon.ObserveErrorCtx(ctx, err)
	}
	return anyChanged
}

func (d *StreamD) normalizeYoutubeData(ctx context.Context, accountID streamcontrol.AccountID) {
	s := d.GetYouTubeCache(ctx, accountID).Broadcasts
	sort.Slice(s, func(i, j int) bool {
		return s[i].Snippet.Title < s[j].Snippet.Title
	})
}

func getYTBroadcastID(d youtube.StreamStatusCustomData) string {
	for _, bc := range d.ActiveBroadcasts {
		return bc.Id
	}
	for _, bc := range d.UpcomingBroadcasts {
		return bc.Id
	}
	return ""
}
