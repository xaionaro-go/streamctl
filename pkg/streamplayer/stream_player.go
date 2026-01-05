package streamplayer

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/url"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/asticode/go-astiav"
	"github.com/facebookincubator/go-belt/tool/experimental/errmon"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/hashicorp/go-multierror"
	"github.com/xaionaro-go/avpipeline/indicator"
	"github.com/xaionaro-go/avpipeline/kernel"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/processor"
	"github.com/xaionaro-go/observability"
	player "github.com/xaionaro-go/player/pkg/player/types"
	"github.com/xaionaro-go/secret"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/types/streamportserver"
	"github.com/xaionaro-go/streamctl/pkg/streamtypes"
	"github.com/xaionaro-go/xcontext"
	"github.com/xaionaro-go/xsync"
)

const (
	enableSeekOnStart    = true
	enableTracksRotation = false
	enableSlowDown       = true
	playerCheckInterval  = 100 * time.Millisecond
)

type Publisher interface {
	ClosedChan() <-chan struct{} // TODO: can I remove this?
}

type WaitPublisherChaner interface {
	WaitPublisherChan(
		ctx context.Context,
		streamID streamtypes.StreamID,
		waitForNext bool,
	) (<-chan Publisher, error)
}

type StreamServer interface {
	WaitPublisherChaner
	streamportserver.GetPortServerser
}

type StreamPlayers struct {
	StreamPlayersLocker xsync.RWMutex
	StreamPlayers       map[streamtypes.StreamID]*StreamPlayerHandler

	StreamServer   StreamServer
	PlayerManager  PlayerManager
	DefaultOptions Options
}

type PlayerManager interface {
	SupportedBackends() []player.Backend
	NewPlayer(
		ctx context.Context,
		title string,
		backend player.Backend,
		opts ...player.Option,
	) (player.Player, error)
}

func New(
	streamServer StreamServer,
	playerManager PlayerManager,
	defaultOptions ...Option,
) *StreamPlayers {
	return &StreamPlayers{
		StreamPlayers:  map[streamtypes.StreamID]*StreamPlayerHandler{},
		StreamServer:   streamServer,
		PlayerManager:  playerManager,
		DefaultOptions: defaultOptions,
	}
}

type StreamPlayer struct {
	PlayerLocker xsync.Mutex
	Player       player.Player
	StreamID     streamtypes.StreamID
	Backend      player.Backend
	Config       Config

	WantSpeedAverage         *indicator.MAMA[float64]
	CurrentJitterBufDuration time.Duration
}

type StreamPlayerHandler struct {
	StreamPlayer
	Parent *StreamPlayers
	Cancel context.CancelFunc

	CurrentVideoTrackID int
	CurrentAudioTrackID int

	NextVideoTrackID      int
	NextVideoTrackIDCount int
}

func (sp *StreamPlayers) Create(
	ctx context.Context,
	streamID streamtypes.StreamID,
	backend player.Backend,
	opts ...Option,
) (_ret *StreamPlayerHandler, _err error) {
	logger.Debugf(ctx, "StreamPlayers.Create(ctx, '%s', %#+v)", streamID, opts)
	defer func() {
		logger.Debugf(ctx, "/StreamPlayers.Create(ctx, '%s', %#+v): (%#+v, %v)", streamID, opts, _ret, _err)
	}()
	ctx, cancel := context.WithCancel(ctx)

	resultingOpts := make(Options, 0, len(sp.DefaultOptions)+len(opts))
	resultingOpts = append(resultingOpts, sp.DefaultOptions...)
	resultingOpts = append(resultingOpts, opts...)

	p := &StreamPlayerHandler{
		Parent: sp,
		Cancel: cancel,
		StreamPlayer: StreamPlayer{
			Backend:          backend,
			Config:           resultingOpts.Config(ctx),
			StreamID:         streamID,
			WantSpeedAverage: indicator.NewMAMA[float64](10, 0.3, 0.05),
		},
	}

	if p.Config.CatchupMaxSpeedFactor <= 1 {
		return nil, fmt.Errorf(
			"MaxCatchupSpeedFactor should be higher than 1, but it is %v",
			p.Config.CatchupMaxSpeedFactor,
		)
	}

	if p.Config.CatchupAtMaxLag < p.Config.JitterBufMaxDuration {
		return nil, fmt.Errorf(
			"MaxCatchupAtLag (%v) should be higher or equal to JitterBufDuration (%v)",
			p.Config.CatchupAtMaxLag,
			p.Config.JitterBufMaxDuration,
		)
	}

	p.CurrentJitterBufDuration = p.Config.JitterBufMinDuration
	if err := p.startU(ctx); err != nil {
		return nil, fmt.Errorf("unable to start the player: %w", err)
	}

	return xsync.DoR2(ctx, &sp.StreamPlayersLocker, func() (*StreamPlayerHandler, error) {
		sp.StreamPlayers[streamID] = p
		return p, nil
	})
}

