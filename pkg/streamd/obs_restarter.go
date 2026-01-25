package streamd

import (
	"context"
	"fmt"
	"time"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/obs-grpc-proxy/protobuf/go/obs_grpc"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/streamctl/pkg/clock"
	"github.com/xaionaro-go/streamctl/pkg/command"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/obs"
	"github.com/xaionaro-go/xsync"
)

type obsRestarter struct {
	locker     xsync.Mutex
	streamD    *StreamD
	cancelFunc context.CancelFunc
}

func newOBSRestarter(d *StreamD) *obsRestarter {
	return &obsRestarter{streamD: d}
}

func (d *StreamD) initOBSRestarter(
	ctx context.Context,
) error {
	d.obsRestarter = newOBSRestarter(d)
	d.obsRestarter.updateConfig(ctx)
	return nil
}

func (d *StreamD) updateOBSRestarterConfig(
	ctx context.Context,
) error {
	return d.obsRestarter.updateConfig(ctx)
}

func (r *obsRestarter) updateConfig(
	ctx context.Context,
) error {
	return xsync.DoA1R1(ctx, &r.locker, r.updateConfigNoLock, ctx)
}

func (r *obsRestarter) updateConfigNoLock(
	ctx context.Context,
) error {
	if r.cancelFunc != nil {
		r.cancelFunc()
		r.cancelFunc = nil
	}

	d := r.streamD
	cfg, err := d.GetConfig(ctx)
	if err != nil {
		return fmt.Errorf("unable to get config: %w", err)
	}

	obsCfg := obs.GetConfig(ctx, cfg.Backends)
	if obsCfg == nil {
		return nil
	}

	accountCfg := obsCfg.Accounts[""]
	if !accountCfg.RestartOnUnavailable.Enable {
		return nil
	}

	execCmd, err := command.Expand(ctx, accountCfg.RestartOnUnavailable.ExecCommand, nil)
	if err != nil {
		return fmt.Errorf("unable to expand the command '%s': %w", accountCfg.RestartOnUnavailable.ExecCommand, err)
	}

	ctx, cancelFn := context.WithCancel(ctx)
	r.cancelFunc = cancelFn
	observability.Go(ctx, func(ctx context.Context) {
		d.obsRestarter.loop(ctx, execCmd)
	})
	return nil
}

func (r *obsRestarter) loop(
	ctx context.Context,
	execCmd []string,
) {
	logger.Debugf(ctx, "OBS-restarter: loop: %#+v", execCmd)
	defer logger.Debugf(ctx, "/OBS-restarter: loop: %#+v", execCmd)

	t := clock.Get().Ticker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			r.checkOBSAndRestartIfNeeded(
				ctx,
				execCmd,
			)
		case <-ctx.Done():
			return
		}
	}
}

func (r *obsRestarter) checkOBSAndRestartIfNeeded(
	ctx context.Context,
	execCmd []string,
) {
	obsServer, closeFn, err := r.streamD.OBS(ctx, "")
	if closeFn != nil {
		defer closeFn()
	}
	if err != nil {
		logger.Error(ctx, "unable to connect to OBS server: %v", err)
		r.restartOBS(ctx, execCmd)
		return
	}

	_, err = obsServer.GetStats(ctx, &obs_grpc.GetStatsRequest{})
	if err != nil {
		logger.Errorf(ctx, "unable to get stats from the OBS server: %v", err)
		r.restartOBS(ctx, execCmd)
		return
	}
}

func (r *obsRestarter) restartOBS(
	ctx context.Context,
	execCmd []string,
) {
	logger.Errorf(ctx, "TODO: implement me, I was supposed to restart OBS here with command %v", execCmd)
}
