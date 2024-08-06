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
	"github.com/xaionaro-go/streamctl/pkg/observability"
	"github.com/xaionaro-go/streamctl/pkg/player"
	"github.com/xaionaro-go/streamctl/pkg/player/types"
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
	PlayerLocker sync.Mutex
	Parent       *StreamPlayers
	Player       player.Player
	StreamID     api.StreamID

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

	if err := p.startU(ctx); err != nil {
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

func (p *StreamPlayer) startU(ctx context.Context) error {
	logger.Debugf(ctx, "StreamPlayers.startU(ctx): '%s'", p.StreamID)
	defer logger.Debugf(ctx, "/StreamPlayers.startU(ctx): '%s'", p.StreamID)

	playerType := p.Parent.PlayerManager.SupportedBackends()[0]
	player, err := p.Parent.PlayerManager.NewPlayer(
		ctx,
		StreamID2Title(p.StreamID),
		playerType,
	)
	if err != nil {
		errmon.ObserveErrorCtx(ctx, p.Close())
		return fmt.Errorf("unable to run a video player '%s': %w", playerType, err)
	}
	p.Player = player
	logger.Tracef(ctx, "initialized player #%+v", player)

	if err := p.openStream(ctx); err != nil {
		errmon.ObserveErrorCtx(ctx, p.Close())
		return fmt.Errorf("unable to open the stream in the player: %w", err)
	}
	logger.Tracef(ctx, "the player #%+v opened the stream", player)

	observability.Go(ctx, func() { p.controllerLoop(ctx) })
	return nil
}

func (p *StreamPlayer) stopU(ctx context.Context) error {
	logger.Debugf(ctx, "StreamPlayers.stopU(ctx): '%s'", p.StreamID)
	defer logger.Debugf(ctx, "/StreamPlayers.stopU(ctx): '%s'", p.StreamID)

	err := p.Player.Close(ctx)
	if err != nil {
		errmon.ObserveErrorCtx(ctx, p.Close())
		return fmt.Errorf("unable to close the player: %w", err)
	}
	return nil
}

func (p *StreamPlayer) restartU(ctx context.Context) error {
	logger.Debugf(ctx, "StreamPlayers.restart(ctx): '%s'", p.StreamID)
	defer logger.Debugf(ctx, "/StreamPlayers.restart(ctx): '%s'", p.StreamID)

	if err := p.stopU(ctx); err != nil {
		return fmt.Errorf("unable to stop the stream player: %w", err)
	}
	if err := p.startU(ctx); err != nil {
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
	p.withPlayer(ctx, func(ctx context.Context, player types.Player) {
		err = player.OpenURL(ctx, u.String())
	})
	if err != nil {
		return fmt.Errorf("unable to open '%s' in the player: %w", u.String(), err)
	}

	return nil
}

func (p *StreamPlayer) Resetup(opts ...Option) {
	for _, opt := range opts {
		opt.Apply(&p.Config)
	}
}

func (p *StreamPlayer) notifyStart(ctx context.Context) {
	logger.Debugf(ctx, "notifyStart")
	defer logger.Debugf(ctx, "/notifyStart")

	for _, f := range p.Config.NotifierStart {
		func(f FuncNotifyStart) {
			defer func() {
				r := recover()
				if r != nil {
					logger.Error(ctx, "got panic during notification about a start: %v", r)
				}
			}()

			f(ctx, p.StreamID)
		}(f)
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
				var (
					pos time.Duration
					err error
				)
				p.withPlayer(ctx, func(ctx context.Context, player types.Player) {
					pos, err = player.GetPosition(ctx)
				})
				if err != nil {
					logger.Tracef(ctx, "StreamPlayer[%s].controllerLoop: unable to get the current position: %v", p.StreamID, err)
					time.Sleep(100 * time.Millisecond)
					continue
				}
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

	observability.Go(ctx, func() {
		time.Sleep(time.Second) // TODO: delete this ugly racy hack
		p.notifyStart(context.WithValue(ctx, CtxKeyStreamPlayer, p))
	})

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

		p.withPlayer(ctx, func(ctx context.Context, player types.Player) {
			now := time.Now()
			l, err := player.GetLength(ctx)
			if err != nil {
				logger.Errorf(ctx, "StreamPlayer[%s].controllerLoop: unable to get the current length: %v", p.StreamID, err)
				time.Sleep(time.Second)
				return
			}
			pos, err := player.GetPosition(ctx)
			if err != nil {
				logger.Errorf(ctx, "StreamPlayer[%s].controllerLoop: unable to get the current position: %v", p.StreamID, err)
				time.Sleep(time.Second)
				return
			}
			logger.Tracef(ctx, "StreamPlayer[%s].controllerLoop: now == %v, len == %v; pos == %v", p.StreamID, now, l, pos)
			if pos != prevPos {
				posUpdatedAt = now
				prevPos = pos
			} else {
				if now.Sub(posUpdatedAt) > p.Config.ReadTimeout {
					err := p.restartU(ctx)
					errmon.ObserveErrorCtx(ctx, err)
					if err != nil {
						err := p.Parent.Remove(ctx, p.StreamID)
						errmon.ObserveErrorCtx(ctx, err)
					}
					return
				}
			}

			lag := l - pos
			logger.Tracef(ctx, "StreamPlayer[%s].controllerLoop: lag == %v", p.StreamID, lag)
			if lag <= p.Config.JitterBufDuration {
				if curSpeed == 1 {
					return
				}
				logger.Tracef(ctx, "StreamPlayer[%s].controllerLoop: resetting the speed to 1", p.StreamID)
				err := player.SetSpeed(ctx, 1)
				if err != nil {
					logger.Errorf(ctx, "unable to reset the speed to 1: %v", err)
					return
				}
				curSpeed = 1
				return
			}

			speed := float64(1) +
				(p.Config.CatchupMaxSpeedFactor-float64(1))*
					(lag.Seconds()-p.Config.JitterBufDuration.Seconds())/
					(p.Config.MaxCatchupAtLag.Seconds()-p.Config.JitterBufDuration.Seconds())

			if speed > p.Config.CatchupMaxSpeedFactor {
				logger.Warnf(
					ctx,
					"internal error: speed is calculated higher than the maximum: %v > %v: (%v-1)*(%v-%v)/(%v-%v)",
					speed, p.Config.CatchupMaxSpeedFactor,
					p.Config.CatchupMaxSpeedFactor,
					lag.Seconds(), p.Config.JitterBufDuration.Seconds(),
					p.Config.MaxCatchupAtLag.Seconds(), p.Config.JitterBufDuration.Seconds(),
				)
				speed = p.Config.CatchupMaxSpeedFactor
			}

			logger.Tracef(ctx, "StreamPlayer[%s].controllerLoop: setting the speed to %v", p.StreamID, speed)
			err = player.SetSpeed(ctx, speed)
			if err != nil {
				logger.Errorf(ctx, "unable to set the speed to %v: %v", speed, err)
				return
			}
			curSpeed = speed
		})
	}
}

func (p *StreamPlayer) withPlayer(
	ctx context.Context,
	fn func(context.Context, types.Player),
) {
	p.PlayerLocker.Lock()
	defer p.PlayerLocker.Unlock()
	if p.Player == nil {
		panic("p.Player is nil")
	}
	fn(ctx, p.Player)
}

func (p *StreamPlayer) Close() error {
	p.PlayerLocker.Lock()
	defer p.PlayerLocker.Unlock()

	var err *multierror.Error
	if p.Cancel != nil {
		p.Cancel()
	}

	if p.Player != nil {
		err = multierror.Append(err, p.Player.Close(context.TODO()))
		p.Player = nil
	}
	return err.ErrorOrNil()
}
