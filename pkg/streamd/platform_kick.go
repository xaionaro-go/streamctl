// Package streamd provides the main daemon logic for streamctl. This file
// implements the kick platform backend integration.
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
	"github.com/xaionaro-go/kickcom"
	"github.com/xaionaro-go/object"
	"github.com/xaionaro-go/streamctl/pkg/oauthhandler"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/kick"
	"github.com/xaionaro-go/streamctl/pkg/streamd/api"
	"github.com/xaionaro-go/streamctl/pkg/streamd/cache"
	"github.com/xaionaro-go/xsync"
)

func (d *StreamD) GetKickCache(ctx context.Context, accountID streamcontrol.AccountID) *cache.Kick {
	return xsync.DoA2R1(ctx, &d.CacheLock, d.getKickCache, ctx, accountID)
}

func (d *StreamD) getKickCache(ctx context.Context, accountID streamcontrol.AccountID) *cache.Kick {
	if d.Cache.PerPlatform == nil {
		d.Cache.PerPlatform = make(map[string]any)
	}
	key := string(kick.ID) + ":" + string(accountID)
	v, ok := d.Cache.PerPlatform[key]
	if !ok {
		c := &cache.Kick{}
		d.Cache.PerPlatform[key] = c
		return c
	}
	if c, ok := v.(*cache.Kick); ok {
		return c
	}
	if c, ok := v.(cache.Kick); ok {
		ptr := &c
		d.Cache.PerPlatform[key] = ptr
		return ptr
	}
	if m, ok := v.(map[string]any); ok {
		b, _ := yaml.Marshal(m)
		var c cache.Kick
		yaml.Unmarshal(b, &c)
		ptr := &c
		d.Cache.PerPlatform[key] = ptr
		return ptr
	}
	c := &cache.Kick{}
	d.Cache.PerPlatform[key] = c
	return c
}

func init() {
	api.RegisterBackendDataType(kick.ID, reflect.TypeOf(api.BackendDataKick{}))
	registerPlatformBackendHandler(kick.ID, platformBackendHandler{
		InitBackend: func(ctx context.Context, d *StreamD) error {
			return d.initKickBackend(ctx)
		},
		InitCache: func(ctx context.Context, d *StreamD) bool {
			changed := d.initKickData(ctx)
			for accountID := range d.getControllersByPlatform(kick.ID) {
				d.normalizeKickData(ctx, accountID)
			}
			return changed
		},
		GetBackendData: func(ctx context.Context, d *StreamD) (any, error) {
			controllers := d.getControllersByPlatform(kick.ID)
			var categories []kickcom.CategoryV1Short
			for accountID := range controllers {
				cats := d.GetKickCache(ctx, accountID).GetCategories()
				if len(cats) > 0 {
					categories = cats
					break
				}
			}
			return api.BackendDataKick{Cache: &cache.Kick{Categories: &categories}}, nil
		},
		IsPlatformURL: func(u *url.URL) bool {
			return strings.Contains(u.Hostname(), "global-contribute.live-video.net")
		},
		StreamStatusCacheDuration: 5 * time.Second,
	})
}

func (d *StreamD) initKickBackend(ctx context.Context) error {
	platCfg := d.Config.Backends[kick.ID]
	if platCfg == nil {
		return nil
	}

	ctx = belt.WithField(ctx, "controller", kick.ID)
	platCfgConverted := streamcontrol.ConvertPlatformConfig[kick.AccountConfig](ctx, platCfg)

	currentControllers := d.getControllersByPlatform(kick.ID)
	for id, ctrl := range currentControllers {
		if id == "" {
			continue
		}
		if _, ok := platCfgConverted.Accounts[id]; !ok {
			ctrl.Close()
			xsync.DoA1(ctx, &d.AccountsLocker, func(fqID streamcontrol.AccountIDFullyQualified) {
				delete(d.AccountMap, fqID)
			}, streamcontrol.NewAccountIDFullyQualified(kick.ID, id))
		}
	}

	for id, accountCfg := range platCfgConverted.Accounts {
		if id == "" {
			logger.Errorf(ctx, "account ID is empty for Kick config (skipping): %#+v", accountCfg)
			continue
		}
		if oldCtrl, ok := currentControllers[id]; ok {
			oldCtrl.Close()
		}

		cache := d.GetKickCache(ctx, id)
		cacheHashBeforeInit, _ := object.CalcCryptoHash(*cache)

		accountCfg.CustomOAuthHandler = func(ctx context.Context, arg oauthhandler.OAuthHandlerArgument) error {
			return d.UI.OAuthHandler(ctx, kick.ID, arg)
		}
		accountCfg.GetOAuthListenPorts = d.GetOAuthListenPorts
		k, err := kick.New(kick.CtxWithCache(ctx, cache), accountCfg, func(c kick.AccountConfig) error {
			return d.setPlatformAccountConfig(ctx, kick.ID, id, &streamcontrol.AbstractPlatformConfig{
				Accounts: map[streamcontrol.AccountID]streamcontrol.RawMessage{
					id: streamcontrol.ToRawMessage(c),
				},
				Custom: platCfgConverted.Custom,
			})
		})
		if err != nil {
			logger.Errorf(ctx, "unable to initialize Kick account '%s': %v", id, err)
			continue
		}

		cacheHashAfterInit, _ := object.CalcCryptoHash(*cache)
		if len(cacheHashAfterInit) == 0 || !cacheHashAfterInit.Equals(cacheHashBeforeInit) {
			err := d.writeCache(ctx)
			if err != nil {
				logger.Errorf(ctx, "unable to write cache: %v", err)
			}
		}

		abstractAccount := streamcontrol.ToAbstractAccount(k)
		xsync.DoA2(ctx, &d.AccountsLocker, func(fqID streamcontrol.AccountIDFullyQualified, abstract streamcontrol.AbstractAccount) {
			d.AccountMap[fqID] = abstract
		}, streamcontrol.NewAccountIDFullyQualified(kick.ID, id), abstractAccount)
		d.startListeningForChatMessages(ctx, kick.ID, id, abstractAccount)
	}

	return nil
}

func (d *StreamD) initKickData(ctx context.Context) bool {
	logger.FromCtx(ctx).Debugf("initializing Kick data")
	defer logger.FromCtx(ctx).Debugf("endof initializing Kick data")

	kickControllers := d.getControllersByPlatform(kick.ID)
	if len(kickControllers) == 0 {
		logger.FromCtx(ctx).Debugf("kick controller is not initialized")
		return false
	}

	anyChanged := false
	for accountID, c := range kickControllers {
		if len(d.GetKickCache(ctx, accountID).GetCategories()) != 0 {
			continue
		}

		accountController := c.GetImplementation().(*kick.Kick)
		categories, err := accountController.GetAllCategories(d.ctxForController(ctx))
		if err != nil {
			d.UI.DisplayError(err)
			continue
		}

		logger.FromCtx(ctx).Debugf("got categories for account '%s': (count: %d)", accountID, len(categories))
		d.CacheLock.Do(ctx, func() {
			d.getKickCache(ctx, accountID).SetCategories(categories)
		})
		anyChanged = true
	}

	if anyChanged {
		err := d.SaveConfig(ctx)
		errmon.ObserveErrorCtx(ctx, err)
	}
	return anyChanged
}

func (d *StreamD) normalizeKickData(ctx context.Context, accountID streamcontrol.AccountID) {
	c := d.GetKickCache(ctx, accountID)
	if c.Categories == nil {
		return
	}
	s := *c.Categories
	sort.Slice(s, func(i, j int) bool {
		return s[i].Name < s[j].Name
	})
}
