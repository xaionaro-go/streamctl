package streamplayer

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/facebookincubator/go-belt/tool/experimental/errmon"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/hashicorp/go-multierror"
	"github.com/xaionaro-go/streamctl/pkg/player"
	"github.com/xaionaro-go/streamctl/pkg/streamd/api"
)

type StreamPortServer struct {
	Addr string
	Type api.StreamServerType
}

type StreamServer interface {
	GetPortServers(context.Context) ([]StreamPortServer, error)
	WaitPublisher(context.Context, api.StreamID) (<-chan struct{}, error)
}

type StreamPlayers struct {
	StreamPlayersLocker sync.RWMutex
	StreamPlayers       map[api.StreamID]*StreamPlayer

	StreamServer   StreamServer
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
	streamServer StreamServer,
	playerManager *player.Manager,
	defaultOptions ...Option,
) *StreamPlayers {
	return &StreamPlayers{
		StreamPlayers:  map[api.StreamID]*StreamPlayer{},
		StreamServer:   streamServer,
		PlayerManager:  playerManager,
		DefaultOptions: defaultOptions,
	}
}

type StreamPlayer struct {
	Parent   *StreamPlayers
	Player   player.Player
	StreamID api.StreamID

	Cancel context.CancelFunc
	Config Config
}

func (sp *StreamPlayers) Create(
	ctx context.Context,
	streamID api.StreamID,
	opts ...Option,
) (_ret *StreamPlayer, _err error) {
	logger.Debugf(ctx, "StreamPlayers.Create(ctx, '%s', %#+v)", streamID, opts)
	defer func() {
		logger.Debugf(ctx, "/StreamPlayers.Create(ctx, '%s', %#+v): (%v, %v)", streamID, opts, _ret, _err)
	}()
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

	sp.StreamPlayersLocker.Lock()
	defer sp.StreamPlayersLocker.Unlock()
	sp.StreamPlayers[streamID] = p
	return p, nil
}

func (sp *StreamPlayers) Remove(
	ctx context.Context,
	streamID api.StreamID,
) error {
	logger.Debugf(ctx, "StreamPlayers.Remove(ctx, '%s')", streamID)
	defer logger.Debugf(ctx, "/StreamPlayers.Remove(ctx, '%s')", streamID)
	sp.StreamPlayersLocker.Lock()
	defer sp.StreamPlayersLocker.Unlock()
	p, ok := sp.StreamPlayers[streamID]
	if !ok {
		return nil
	}
	errmon.ObserveErrorCtx(ctx, p.Close())
	delete(sp.StreamPlayers, streamID)
	return nil
}

func (sp *StreamPlayers) Get(streamID api.StreamID) *StreamPlayer {
	sp.StreamPlayersLocker.Lock()
	defer sp.StreamPlayersLocker.Unlock()

	return sp.StreamPlayers[streamID]
}

func (sp *StreamPlayers) GetAll() map[api.StreamID]*StreamPlayer {
	sp.StreamPlayersLocker.Lock()
	defer sp.StreamPlayersLocker.Unlock()
	r := map[api.StreamID]*StreamPlayer{}
	for k, v := range sp.StreamPlayers {
		r[k] = v
	}
	return r
}

const (
	processTitlePrefix = "streampanel-player-"
)

func StreamID2Title(streamID api.StreamID) string {
	return fmt.Sprintf("%s%s", processTitlePrefix, streamID)
}

func Title2StreamID(title string) api.StreamID {
	if !strings.HasPrefix(title, processTitlePrefix) {
		return ""
	}
	return api.StreamID(title[len(processTitlePrefix):])
}