func (sp *StreamPlayers) Remove(
	ctx context.Context,
	streamID streamtypes.StreamID,
) error {
	logger.Debugf(ctx, "StreamPlayers.Remove(ctx, '%s')", streamID)
	defer logger.Debugf(ctx, "/StreamPlayers.Remove(ctx, '%s')", streamID)
	return xsync.DoR1(ctx, &sp.StreamPlayersLocker, func() error {
		p, ok := sp.StreamPlayers[streamID]
		if !ok {
			return nil
		}
		errmon.ObserveErrorCtx(ctx, p.Close())
		delete(sp.StreamPlayers, streamID)
		return nil
	})
}

func (sp *StreamPlayers) Get(streamID streamtypes.StreamID) *StreamPlayerHandler {
	ctx := context.TODO()
	return xsync.DoR1(ctx, &sp.StreamPlayersLocker, func() *StreamPlayerHandler {
		return sp.StreamPlayers[streamID]
	})
}

func (sp *StreamPlayers) GetAll() map[streamtypes.StreamID]*StreamPlayerHandler {
	ctx := context.TODO()
	return xsync.DoR1(
		ctx,
		&sp.StreamPlayersLocker,
		func() map[streamtypes.StreamID]*StreamPlayerHandler {
			r := map[streamtypes.StreamID]*StreamPlayerHandler{}
			for k, v := range sp.StreamPlayers {
				r[k] = v
			}
			return r
		},
	)
}

const (
	processTitlePrefix = "streampanel-player-"
)

func StreamID2Title(streamID streamtypes.StreamID) string {
	return fmt.Sprintf("%s%s", processTitlePrefix, streamID)
}

func Title2StreamID(title string) streamtypes.StreamID {
	if !strings.HasPrefix(title, processTitlePrefix) {
		return ""
	}
	return streamtypes.StreamID(title[len(processTitlePrefix):])
}

func (p *StreamPlayerHandler) startU(ctx context.Context) error {
	logger.Debugf(ctx, "StreamPlayers.startU(ctx): '%s'", p.StreamID)
	defer logger.Debugf(ctx, "/StreamPlayers.startU(ctx): '%s'", p.StreamID)

	instanceCtx, cancelFn := context.WithCancel(ctx)

	opts := player.Options{}
	if p.Config.CustomPlayerOptions != nil {
		opts = append(opts, p.Config.CustomPlayerOptions...)
	}

	playerType := p.Backend
	player, err := p.Parent.PlayerManager.NewPlayer(
		instanceCtx,
		StreamID2Title(p.StreamID),
		playerType,
		opts...,
	)
	if err != nil {
		errmon.ObserveErrorCtx(ctx, p.Close())
		cancelFn()
		return fmt.Errorf("unable to run a video player '%s': %w", playerType, err)
	}
	if player == nil {
		errmon.ObserveErrorCtx(ctx, p.Close())
		cancelFn()
		return fmt.Errorf("player == nil")
	}
	p.Player = wrapPlayer(player)
	logger.Debugf(ctx, "initialized player %#+v", player)

	observability.Go(ctx, func(ctx context.Context) { p.controllerLoop(ctx, cancelFn) })
	return nil
}

func (p *StreamPlayerHandler) stopU(ctx context.Context) error {
	logger.Debugf(ctx, "StreamPlayers.stopU(ctx): '%s'", p.StreamID)
	defer logger.Debugf(ctx, "/StreamPlayers.stopU(ctx): '%s'", p.StreamID)
	defer func() {
		p.Player = nil
	}()

	if p.Player == nil {
		return fmt.Errorf("p.Player == nil")
	}

	err := p.Player.Close(ctx)
	if err != nil {
		errmon.ObserveErrorCtx(ctx, p.close(ctx))
		return fmt.Errorf("unable to close the player: %w", err)
	}
	return nil
}

func (p *StreamPlayerHandler) restartU(ctx context.Context) error {
	logger.Debugf(ctx, "StreamPlayers.restartU(ctx): '%s'", p.StreamID)
	defer logger.Debugf(ctx, "/StreamPlayers.restartU(ctx): '%s'", p.StreamID)

	if err := p.stopU(ctx); err != nil {
		logger.Errorf(ctx, "unable to stop the stream player: %v", err)
	}
	if err := p.startU(ctx); err != nil {
		return fmt.Errorf("unable to start the stream player: %w", err)
	}
	return nil
}

func (p *StreamPlayerHandler) getURL(ctx context.Context) (*url.URL, error) {
	if p.Config.OverrideURL != "" {
		logger.Debugf(ctx, "override URL is '%s'", p.Config.OverrideURL)
		return p.getOverriddenURL(ctx)
	} else {
		logger.Debugf(ctx, "no override URL")
		return p.getInternalURL(ctx)
	}
}

func portServerPreferenceFunc(a, b *streamportserver.Config) bool {
	switch {
	case a.Type == b.Type:
		return false
	case b.Type == streamtypes.ServerTypeRTSP:
		return true
	default:
		return false
	}
}

