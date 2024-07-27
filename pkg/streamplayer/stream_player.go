package playersrv

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/facebookincubator/go-belt/tool/experimental/errmon"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/hashicorp/go-multierror"
	"github.com/xaionaro-go/streamctl/pkg/player"
	"github.com/xaionaro-go/streamctl/pkg/streamserver"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/types"
)

type StreamPlayers struct {
	StreamServer   *streamserver.StreamServer
	PlayerManager  *player.Manager
	DefaultOptions Options
}

type PlayerManager interface {
	SupportedBackends() []player.Backend
	NewPlayer(
		title string,
		backend player.Backend,
	) (player.Player, error)
}

func New(
	streamServer *streamserver.StreamServer,
	playerManager *player.Manager,
	defaultOptions ...Option,
) *StreamPlayers {
	return &StreamPlayers{
		StreamServer:   streamServer,
		PlayerManager:  playerManager,
		DefaultOptions: defaultOptions,
	}
}

type StreamPlayer struct {
	Parent   *StreamPlayers
	Player   player.Player
	StreamID types.StreamID

	Cancel context.CancelFunc
	Config Config
}

func (sp *StreamPlayers) Create(
	ctx context.Context,
	streamID types.StreamID,
	opts ...Option,
) (*StreamPlayer, error) {
	ctx, cancel := context.WithCancel(ctx)

	resultingOpts := make(Options, 0, len(sp.DefaultOptions)+len(opts))
	resultingOpts = append(resultingOpts, sp.DefaultOptions...)
	resultingOpts = append(resultingOpts, opts...)

	p := &StreamPlayer{
		Parent:   sp,
		Cancel:   cancel,
		Config:   resultingOpts.Config(),
		StreamID: streamID,
	}

	if p.Config.CatchupMaxSpeedFactor <= 1 {
		return nil, fmt.Errorf("MaxCatchupSpeedFactor should be higher than 1, but it is %v", p.Config.CatchupMaxSpeedFactor)
	}

	if p.Config.MaxCatchupAtLag <= p.Config.JitterBufDuration {
		return nil, fmt.Errorf("MaxCatchupAtLag (%v) should be higher than JitterBufDuration (%v)", p.Config.MaxCatchupAtLag, p.Config.JitterBufDuration)
	}

	if err := p.start(ctx); err != nil {
		return nil, fmt.Errorf("unable to start the player: %w", err)
	}
	return p, nil
}
func (p *StreamPlayer) start(ctx context.Context) error {
	playerType := p.Parent.PlayerManager.SupportedBackends()[0]
	player, err := p.Parent.PlayerManager.NewPlayer(
		fmt.Sprintf("streampanel-player-%s", p.StreamID),
		playerType,
	)
	if err != nil {
		errmon.ObserveErrorCtx(ctx, p.Close())
		return fmt.Errorf("unable to run a video player '%s': %w", playerType, err)
	}
	p.Player = player

	if err := p.openStream(ctx); err != nil {
		errmon.ObserveErrorCtx(ctx, p.Close())
		return fmt.Errorf("unable to open the stream in the player: %w", err)
	}

	go p.controllerLoop(ctx)
	return nil
}

func (p *StreamPlayer) stop(ctx context.Context) error {
	if err := p.Player.Close(); err != nil {
		errmon.ObserveErrorCtx(ctx, p.Close())
		return fmt.Errorf("unable to close the player: %w", err)
	}
	return nil
}

func (p *StreamPlayer) restart(ctx context.Context) error {
	if err := p.stop(ctx); err != nil {
		return fmt.Errorf("unable to stop the stream player: %w", err)
	}
	if err := p.start(ctx); err != nil {
		return fmt.Errorf("unable to start the stream player: %w", err)
	}
	return nil
}

func (p *StreamPlayer) openStream(_ context.Context) error {
	portSrvs := p.Parent.StreamServer.ServerHandlers
	if len(portSrvs) == 0 {
		return fmt.Errorf("there are no open server ports")
	}
	portSrv := portSrvs[0]

	var u url.URL
	u.Scheme = portSrv.Type().String()
	u.Host = portSrv.ListenAddr()
	u.Path = string(p.StreamID)

	if err := p.Player.OpenURL(u.String()); err != nil {
		return fmt.Errorf("unable to open '%s' in the player: %w", u.String(), err)
	}

	return nil
}

func (p *StreamPlayer) controllerLoop(ctx context.Context) {
	t := time.NewTimer(100 * time.Millisecond)
	defer t.Stop()

	// wait for video to start:
	for {
		select {
		case <-ctx.Done():
			errmon.ObserveErrorCtx(ctx, p.Close())
			return
		case <-t.C:
		}

		if p.Player.GetPosition() != 0 {
			break
		}
	}

	// now monitoring if everything is OK:
	var prevPos time.Duration
	posUpdatedAt := time.Now()
	curSpeed := float64(1)
	for {
		select {
		case <-ctx.Done():
			errmon.ObserveErrorCtx(ctx, p.Close())
			return
		case <-t.C:
		}

		now := time.Now()
		l := p.Player.GetLength()
		pos := p.Player.GetPosition()
		if pos != prevPos {
			posUpdatedAt = now
			prevPos = pos
		} else {
			if now.Sub(posUpdatedAt) > p.Config.ReadTimeout {
				p.restart(ctx)
				return
			}
		}

		lag := l - pos
		if lag <= p.Config.JitterBufDuration {
			if curSpeed == 1 {
				continue
			}
			err := p.Player.SetSpeed(1)
			if err != nil {
				logger.Errorf(ctx, "unable to reset the speed to 1: %w", err)
				continue
			}
			curSpeed = 1
			continue
		}

		speed := float64(1) +
			(p.Config.CatchupMaxSpeedFactor-float64(1))*
				(lag.Seconds()-p.Config.JitterBufDuration.Seconds())/
				(p.Config.MaxCatchupAtLag.Seconds()-p.Config.JitterBufDuration.Seconds())

		err := p.Player.SetSpeed(speed)
		if err != nil {
			logger.Errorf(ctx, "unable to set the speed to %v: %w", speed, err)
			continue
		}
		curSpeed = speed
	}
}

func (p *StreamPlayer) Close() error {
	var err *multierror.Error

	if p.Cancel != nil {
		p.Cancel()
	}

	if p.Player != nil {
		err = multierror.Append(err, p.Player.Close())
		p.Player = nil
	}

	return err.ErrorOrNil()
}
