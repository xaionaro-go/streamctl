// Package streamd provides the main daemon logic for streamctl. This file
// implements the obs platform backend integration.
package streamd

import (
	"context"
	"reflect"
	"time"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/obs"
	"github.com/xaionaro-go/streamctl/pkg/streamd/api"
	"github.com/xaionaro-go/xsync"
)

func init() {
	api.RegisterBackendDataType(obs.ID, reflect.TypeOf(api.BackendDataOBS{}))
	registerPlatformBackendHandler(obs.ID, platformBackendHandler{
		InitBackend: func(ctx context.Context, d *StreamD) error {
			return d.initOBSBackend(ctx)
		},
		GetBackendData: func(ctx context.Context, d *StreamD) (any, error) {
			return api.BackendDataOBS{}, nil
		},
		StreamStatusCacheDuration: 3 * time.Second,
	})
}

func (d *StreamD) initOBSBackend(ctx context.Context) error {
	platCfg := d.Config.Backends[obs.ID]
	if platCfg == nil {
		return nil
	}

	platCfgConverted := streamcontrol.ConvertPlatformConfig[obs.AccountConfig, obs.StreamProfile](ctx, platCfg)
	for id, accountCfg := range platCfgConverted.Accounts {
		if id == "" {
			logger.Errorf(ctx, "account ID is empty for OBS config (skipping): %#+v", accountCfg)
			continue
		}
		if accountCfg.Enable != nil && !*accountCfg.Enable {
			continue
		}

		o, err := obs.New(ctx, accountCfg, func(obs.AccountConfig) error { return nil })
		if err != nil {
			logger.Errorf(ctx, "unable to initialize OBS account '%s': %v", id, err)
			continue
		}

		xsync.DoA2(ctx, &d.AccountsLocker, func(fqID streamcontrol.AccountIDFullyQualified, abstract streamcontrol.AbstractAccount) {
			d.AccountMap[fqID] = abstract
		}, streamcontrol.NewAccountIDFullyQualified(obs.ID, id), streamcontrol.ToAbstractAccount(o))
		go d.listenOBSEvents(ctx, o)
	}

	return nil
}