func (p *StreamPlayerHandler) GetProtocol(
	ctx context.Context,
) streamtypes.ServerType {
	if p.Config.OverrideURL == "" {
		portSrv, err := streamportserver.GetPreferredPortServer(
			ctx,
			p.Parent.StreamServer,
			portServerPreferenceFunc,
		)
		if err != nil {
			logger.Tracef(ctx, "unable to get the port server: %v", err)
			return streamtypes.ServerTypeUndefined
		}
		return portSrv.Type
	}

	u, err := p.getOverriddenURL(ctx)
	if err != nil {
		logger.Tracef(ctx, "unable to get the overridden URL: %v", err)
		return streamtypes.ServerTypeUndefined
	}

	switch u.Scheme {
	case "":
		logger.Tracef(ctx, "no network scheme")
		return streamtypes.ServerTypeUndefined
	case "rtmp", "rtmps", "rtmpt", "rtmpe":
		return streamtypes.ServerTypeRTMP
	case "rtsp":
		return streamtypes.ServerTypeRTSP
	case "srt":
		return streamtypes.ServerTypeSRT
	}
	logger.Warnf(ctx, "unknown/unexpected protocol scheme: '%s'", u.Scheme)
	return streamtypes.ServerTypeUndefined
}

func (p *StreamPlayerHandler) getOverriddenURL(context.Context) (*url.URL, error) {
	return url.Parse(p.Config.OverrideURL)
}

func (p *StreamPlayerHandler) getInternalURL(ctx context.Context) (*url.URL, error) {
	return streamportserver.GetURLForLocalStreamID(
		ctx,
		p.Parent.StreamServer, p.StreamID,
		portServerPreferenceFunc,
	)
}

func (p *StreamPlayerHandler) startObserver(
	ctx context.Context,
	url *url.URL,
	restartFn context.CancelFunc,
) {
	observability.Go(ctx, func(ctx context.Context) {
		defer restartFn()
		logger.Debugf(ctx, "observer started")
		defer func() { logger.Debugf(ctx, "observer ended") }()
		inputNode, err := processor.NewInputFromURL(ctx, url.String(), secret.New(""), kernel.InputConfig{})
		if err != nil {
			logger.Errorf(ctx, "unable initialize the input node: %v", err)
			return
		}

		defer func() {
			err := inputNode.Close(ctx)
			if err != nil {
				logger.Errorf(ctx, "unable to close the input: %v", err)
			}
		}()

		for {
			select {
			case out, ok := <-inputNode.OutputCh:
				if !ok {
					return
				}
				pkt := out.Packet
				if pkt == nil {
					continue
				}
				if err := p.acknowledgeInputPacket(ctx, *pkt); err != nil {
					logger.Errorf(ctx, "unable to acknowledge a packet: %v", err)
					return
				}
			case <-ctx.Done():
				return
			}
		}
	})
}

func (p *StreamPlayerHandler) acknowledgeInputPacket(
	ctx context.Context,
	pkt packet.Output,
) (_err error) {
	logger.Tracef(ctx, "acknowledgeInputPacket")
	defer func() { logger.Tracef(ctx, "/acknowledgeInputPacket: %v", _err) }()

	if !enableTracksRotation {
		return nil
	}

	if pkt.Stream.CodecParameters().MediaType() != astiav.MediaTypeVideo {
		return nil
	}

	if pkt.Flags().Has(astiav.PacketFlagKey) {
		return nil
	}

	var trackID int
	if pkt.GetStreamIndex() == 0 {
		trackID = 1
	} else {
		trackID = 2
	}

	if trackID != p.NextVideoTrackID {
		p.NextVideoTrackID = trackID
		p.NextVideoTrackIDCount = 0
	}
	p.NextVideoTrackIDCount++
	if trackID == p.CurrentVideoTrackID {
		return nil
	}

	if p.NextVideoTrackIDCount > 10 {
		p.changeTrackTo(ctx, trackID)
	}
	return nil
}

func (p *StreamPlayerHandler) changeTrackTo(
	ctx context.Context,
	trackID int,
) {
	p.withPlayer(ctx, func(ctx context.Context, player player.Player) error {
		if err := player.SetVideoTrack(ctx, int64(trackID)); err != nil {
			err = fmt.Errorf("unable to rotate the video track: %w", err)
			logger.Errorf(ctx, "%v", err)
			return err
		}
		if err := player.SetAudioTrack(ctx, int64(trackID)); err != nil {
			err = fmt.Errorf("unable to rotate the audio track: %w", err)
			logger.Errorf(ctx, "%v", err)
			return err
		}
		p.CurrentVideoTrackID = trackID
		p.CurrentAudioTrackID = trackID
		p.NextVideoTrackIDCount = 0
		return nil
	})
}