func (p *StreamPlayer) start(ctx context.Context) error {
	logger.Debugf(ctx, "StreamPlayers.start(ctx): '%s'", p.StreamID)
	defer logger.Debugf(ctx, "/StreamPlayers.start(ctx): '%s'", p.StreamID)

	playerType := p.Parent.PlayerManager.SupportedBackends()[0]
	player, err := p.Parent.PlayerManager.NewPlayer(
		StreamID2Title(p.StreamID),
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
	logger.Debugf(ctx, "StreamPlayers.stop(ctx): '%s'", p.StreamID)
	defer logger.Debugf(ctx, "/StreamPlayers.stop(ctx): '%s'", p.StreamID)

	if err := p.Player.Close(); err != nil {
		errmon.ObserveErrorCtx(ctx, p.Close())
		return fmt.Errorf("unable to close the player: %w", err)
	}
	return nil
}

func (p *StreamPlayer) restart(ctx context.Context) error {
	logger.Debugf(ctx, "StreamPlayers.restart(ctx): '%s'", p.StreamID)
	defer logger.Debugf(ctx, "/StreamPlayers.restart(ctx): '%s'", p.StreamID)

	if err := p.stop(ctx); err != nil {
		return fmt.Errorf("unable to stop the stream player: %w", err)
	}
	if err := p.start(ctx); err != nil {
		return fmt.Errorf("unable to start the stream player: %w", err)
	}
	return nil
}

func (p *StreamPlayer) openStream(ctx context.Context) error {
	portSrvs, err := p.Parent.StreamServer.GetPortServers(ctx)
	if err != nil {
		return fmt.Errorf("unable to get the list of stream server ports: %w", err)
	}
	if len(portSrvs) == 0 {
		return fmt.Errorf("there are no open server ports")
	}
	portSrv := portSrvs[0]

	var u url.URL
	u.Scheme = portSrv.Type.String()
	u.Host = portSrv.Addr
	u.Path = string(p.StreamID)

	logger.Debugf(ctx, "opening '%s'", u.String())
	if err := p.Player.OpenURL(u.String()); err != nil {
		return fmt.Errorf("unable to open '%s' in the player: %w", u.String(), err)
	}

	return nil
}

func (p *StreamPlayer) Resetup(opts ...Option) {
	for _, opt := range opts {
		opt.Apply(&p.Config)
	}
}

func (p *StreamPlayer) controllerLoop(ctx context.Context) {
	logger.Debugf(ctx, "StreamPlayer[%s].controllerLoop", p.StreamID)
	defer logger.Debugf(ctx, "/StreamPlayer[%s].controllerLoop", p.StreamID)

	// wait for video to start:
	{
		var ch <-chan struct{}
		_ch := make(chan struct{})
		close(_ch)
		ch = _ch

		for func() bool {
			waitPublisherCtx, waitPublisherCancel := context.WithCancel(ctx)
			defer waitPublisherCancel()

			var err error
			ch, err = p.Parent.StreamServer.WaitPublisher(waitPublisherCtx, p.StreamID)
			logger.Debugf(ctx, "got a waiter from WaitPublisher for '%s'; %v", p.StreamID, err)
			errmon.ObserveErrorCtx(ctx, err)

			logger.Debugf(ctx, "waiting for stream '%s'", p.StreamID)
			select {
			case <-ctx.Done():
				errmon.ObserveErrorCtx(ctx, p.Close())
				return false
			case <-ch:
			}

			logger.Tracef(ctx, "player has ended, reopening the stream")
			err = p.openStream(ctx)
			errmon.ObserveErrorCtx(ctx, err)

			startedWaitingForBuffering := time.Now()
			for time.Since(startedWaitingForBuffering) <= p.Config.StartTimeout {
				pos := p.Player.GetPosition()
				logger.Tracef(ctx, "StreamPlayer[%s].controllerLoop: pos == %v", p.StreamID, pos)
				if pos != 0 {
					return false
				}
				time.Sleep(100 * time.Millisecond)
			}
			return true
		}() {
		}
	}

	logger.Debugf(ctx, "finished waiting for a publisher at '%s'", p.StreamID)

	t := time.NewTicker(100 * time.Millisecond)
	defer t.Stop()

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
		logger.Tracef(ctx, "StreamPlayer[%s].controllerLoop: now == %v, len == %v; pos == %v", now, l, pos)
		if pos != prevPos {
			posUpdatedAt = now
			prevPos = pos
		} else {
			if now.Sub(posUpdatedAt) > p.Config.ReadTimeout {
				err := p.restart(ctx)
				errmon.ObserveErrorCtx(ctx, err)
				if err != nil {
					err := p.Parent.Remove(ctx, p.StreamID)
					errmon.ObserveErrorCtx(ctx, err)
				}
				return
			}
		}

		lag := l - pos
		logger.Tracef(ctx, "StreamPlayer[%s].controllerLoop: lag == %v", lag)
		if lag <= p.Config.JitterBufDuration {
			if curSpeed == 1 {
				continue
			}
			logger.Tracef(ctx, "StreamPlayer[%s].controllerLoop: resetting the speed to 1")
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

		logger.Tracef(ctx, "StreamPlayer[%s].controllerLoop: setting the speed to %v", speed)
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
