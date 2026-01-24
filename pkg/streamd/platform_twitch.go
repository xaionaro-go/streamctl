// Package streamd provides the main daemon logic for streamctl. This file
// implements the twitch platform backend integration.
package streamd

import (
	"context"
	"net/url"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/experimental/errmon"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/goccy/go-yaml"
	"github.com/xaionaro-go/streamctl/pkg/oauthhandler"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/twitch"
	"github.com/xaionaro-go/streamctl/pkg/streamd/api"
	"github.com/xaionaro-go/streamctl/pkg/streamd/cache"
	"github.com/xaionaro-go/xsync"
)

func (d *StreamD) GetTwitchCache(ctx context.Context, accountID streamcontrol.AccountID) *cache.Twitch {
	return xsync.DoA2R1(ctx, &d.CacheLock, d.getTwitchCache, ctx, accountID)
}

func (d *StreamD) getTwitchCache(ctx context.Context, accountID streamcontrol.AccountID) *cache.Twitch {
	if d.Cache.PerPlatform == nil {
		d.Cache.PerPlatform = make(map[string]any)
	}
	key := string(twitch.ID) + ":" + string(accountID)
	v, ok := d.Cache.PerPlatform[key]
	if !ok {
		c := &cache.Twitch{}
		d.Cache.PerPlatform[key] = c
		return c
	}
	if c, ok := v.(*cache.Twitch); ok {
		return c
	}
	if c, ok := v.(cache.Twitch); ok {
		ptr := &c
		d.Cache.PerPlatform[key] = ptr
		return ptr
	}
	if m, ok := v.(map[string]any); ok {
		b, _ := yaml.Marshal(m)
		var c cache.Twitch
		yaml.Unmarshal(b, &c)
		ptr := &c
		d.Cache.PerPlatform[key] = ptr
		return ptr
	}
	c := &cache.Twitch{}
	d.Cache.PerPlatform[key] = c
	return c
}

func init() {
	api.RegisterBackendDataType(twitch.ID, reflect.TypeOf(api.BackendDataTwitch{}))
	registerPlatformBackendHandler(twitch.ID, platformBackendHandler{
		InitBackend: func(ctx context.Context, d *StreamD) error {
			return d.initTwitchBackend(ctx)
		},
		InitCache: func(ctx context.Context, d *StreamD) bool {
			changed := d.initTwitchData(ctx)
			for accountID := range d.getControllersByPlatform(twitch.ID) {
				d.normalizeTwitchData(ctx, accountID)
			}
			return changed
		},
		GetBackendData: func(ctx context.Context, d *StreamD) (any, error) {
			return api.BackendDataTwitch{Cache: d.GetTwitchCache(ctx, "")}, nil
		},
		IsPlatformURL: func(u *url.URL) bool {
			return strings.Contains(u.Hostname(), "twitch")
		},
		StreamStatusCacheDuration: 5 * time.Second,
		PostRaidWaitDuration:      20 * time.Second,
	})
}

func (d *StreamD) initTwitchBackend(ctx context.Context) error {
	platCfg := d.Config.Backends[twitch.ID]
	if platCfg == nil {
		return nil
	}

	ctx = belt.WithField(ctx, "controller", twitch.ID)
	platCfgConverted := streamcontrol.ConvertPlatformConfig[twitch.AccountConfig, twitch.StreamProfile](ctx, platCfg)

	currentControllers := d.getControllersByPlatform(twitch.ID)
	for id, ctrl := range currentControllers {
		if id == "" {
			continue
		}
		if _, ok := platCfgConverted.Accounts[id]; !ok {
			ctrl.Close()
			xsync.DoA1(ctx, &d.AccountsLocker, func(fqID streamcontrol.AccountIDFullyQualified) {
				delete(d.AccountMap, fqID)
			}, streamcontrol.NewAccountIDFullyQualified(twitch.ID, id))
		}
	}

	for id, accountCfg := range platCfgConverted.Accounts {
		if id == "" {
			logger.Errorf(ctx, "account ID is empty for Twitch config (skipping): %#+v", accountCfg)
			continue
		}
		if oldCtrl, ok := currentControllers[id]; ok {
			oldCtrl.Close()
		}

		accountCfg.CustomOAuthHandler = func(ctx context.Context, arg oauthhandler.OAuthHandlerArgument) error {
			return d.UI.OAuthHandler(ctx, twitch.ID, arg)
		}
		accountCfg.GetOAuthListenPorts = d.GetOAuthListenPorts
		t, err := twitch.New(ctx, accountCfg, func(c twitch.AccountConfig) error {
			return d.setPlatformAccountConfig(ctx, twitch.ID, id, &streamcontrol.AbstractPlatformConfig{
				Accounts: map[streamcontrol.AccountID]streamcontrol.RawMessage{
					id: streamcontrol.ToRawMessage(c),
				},
				Custom: platCfgConverted.Custom,
			})
		})
		if err != nil {
			logger.Errorf(ctx, "unable to initialize Twitch account '%s': %v", id, err)
			continue
		}

		abstractAccount := streamcontrol.ToAbstractAccount(t)
		xsync.DoA2(ctx, &d.AccountsLocker, func(fqID streamcontrol.AccountIDFullyQualified, abstract streamcontrol.AbstractAccount) {
			d.AccountMap[fqID] = abstract
		}, streamcontrol.NewAccountIDFullyQualified(twitch.ID, id), abstractAccount)
		d.startListeningForChatMessages(ctx, twitch.ID, id, abstractAccount)
	}

	return nil
}

func (d *StreamD) initTwitchData(ctx context.Context) bool {
	logger.FromCtx(ctx).Debugf("initializing Twitch data")
	defer logger.FromCtx(ctx).Debugf("endof initializing Twitch data")

	twitchControllers := d.getControllersByPlatform(twitch.ID)
	if len(twitchControllers) == 0 {
		logger.FromCtx(ctx).Debugf("twitch controller is not initialized")
		return false
	}

	anyChanged := false
	for accountID, c := range twitchControllers {
		if c := len(d.GetTwitchCache(ctx, accountID).Categories); c != 0 {
			logger.FromCtx(ctx).Debugf("already have categories for account '%s' (count: %d)", accountID, c)
			continue
		}

		anyTwitch := c.GetImplementation().(*twitch.Twitch)
		allCategories, err := anyTwitch.GetAllCategories(d.ctxForController(ctx))
		if err != nil {
			d.UI.DisplayError(err)
			continue
		}

		logger.FromCtx(ctx).Debugf("got categories for account '%s': %#+v", accountID, allCategories)

		d.CacheLock.Do(ctx, func() {
			d.getTwitchCache(ctx, accountID).Categories = allCategories
		})
		anyChanged = true
	}

	if anyChanged {
		err := d.SaveConfig(ctx)
		errmon.ObserveErrorCtx(ctx, err)
	}
	return anyChanged
}

func (d *StreamD) normalizeTwitchData(ctx context.Context, accountID streamcontrol.AccountID) {
	s := d.GetTwitchCache(ctx, accountID).Categories
	sort.Slice(s, func(i, j int) bool {
		return s[i].Name < s[j].Name
	})
}