func (p *StreamPlayerHandler) openStream(
	ctx context.Context,
	restartFn context.CancelFunc,
) (_err error) {
	logger.Debugf(ctx, "openStream")
	defer func() { logger.Debugf(ctx, "/openStream: %v", _err) }()

	openURLTimeout := p.Config.StartTimeout

	u, err := p.getURL(ctx)
	if err != nil {
		return fmt.Errorf("unable to get URL: %w", err)
	}
	logger.Debugf(ctx, "opening '%s'", u.String())

	if p.Config.EnableObserver {
		p.startObserver(ctx, u, restartFn)
	}

	err = p.withPlayer(ctx, func(ctx context.Context, player player.Player) error {
		ctx, cancelFn := context.WithTimeout(ctx, openURLTimeout)
		defer cancelFn()
		var once sync.Once
		observability.Go(ctx, func(ctx context.Context) {
			<-ctx.Done()
			once.Do(func() {
				logger.Errorf(ctx, "timed out, unable to open the URL '%s' within the timeout of %s", u, openURLTimeout)
				err := player.Close(xcontext.DetachDone(ctx))
				logger.Debugf(ctx, "closing timed-out player result: %v", err)
			})
		})
		err := player.OpenURL(ctx, u.String())
		once.Do(func() {})
		if err != nil {
			return fmt.Errorf("unable to open the URL: %w", err)
		}
		return nil
	})
	logger.Debugf(ctx, "opened '%s': %v", u.String(), err)
	if err != nil {
		return fmt.Errorf("unable to open '%s' in the player: %w", u.String(), err)
	}

	return nil
}

func (p *StreamPlayerHandler) Resetup(opts ...Option) {
	for _, opt := range opts {
		opt.Apply(&p.Config)
	}
}

func (p *StreamPlayerHandler) notifyStart(ctx context.Context) {
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

func (p *StreamPlayerHandler) controllerLoop(
	ctx context.Context,
	cancelPlayerInstance context.CancelFunc,
) {
	defer cancelPlayerInstance() // this is not necessary, but exists, just in case to reduce risks of a bad cleanup

	logger.Debugf(ctx, "StreamPlayer[%s].controllerLoop", p.StreamID)
	defer logger.Debugf(ctx, "/StreamPlayer[%s].controllerLoop", p.StreamID)

	instanceCtx, cancelFn := context.WithCancel(ctx)

	isClosed := false
	restart := func() {
		logger.WithField(
			ctx,
			"stack_trace", string(debug.Stack()),
		).Debugf("restart() was invoked")
		if isClosed {
			return
		}
		isClosed = true
		observability.Go(ctx, func(ctx context.Context) {
			p.PlayerLocker.Do(ctx, func() {
				err := p.restartU(ctx)
				errmon.ObserveErrorCtx(ctx, err)
				if err != nil {
					err := p.Parent.Remove(ctx, p.StreamID)
					errmon.ObserveErrorCtx(ctx, err)
				}
			})
		})
	}
	protocol := p.GetProtocol(ctx)

	// wait for video to start:
	{
		var ch <-chan Publisher
		_ch := make(chan Publisher)
		close(_ch)
		ch = _ch

		for func() bool {
			if p.Config.OverrideURL == "" || p.Config.ForceWaitForPublisher {
				waitPublisherCtx, waitPublisherCancel := context.WithCancel(ctx)
				defer waitPublisherCancel()

				var err error
				ch, err = p.Parent.StreamServer.WaitPublisherChan(waitPublisherCtx, p.StreamID, false)
				logger.Debugf(ctx, "got a waiter from WaitPublisherChan for '%s'; %v", p.StreamID, err)
				errmon.ObserveErrorCtx(ctx, err)

				logger.Debugf(ctx, "waiting for stream '%s'", p.StreamID)
				select {
				case <-instanceCtx.Done():
					logger.Debugf(ctx, "the instance was cancelled")
					errmon.ObserveErrorCtx(ctx, p.Close())
					return false
				case <-ch:
					logger.Debugf(ctx, "a stream started, let's open it in the player")
				}
				logger.Debugf(ctx, "opening the stream")
				err = p.openStream(ctx, restart)
				logger.Debugf(ctx, "opened the stream: %v", err)
				errmon.ObserveErrorCtx(ctx, err)
				time.Sleep(2 * time.Second)
			} else {
				t := time.NewTicker(1 * time.Second)
				defer t.Stop()
				for {
					select {
					case <-instanceCtx.Done():
						return false
					case <-t.C:
					}
					logger.Debugf(ctx, "opening the external stream")
					err := p.openStream(ctx, restart)
					logger.Debugf(ctx, "opened the external stream: %v", err)
					if err != nil {
						logger.Debugf(ctx, "unable to open the stream: %v", err)
						continue
					}
					deadline := time.Now().Add(30 * time.Second)
					for {
						select {
						case <-instanceCtx.Done():
							return false
						case <-t.C:
						}
						logger.Debugf(ctx, "checking if we get get the position")
						err = p.withPlayer(ctx, func(ctx context.Context, player player.Player) error {
							pos, err := player.GetAudioPosition(ctx)
							logger.Debugf(ctx, "result of getting the position: %v %v", pos, err)
							if err != nil {
								return fmt.Errorf("unable to get the position: %w", err)
							}
							return nil
						})
						if errors.As(err, &ErrNilPlayer{}) {
							logger.Debugf(ctx, "player is nil, finishing")
							cancelFn()
							return false
						}
						if err == nil {
							break
						}
						now := time.Now()
						logger.Debugf(ctx, "checking if deadline reached: %v %v", now, deadline)
						if now.After(deadline) {
							break
						}
					}
					if err == nil {
						break
					}
				}
				logger.Debugf(ctx, "we opened the external stream and the player started to play it")
			}

			triedToFixEmptyLinkViaReopen := false
			triedToFixBadLengthViaReopen := false
			triedToSeek := false
			startedWaitingForBuffering := time.Now()
			iterationNum := 0
			for time.Since(startedWaitingForBuffering) <= p.Config.StartTimeout {
				var (
					pos time.Duration
					err error
				)
				err = p.withPlayer(ctx, func(ctx context.Context, player player.Player) error {
					if !triedToFixEmptyLinkViaReopen {
						if link, err := player.GetLink(ctx); link == "" {
							logger.Debugf(ctx, "the link is empty for some reason, reopening the link (BTW, err if any is: %v)", err)
							observability.Go(ctx, func(ctx context.Context) {
								if err := p.openStream(ctx, restart); err != nil {
									logger.Errorf(ctx, "unable to open link '%s': %v", link, err)
								}
							})
							triedToFixEmptyLinkViaReopen = true
							return nil
						}
					}
					if isPaused, _ := player.GetPause(ctx); isPaused {
						logger.Debugf(ctx, "is paused for some reason, unpausing")
						if err := player.SetPause(ctx, false); err != nil {
							logger.Errorf(ctx, "unable to unpause: %v", err)
							return err
						}
					}
					var err error
					pos, err = player.GetAudioPosition(ctx)
					if err != nil {
						return fmt.Errorf("unable to get position: %w", err)
					}
					if enableSeekOnStart && protocol != streamtypes.ServerTypeRTMP && !triedToSeek {
						var l time.Duration
						l, err = player.GetLength(ctx)
						if err != nil {
							return fmt.Errorf("unable to get length: %w", err)
						}
						if l > p.Config.JitterBufMaxDuration/2 {
							if err := player.Seek(ctx, -time.Second, true, true); err != nil {
								logger.Errorf(ctx, "unable to seek: %v", err)
								return err
							}
							triedToSeek = true
						}
					}
					return nil
				})
				if err != nil {
					logger.Tracef(ctx, "StreamPlayer[%s].controllerLoop: unable to get the current position: %v", p.StreamID, err)
					time.Sleep(100 * time.Millisecond)
					continue
				}
				logger.Tracef(ctx, "StreamPlayer[%s].controllerLoop: pos == %v", p.StreamID, pos)
				var l time.Duration
				err = p.withPlayer(ctx, func(ctx context.Context, player player.Player) error {
					var err error
					l, err = player.GetLength(ctx)
					if err != nil {
						return fmt.Errorf("unable to get length: %w", err)
					}
					return nil
				})
				logger.Tracef(ctx, "StreamPlayer[%s].controllerLoop: length == %v", p.StreamID, l)
				if l < 0 {
					logger.Debugf(ctx, "StreamPlayer[%s].controllerLoop: negative length, restarting", p.StreamID)
					restart()
					return false
				}
				if l > 48*time.Hour {
					logger.Debugf(ctx, "StreamPlayer[%s].controllerLoop: the length is more than 48 hours: %v (we expect only like a second, not 2 days)", l, p.StreamID)
					if triedToFixBadLengthViaReopen {
						logger.Debugf(ctx, "StreamPlayer[%s].controllerLoop: already tried reopening the stream, did not help, so restarting")
						restart()
						return false
					}
					observability.Go(ctx, func(ctx context.Context) {
						if err := p.openStream(ctx, restart); err != nil {
							logger.Error(ctx, "unable to re-open the stream: %v", err)
							restart()
						}
					})
					triedToFixBadLengthViaReopen = true
					startedWaitingForBuffering = time.Now()
					continue
				}
				if pos != 0 {
					return false
				}
				if iterationNum%10 == 0 {
					logger.Debugf(ctx, "l == %v, pos == %v, iterationNum == %v", l, pos, iterationNum)
				}
				iterationNum++
				time.Sleep(100 * time.Millisecond)
			}

			logger.Errorf(ctx, "StreamPlayer[%s].controllerLoop: timed out on waiting until the player would start up; restarting", p.StreamID)
			restart()
			return false
		}() {
		}
	}

	if isClosed {
		return
	}

	select {
	case <-instanceCtx.Done():
		return
	default:
	}

	err := p.withPlayer(ctx, func(ctx context.Context, player player.Player) error {
		closeChan, err := player.EndChan(ctx)
		if err != nil {
			logger.Errorf(ctx, "unable to get the EndChan of the player")
			cancelFn()
			return err
		} else {
			observability.Go(ctx, func(ctx context.Context) {
				select {
				case <-closeChan:
					logger.Warnf(ctx, "the player is apparently closed, restarting it")
					cancelFn()
					return
				default:
				}
			})
		}

		err = player.SetupForStreaming(ctx)
		if err != nil {
			logger.Errorf(ctx, "unable to setup the player for streaming: %v", err)
			return err
		}
		if enableSeekOnStart && protocol != streamtypes.ServerTypeRTMP {
			if err := player.Seek(ctx, -time.Second, true, true); err != nil {
				logger.Errorf(ctx, "unable to seek: %v", err)
				return err
			}
		}
		return nil
	})
	if err != nil {
		logger.Error(ctx, "unable to access the player for setting it up for streaming: %v", err)
	}

	observability.Go(ctx, func(ctx context.Context) {
		time.Sleep(time.Second) // TODO: delete this ugly racy hack
		p.notifyStart(context.WithValue(ctx, CtxKeyStreamPlayer, p))
	})

	getRestartChan := context.Background().Done()
	if fn := p.Config.GetRestartChanFunc; fn != nil {
		getRestartChan = fn()
	}

	logger.Debugf(ctx, "finished waiting for a publisher at '%s'", p.StreamID)

	traceLogLevel := logger.LevelTrace
	jitterBufDurationIncrease := time.Duration(0)
	increaseJitterBufferBy := func(inc time.Duration) {
		if inc == 0 {
			return
		}
		p.CurrentJitterBufDuration = min(
			p.CurrentJitterBufDuration+inc,
			p.Config.JitterBufMaxDuration,
		)
		logger.Debugf(ctx,
			"StreamPlayer[%s].controllerLoop: increased jitter buffer duration to %v (increased by %v)",
			p.StreamID,
			p.CurrentJitterBufDuration,
			inc,
		)
	}
	commitJitterBufferIncrease := func() {
		logger.Logf(ctx, traceLogLevel,
			"StreamPlayer[%s].controllerLoop: committing jitter buffer increase of %v",
			p.StreamID,
			jitterBufDurationIncrease,
		)
		increaseJitterBufferBy(jitterBufDurationIncrease)
		jitterBufDurationIncrease = 0
	}
	defer commitJitterBufferIncrease()

	t := time.NewTicker(playerCheckInterval)
	defer t.Stop()

	// now monitoring if everything is OK:
	var prevPos time.Duration
	var prevLength time.Duration
	posUpdatedAt := time.Now()
	lastDebugReportAt := time.Now()
	curSpeed := float64(1)
	prevNow := time.Now()
	for {
		if isClosed {
			logger.Debug(ctx, "the player is closed, so closing the controllerLoop")
			return
		}
		var now time.Time
		select {
		case <-instanceCtx.Done():
			errmon.ObserveErrorCtx(ctx, p.Close())
			return
		case <-getRestartChan:
			logger.Debugf(
				ctx,
				"received a notification that the player should be restarted immediately",
			)
			restart()
			return
		case now = <-t.C:
		}
		timeDelta := now.Sub(prevNow)
		prevNow = now

		if time.Since(lastDebugReportAt) > 10*time.Second {
			traceLogLevel = logger.LevelDebug
			lastDebugReportAt = time.Now()
		}

		err := p.withPlayer(ctx, func(ctx context.Context, player player.Player) error {
			now := time.Now()
			pos, err := player.GetAudioPosition(ctx)
			if err != nil {
				logger.Errorf(ctx,
					"StreamPlayer[%s].controllerLoop: unable to get the current position: %v",
					p.StreamID, err,
				)
				if now.Sub(posUpdatedAt) > p.Config.ReadTimeout {
					logger.Debugf(ctx, "StreamPlayer[%s].controllerLoop: now == %v, posUpdatedAt == %v, pos == %v; readTimeout == %v, restarting (cannot even get a position)", p.StreamID, now, posUpdatedAt, pos, p.Config.ReadTimeout)
					restart()
					return nil
				}
				time.Sleep(time.Second)
				return nil
			}

			l := time.Duration(-1)
			if mpv, ok := player.(interface {
				GetCachedDuration(context.Context) (time.Duration, error)
			}); ok {
				dur, err := mpv.GetCachedDuration(ctx)
				switch {
				case err == nil:
					logger.Logf(ctx, traceLogLevel, "cached duration: %v", dur)
					l = pos + dur
				case errors.As(err, &ErrNotImplemented{}):
				default:
					logger.Errorf(ctx,
						"StreamPlayer[%s].controllerLoop: unable to get the current cache duration: %v",
						p.StreamID, err,
					)
					if prevLength != 0 {
						logger.Debugf(ctx,
							"previously GetCachedDuration worked, so it seems like the player died or something, restarting",
						)
						restart()
						return nil
					}
					time.Sleep(time.Second)
					return nil
				}
			}
			if l < 0 {
				l, err = player.GetLength(ctx)
				if err != nil {
					logger.Errorf(ctx,
						"StreamPlayer[%s].controllerLoop: unable to get the current length: %v",
						p.StreamID, err,
					)
					if prevLength != 0 {
						logger.Debugf(ctx,
							"previously GetLength worked, so it seems like the player died or something, restarting",
						)
						restart()
						return nil
					}
					time.Sleep(time.Second)
					return nil
				}
				logger.Logf(ctx, traceLogLevel, "length: %v", l)
			}
			prevLength = l

			logger.Logf(ctx, traceLogLevel,
				"StreamPlayer[%s].controllerLoop: now == %v, posUpdatedAt == %v, len == %v; pos == %v; jitterBuf == %v; readTimeout == %v",
				p.StreamID, now, posUpdatedAt, l, pos, p.CurrentJitterBufDuration, p.Config.ReadTimeout,
			)

			if pos < -p.Config.ReadTimeout {
				logger.Debugf(ctx, "negative position: %v", pos)
				restart()
				return nil
			}

			if l < 0 {
				logger.Debugf(ctx, "negative length: %v", l)
				restart()
				return nil
			}

			var stalling bool
			if pos != prevPos {
				posUpdatedAt = now
				prevPos = pos
			} else {
				stalling = true
				noMovementDuration := now.Sub(posUpdatedAt)
				if noMovementDuration > p.Config.ReadTimeout {
					logger.Debugf(ctx, "StreamPlayer[%s].controllerLoop: now == %v, posUpdatedAt == %v, len == %v; pos == %v; readTimeout == %v, restarting", p.StreamID, now, posUpdatedAt, l, pos, p.Config.ReadTimeout)
					jitterBufDurationIncrease = 0 // we are not sure why this happened, so not raising the latency without a good reason
					restart()
					return nil
				}
				minExtraBuf := noMovementDuration
				if minExtraBuf > jitterBufDurationIncrease {
					jitterBufDurationIncrease = minExtraBuf
					logger.Debugf(ctx, "StreamPlayer[%s].controllerLoop: no movement duration == %v, setting the jitterBufDurationIncrease to %v", p.StreamID, noMovementDuration, minExtraBuf)
				}
			}

			if !stalling {
				// x^(halftime/interval) = 0.5
				// log(x) * (halftime/interval) = log(0.5)
				// log(x) = log(0.5) / (halftime/interval)
				// x = e^(log(0.5)/(halftime/interval))
				jitterBufFactor := math.Exp(math.Log(0.5) / (p.Config.JitterBufDecayHalftime.Seconds() / timeDelta.Seconds()))
				p.CurrentJitterBufDuration = max(
					time.Duration(float64(p.CurrentJitterBufDuration)*jitterBufFactor),
					p.Config.JitterBufMinDuration,
				)
				logger.Logf(ctx, traceLogLevel,
					"StreamPlayer[%s].controllerLoop: decreasing the jitter buffer duration: factor:%v (halftime: %v, interval: %v); new duration: %v",
					p.StreamID,
					jitterBufFactor,
					p.Config.JitterBufDecayHalftime,
					playerCheckInterval,
					p.CurrentJitterBufDuration,
				)
			}

			lag := l - pos
			logger.Logf(ctx, traceLogLevel, "StreamPlayer[%s].controllerLoop: speed == %v; lag == %v; protocol == %v; jitterBuf == %v", p.StreamID, curSpeed, lag, protocol, p.CurrentJitterBufDuration)

			// [ lag < jitBuf ]
			if enableSlowDown && protocol == streamtypes.ServerTypeRTMP && lag < p.CurrentJitterBufDuration {
				if !stalling {
					jitterBufDurationIncreaseNew := p.CurrentJitterBufDuration/2 - lag
					if jitterBufDurationIncreaseNew < jitterBufDurationIncrease {
						if jitterBufDurationIncreaseNew < 0 {
							jitterBufDurationIncreaseNew = 0
						}
						commitIncreaseNowDuration := jitterBufDurationIncrease - jitterBufDurationIncreaseNew
						logger.Logf(ctx, traceLogLevel,
							"StreamPlayer[%s].controllerLoop: increasing the jitter buffer by %v (%v - %v)",
							p.StreamID,
							commitIncreaseNowDuration,
							jitterBufDurationIncrease, jitterBufDurationIncreaseNew,
						)
						increaseJitterBufferBy(commitIncreaseNowDuration)
						jitterBufDurationIncrease = jitterBufDurationIncreaseNew
					}
				}
				k := lag.Seconds() / p.CurrentJitterBufDuration.Seconds()
				wantSpeed := 1 - (1-k)*(1-p.Config.MinSpeed)
				if wantSpeed <= 0 {
					logger.Errorf(ctx, "invalid wantSpeed: %f", wantSpeed)
					return nil
				}
				setSpeed := p.WantSpeedAverage.Update(wantSpeed)
				if math.Abs(wantSpeed-curSpeed) < p.Config.MinSpeedDifference {
					logger.Logf(ctx, traceLogLevel,
						"StreamPlayer[%s].controllerLoop: want to slow down to %f, but the difference is too small vs current speed %f",
						p.StreamID, setSpeed, curSpeed,
					)
					return nil
				}
				curSpeed = setSpeed
				logger.Debugf(ctx,
					"StreamPlayer[%s].controllerLoop: slowing down to %f",
					p.StreamID, curSpeed,
				)
				err := player.SetSpeed(ctx, curSpeed)
				if err != nil {
					logger.Errorf(ctx, "unable to slow down to %f: %v", curSpeed, err)
					return nil
				}
				time.Sleep(100 * time.Millisecond) // let it catch up at least a bit, before changing the speed back (to avoid flickering)
				return nil
			}
			if jitterBufDurationIncrease > 0 {
				commitJitterBufferIncrease()
			}

			// [ jitBuf <= lag ]

			wantSpeed := float64(1) +
				(p.Config.CatchupMaxSpeedFactor-float64(1))*
					(lag-p.CurrentJitterBufDuration).Seconds()/
					(p.Config.CatchupAtMaxLag-p.CurrentJitterBufDuration+time.Millisecond).Seconds()

			setSpeed := p.WantSpeedAverage.Update(wantSpeed)
			logger.Logf(ctx, traceLogLevel,
				"StreamPlayer[%s].controllerLoop: optimal speed %f (wantSpeed == %f)",
				p.StreamID, setSpeed, wantSpeed,
			)
			setSpeed = float64(uint(setSpeed*200)) / 200 // to avoid flickering (for example between 1.0001 and 1.0000)

			if setSpeed > p.Config.CatchupMaxSpeedFactor {
				logger.Warnf(ctx,
					"speed is calculated higher than the maximum: %v > %v: (%v-1)*(%v-%v)/(%v-%v+1ms); lag calculation: %v - %v",
					setSpeed,
					p.Config.CatchupMaxSpeedFactor,
					p.Config.CatchupMaxSpeedFactor,
					lag.Seconds(),
					p.CurrentJitterBufDuration.Seconds(),
					p.Config.CatchupAtMaxLag.Seconds(),
					p.CurrentJitterBufDuration.Seconds(),
					l, pos,
				)
				setSpeed = p.Config.CatchupMaxSpeedFactor
			}

			if setSpeed == curSpeed {
				return nil
			}

			logger.Debugf(
				ctx,
				"StreamPlayer[%s].controllerLoop: setting the speed to %v: lag: %v - %v == %v",
				p.StreamID, setSpeed, l, pos, lag,
			)
			err = player.SetSpeed(ctx, setSpeed)
			if err != nil {
				logger.Errorf(ctx, "unable to set the speed to %v: %v", setSpeed, err)
				return nil
			}
			curSpeed = setSpeed
			return nil
		})
		if err != nil {
			logger.Error(ctx, "unable to get the player: %v", err)
			return
		}
	}
}

type ErrNilPlayer struct{}

func (e ErrNilPlayer) Error() string {
	return "p.Player is nil"
}

func (p *StreamPlayerHandler) SetJitterBuffer(
	ctx context.Context,
	size time.Duration,
	seekToOptimum bool,
) (_err error) {
	p.CurrentJitterBufDuration = min(max(p.Config.JitterBufMinDuration, size), p.Config.JitterBufMaxDuration)
	if !seekToOptimum {
		return nil
	}
	err := p.withPlayer(ctx, func(ctx context.Context, player player.Player) error {
		l, err := player.GetLength(ctx)
		if err != nil {
			return fmt.Errorf("unable to get length for seeking to end: %w", err)
		}
		if err := player.Seek(ctx, l-size, false, true); err != nil {
			return fmt.Errorf("unable to seek to end: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("unable to seek to end: %w", err)
	}
	return nil
}

func (p *StreamPlayerHandler) withPlayer(
	ctx context.Context,
	fn func(context.Context, player.Player) error,
) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	return xsync.DoR1(ctx, &p.PlayerLocker, func() error {
		if p.Player == nil {
			return ErrNilPlayer{}
		}
		return fn(ctx, p.Player)
	})
}

func (p *StreamPlayerHandler) Close() error {
	ctx := context.TODO()
	return xsync.DoA1R1(ctx, &p.PlayerLocker, p.close, ctx)
}

func (p *StreamPlayerHandler) close(ctx context.Context) error {
	var err *multierror.Error
	if p.Cancel != nil {
		p.Cancel()
	}

	if p.Player != nil {
		err = multierror.Append(err, p.Player.Close(ctx))
		p.Player = nil
	}
	return err.ErrorOrNil()
}
