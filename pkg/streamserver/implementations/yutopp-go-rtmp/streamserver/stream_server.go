// This implementation does not stream more than 280 minutes, because of
// some bug with implementing the extended timestamp of RTMP.

package streamserver

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/url"
	"sort"
	"time"

	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/hashicorp/go-multierror"
	"github.com/xaionaro-go/streamctl/pkg/observability"
	"github.com/xaionaro-go/streamctl/pkg/player"
	playertypes "github.com/xaionaro-go/streamctl/pkg/player/types"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/youtube"
	"github.com/xaionaro-go/streamctl/pkg/streamd/api"
	"github.com/xaionaro-go/streamctl/pkg/streamd/memoize"
	"github.com/xaionaro-go/streamctl/pkg/streamplayer"
	sptypes "github.com/xaionaro-go/streamctl/pkg/streamplayer/types"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/implementations/yutopp-go-rtmp/streamforward"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/types"
	"github.com/xaionaro-go/streamctl/pkg/streamtypes"
	"github.com/xaionaro-go/streamctl/pkg/xcontext"
	"github.com/xaionaro-go/streamctl/pkg/xlogger"
	"github.com/xaionaro-go/streamctl/pkg/xsync"
	"github.com/xaionaro-go/typing/ordered"
	"github.com/yutopp/go-rtmp"
)

type PlatformsController interface {
	CheckStreamStartedByURL(ctx context.Context, destination *url.URL) (bool, error)
	CheckStreamStartedByPlatformID(ctx context.Context, platID streamcontrol.PlatformName) (bool, error)
}

type BrowserOpener interface {
	OpenURL(ctx context.Context, url string) error
}

type ForwardingKey struct {
	StreamID      types.StreamID
	DestinationID types.DestinationID
}

type StreamServer struct {
	xsync.Mutex
	Config                  *types.Config
	RelayService            *RelayService
	ServerHandlers          []types.PortServer
	StreamDestinations      []types.StreamDestination
	ActiveStreamForwardings map[ForwardingKey]*streamforward.ActiveStreamForwarding
	PlatformsController     PlatformsController
	BrowserOpener           BrowserOpener
	StreamPlayers           *streamplayer.StreamPlayers
	StreamForwards          *streamforward.StreamForwards
}

var _ streamforward.StreamServer = (*StreamServer)(nil)

func New(
	cfg *types.Config,
	platformsController PlatformsController,
	browserOpener BrowserOpener,
) *StreamServer {
	s := &StreamServer{
		RelayService: NewRelayService(),
		Config:       cfg,

		ActiveStreamForwardings: map[ForwardingKey]*streamforward.ActiveStreamForwarding{},
		PlatformsController:     platformsController,
		BrowserOpener:           browserOpener,
		StreamForwards:          streamforward.NewStreamForwards(),
	}
	s.StreamPlayers = streamplayer.New(
		s,
		player.NewManager(playertypes.OptionPathToMPV(cfg.VideoPlayer.MPV.Path)),
	)
	return s
}

func (s *StreamServer) Init(
	ctx context.Context,
	opts ...types.InitOption,
) (_err error) {
	logger.Debugf(ctx, "Init")
	defer func() { logger.Debugf(ctx, "/Init: %v", _err) }()

	ctx = belt.WithField(ctx, "module", "StreamServer")
	return xsync.DoR1(ctx, &s.Mutex, func() error {
		return s.init(ctx, opts...)
	})
}

func (s *StreamServer) init(
	ctx context.Context,
	opts ...types.InitOption,
) (_err error) {
	initCfg := types.InitOptions(opts).Config()

	cfg := s.Config
	logger.Debugf(ctx, "config == %#+v", *cfg)

	for _, srv := range cfg.Servers {
		{
			srv := srv
			observability.Go(ctx, func() {
				err := s.startServer(ctx, srv.Type, srv.Listen)
				if err != nil {
					logger.Errorf(ctx, "unable to initialize %s server at %s: %w", srv.Type, srv.Listen, err)
				}
			})
		}
	}

	for dstID, dstCfg := range cfg.Destinations {
		err := s.addStreamDestination(ctx, dstID, dstCfg.URL)
		if err != nil {
			return fmt.Errorf("unable to initialize stream destination '%s' to %#+v: %w", dstID, dstCfg, err)
		}
	}

	for streamID, streamCfg := range cfg.Streams {
		err := s.addIncomingStream(ctx, streamID)
		if err != nil {
			return fmt.Errorf("unable to initialize stream '%s': %w", streamID, err)
		}

		for dstID, fwd := range streamCfg.Forwardings {
			if !fwd.Disabled {
				_, err := s.addStreamForward(ctx, streamID, dstID, fwd.Quirks)
				if err != nil {
					return fmt.Errorf("unable to launch stream forward from '%s' to '%s': %w", streamID, dstID, err)
				}
			}
		}
	}

	observability.Go(ctx, func() {
		var opts setupStreamPlayersOptions
		if initCfg.DefaultStreamPlayerOptions != nil {
			opts = append(
				opts,
				setupStreamPlayersOptionDefaultStreamPlayerOptions(
					initCfg.DefaultStreamPlayerOptions,
				),
			)
		}
		err := s.setupStreamPlayers(ctx, opts...)
		if err != nil {
			logger.Error(ctx, err)
		}
	})

	return nil
}

type setupStreamPlayersConfig struct {
	DefaultStreamPlayerOptions streamplayer.Options
}

type setupStreamPlayersOption interface {
	apply(*setupStreamPlayersConfig)
}

type setupStreamPlayersOptions []setupStreamPlayersOption

func (s setupStreamPlayersOptions) Config() setupStreamPlayersConfig {
	cfg := setupStreamPlayersConfig{}
	for _, opt := range s {
		opt.apply(&cfg)
	}
	return cfg
}

type setupStreamPlayersOptionDefaultStreamPlayerOptions streamplayer.Options

func (opt setupStreamPlayersOptionDefaultStreamPlayerOptions) apply(cfg *setupStreamPlayersConfig) {
	cfg.DefaultStreamPlayerOptions = (streamplayer.Options)(opt)
}
func (s *StreamServer) setupStreamPlayers(
	ctx context.Context,
	opts ...setupStreamPlayersOption,
) (_err error) {
	defer func() { logger.Debugf(ctx, "setupStreamPlayers result: %v", _err) }()

	setupCfg := setupStreamPlayersOptions(opts).Config()

	var streamIDsToDelete []api.StreamID

	curPlayers := map[api.StreamID]*streamplayer.StreamPlayerHandler{}
	s.StreamPlayers.StreamPlayersLocker.Do(ctx, func() {
		for _, player := range s.StreamPlayers.StreamPlayers {
			streamCfg, ok := s.Config.Streams[types.StreamID(player.StreamID)]
			if !ok || streamCfg.Player == nil || streamCfg.Player.Disabled {
				streamIDsToDelete = append(streamIDsToDelete, player.StreamID)
				continue
			}
			curPlayers[player.StreamID] = player
		}
	})

	var result *multierror.Error

	logger.Debugf(ctx, "streamIDsToDelete == %#+v", streamIDsToDelete)

	for _, streamID := range streamIDsToDelete {
		err := s.StreamPlayers.Remove(ctx, streamID)
		if err != nil {
			err = fmt.Errorf("unable to remove stream '%s': %w", streamID, err)
			logger.Warnf(ctx, "%s", err)
			result = multierror.Append(result, err)
		} else {
			logger.Infof(ctx, "stopped the player for stream '%s'", streamID)
		}
	}

	for streamID, streamCfg := range s.Config.Streams {
		playerCfg := streamCfg.Player
		if playerCfg == nil || playerCfg.Disabled {
			continue
		}

		if _, ok := curPlayers[streamID]; ok {
			continue
		}

		ssOpts := playerCfg.StreamPlayback.Options()
		if setupCfg.DefaultStreamPlayerOptions != nil {
			ssOpts = append(ssOpts, setupCfg.DefaultStreamPlayerOptions...)
		}
		ssOpts = append(ssOpts, sptypes.OptionGetRestartChanFunc(func() <-chan struct{} {
			pubSub := s.RelayService.GetPubsub(types.StreamID2LocalAppName(streamID))
			if pubSub == nil {
				return nil
			}
			return pubSub.PublisherHandler().ClosedChan()
		}))
		_, err := s.StreamPlayers.Create(
			xcontext.DetachDone(ctx),
			streamID,
			ssOpts...,
		)
		if err != nil {
			err = fmt.Errorf("unable to create a stream player for stream '%s': %w", streamID, err)
			logger.Warnf(ctx, "%s", err)
			result = multierror.Append(result, err)
			continue
		} else {
			logger.Infof(ctx, "started a player for stream '%s'", streamID)
		}
	}

	return result.ErrorOrNil()
}

func (s *StreamServer) WaitPubsub(
	ctx context.Context,
	appKey types.AppKey,
) streamforward.Pubsub {
	return &pubsubAdapter{s.RelayService.WaitPubsub(ctx, appKey)}
}

func (s *StreamServer) PubsubNames() types.AppKeys {
	return s.RelayService.PubsubNames()
}

func (s *StreamServer) ListServers(
	ctx context.Context,
) (_ret []types.PortServer) {
	ctx = belt.WithField(ctx, "module", "StreamServer")
	logger.Tracef(ctx, "ListServers")
	defer func() { logger.Tracef(ctx, "/ListServers: %d servers", len(_ret)) }()

	return xsync.DoR1(ctx, &s.Mutex, func() []types.PortServer {
		c := make([]types.PortServer, len(s.ServerHandlers))
		copy(c, s.ServerHandlers)
		return c
	})
}

func (s *StreamServer) StartServer(
	ctx context.Context,
	serverType streamtypes.ServerType,
	listenAddr string,
) error {
	ctx = belt.WithField(ctx, "module", "StreamServer")
	return xsync.DoR1(ctx, &s.Mutex, func() error {
		err := s.startServer(ctx, serverType, listenAddr)
		if err != nil {
			return err
		}
		s.Config.Servers = append(s.Config.Servers, types.Server{
			Type:   serverType,
			Listen: listenAddr,
		})
		return nil
	})
}

func (s *StreamServer) startServer(
	ctx context.Context,
	serverType streamtypes.ServerType,
	listenAddr string,
) (_ret error) {
	logger.Tracef(ctx, "startServer(%s, '%s')", serverType, listenAddr)
	defer func() { logger.Tracef(ctx, "/startServer(%s, '%s'): %v", serverType, listenAddr, _ret) }()
	var srv types.PortServer
	var err error
	switch serverType {
	case streamtypes.ServerTypeRTMP:
		var listener net.Listener
		listener, err = net.Listen("tcp", listenAddr)
		if err != nil {
			err = fmt.Errorf("unable to start listening '%s': %w", listenAddr, err)
			break
		}
		portSrv := &PortServer{
			Listener: listener,
		}
		portSrv.Server = rtmp.NewServer(&rtmp.ServerConfig{
			OnConnect: func(conn net.Conn) (io.ReadWriteCloser, *rtmp.ConnConfig) {
				ctx := belt.WithField(ctx, "client", conn.RemoteAddr().String())
				h := NewHandler(s.RelayService)
				wrcc := types.NewReaderWriterCloseCounter(conn, &portSrv.ReadCount, &portSrv.WriteCount)
				return wrcc, &rtmp.ConnConfig{
					Handler: h,
					ControlState: rtmp.StreamControlStateConfig{
						DefaultBandwidthWindowSize: 20 * 1024 * 1024 / 8,
					},
					Logger: xlogger.LogrusFieldLoggerFromCtx(ctx),
				}
			},
		})
		observability.Go(ctx, func() {
			err = portSrv.Serve(listener)
			if err != nil {
				err = fmt.Errorf("unable to start serving RTMP at '%s': %w", listener.Addr().String(), err)
				logger.Error(ctx, err)
			}
		})
		srv = portSrv
	case streamtypes.ServerTypeRTSP:
		return fmt.Errorf("RTSP is not supported, yet")
	default:
		return fmt.Errorf("unexpected server type %v", serverType)
	}
	if err != nil {
		return err
	}

	s.ServerHandlers = append(s.ServerHandlers, srv)
	return nil
}

func (s *StreamServer) findServer(
	_ context.Context,
	server types.PortServer,
) (int, error) {
	for i := range s.ServerHandlers {
		if s.ServerHandlers[i] == server {
			return i, nil
		}
	}
	return -1, fmt.Errorf("server not found")
}

func (s *StreamServer) StopServer(
	ctx context.Context,
	server types.PortServer,
) error {
	ctx = belt.WithField(ctx, "module", "StreamServer")
	return xsync.DoR1(ctx, &s.Mutex, func() error {
		for idx, srv := range s.Config.Servers {
			if srv.Listen == server.ListenAddr() {
				s.Config.Servers = append(s.Config.Servers[:idx], s.Config.Servers[idx+1:]...)
				break
			}
		}
		return s.stopServer(ctx, server)
	})
}

func (s *StreamServer) stopServer(
	ctx context.Context,
	server types.PortServer,
) error {
	idx, err := s.findServer(ctx, server)
	if err != nil {
		return err
	}

	s.ServerHandlers = append(s.ServerHandlers[:idx], s.ServerHandlers[idx+1:]...)
	return server.Close()
}

func (s *StreamServer) AddIncomingStream(
	ctx context.Context,
	streamID types.StreamID,
) error {
	ctx = belt.WithField(ctx, "module", "StreamServer")
	return xsync.DoR1(ctx, &s.Mutex, func() error {
		err := s.addIncomingStream(ctx, streamID)
		if err != nil {
			return err
		}
		return nil
	})
}

func (s *StreamServer) addIncomingStream(
	_ context.Context,
	streamID types.StreamID,
) error {
	if _, ok := s.Config.Streams[streamID]; ok {
		return nil
	}
	s.Config.Streams[streamID] = &types.StreamConfig{}
	return nil
}

type IncomingStream = types.IncomingStream

func (s *StreamServer) ListIncomingStreams(
	ctx context.Context,
) []IncomingStream {
	ctx = belt.WithField(ctx, "module", "StreamServer")
	_ = ctx
	return xsync.DoA1R1(ctx, &s.Mutex, s.listIncomingStreams, ctx)
}

func (s *StreamServer) listIncomingStreams(
	_ context.Context,
) []IncomingStream {
	var result []IncomingStream
	for streamID := range s.Config.Streams {
		result = append(result, IncomingStream{
			StreamID: streamID,
		})
	}
	return result
}

func (s *StreamServer) RemoveIncomingStream(
	ctx context.Context,
	streamID types.StreamID,
) error {
	ctx = belt.WithField(ctx, "module", "StreamServer")
	return xsync.DoA2R1(ctx, &s.Mutex, s.removeIncomingStream, ctx, streamID)
}

func (s *StreamServer) removeIncomingStream(
	_ context.Context,
	streamID types.StreamID,
) error {
	delete(s.Config.Streams, streamID)
	return nil
}

type StreamForward = types.StreamForward[*streamforward.ActiveStreamForwarding]

func (s *StreamServer) AddStreamForward(
	ctx context.Context,
	streamID types.StreamID,
	destinationID types.DestinationID,
	enabled bool,
	quirks types.ForwardingQuirks,
) (*StreamForward, error) {
	ctx = belt.WithField(ctx, "module", "StreamServer")
	return xsync.DoR2(ctx, &s.Mutex, func() (*StreamForward, error) {
		streamConfig := s.Config.Streams[streamID]
		if _, ok := streamConfig.Forwardings[destinationID]; ok {
			return nil, fmt.Errorf("the forwarding %s->%s already exists", streamID, destinationID)
		}

		streamConfig.Forwardings[destinationID] = types.ForwardingConfig{
			Disabled: !enabled,
			Quirks:   quirks,
		}

		if enabled {
			fwd, err := s.addStreamForward(ctx, streamID, destinationID, quirks)
			if err != nil {
				return nil, err
			}
			return fwd, nil
		}
		return &StreamForward{
			StreamID:      streamID,
			DestinationID: destinationID,
			Enabled:       enabled,
			Quirks:        quirks,
		}, nil
	})
}

func (s *StreamServer) addStreamForward(
	ctx context.Context,
	streamID types.StreamID,
	destinationID types.DestinationID,
	quirks types.ForwardingQuirks,
) (*StreamForward, error) {
	ctx = belt.WithField(ctx, "stream_forward", fmt.Sprintf("%s->%s", streamID, destinationID))
	key := ForwardingKey{
		StreamID:      streamID,
		DestinationID: destinationID,
	}
	if _, ok := s.ActiveStreamForwardings[key]; ok {
		return nil, fmt.Errorf("there is already an active stream forwarding to '%s'", destinationID)
	}

	dst, err := s.findStreamDestinationByID(ctx, destinationID)
	if err != nil {
		return nil, fmt.Errorf("unable to find stream destination '%s': %w", destinationID, err)
	}

	urlParsed, err := url.Parse(dst.URL)
	if err != nil {
		return nil, fmt.Errorf("unable to parse URL '%s': %w", dst.URL, err)
	}

	if urlParsed.Host == "" {
		portSrv := s.ServerHandlers[0]
		urlParsed.Scheme = portSrv.Type().String()
		urlParsed.Host = portSrv.ListenAddr()
	}

	result := &StreamForward{
		StreamID:      streamID,
		DestinationID: destinationID,
		Enabled:       true,
		Quirks:        quirks,
		NumBytesWrote: 0,
		NumBytesRead:  0,
	}

	fwd, err := s.StreamForwards.NewActiveStreamForward(
		ctx,
		s,
		streamID,
		destinationID,
		urlParsed.String(),
		func(ctx context.Context, fwd *streamforward.ActiveStreamForwarding) {
			if quirks.StartAfterYoutubeRecognizedStream.Enabled {
				if quirks.RestartUntilYoutubeRecognizesStream.Enabled {
					logger.Errorf(ctx, "StartAfterYoutubeRecognizedStream should not be used together with RestartUntilYoutubeRecognizesStream")
				} else {
					logger.Debugf(ctx, "fwd %s->%s is waiting for YouTube to recognize the stream", streamID, destinationID)
					started, err := s.PlatformsController.CheckStreamStartedByPlatformID(
						memoize.SetNoCache(ctx, true),
						youtube.ID,
					)
					logger.Debugf(ctx, "youtube status check: %v %v", started, err)
					if started {
						return
					}
					t := time.NewTicker(time.Second)
					for {
						select {
						case <-ctx.Done():
							return
						case <-t.C:
						}
						started, err := s.PlatformsController.CheckStreamStartedByPlatformID(
							ctx,
							youtube.ID,
						)
						logger.Debugf(ctx, "youtube status check: %v %v", started, err)
						if started {
							return
						}
					}
				}
			}
		},
	)
	if err != nil {
		return nil, fmt.Errorf("unable to run the stream forwarding: %w", err)
	}
	s.ActiveStreamForwardings[key] = fwd
	result.ActiveForwarding = fwd

	if quirks.RestartUntilYoutubeRecognizesStream.Enabled {
		observability.Go(ctx, func() {
			s.restartUntilYoutubeRecognizesStream(
				ctx,
				result,
				quirks.RestartUntilYoutubeRecognizesStream,
			)
		})
	}

	return result, nil
}

func (s *StreamServer) restartUntilYoutubeRecognizesStream(
	ctx context.Context,
	fwd *StreamForward,
	cfg types.RestartUntilYoutubeRecognizesStream,
) {
	ctx = belt.WithField(ctx, "module", "restartUntilYoutubeRecognizesStream")
	ctx = belt.WithField(ctx, "stream_forward", fmt.Sprintf("%s->%s", fwd.StreamID, fwd.DestinationID))

	logger.Debugf(ctx, "restartUntilYoutubeRecognizesStream(ctx, %#+v, %#+v)", fwd, cfg)
	defer func() { logger.Debugf(ctx, "restartUntilYoutubeRecognizesStream(ctx, %#+v, %#+v)", fwd, cfg) }()

	if !cfg.Enabled {
		logger.Errorf(ctx, "an attempt to start restartUntilYoutubeRecognizesStream when the hack is disabled for this stream forwarder: %#+v", cfg)
		return
	}

	if s.PlatformsController == nil {
		logger.Errorf(ctx, "PlatformsController is nil")
		return
	}

	if fwd.ActiveForwarding == nil {
		logger.Error(ctx, "ActiveForwarding is nil")
		return
	}

	_, err := fwd.ActiveForwarding.WaitForPublisher(ctx)
	if err != nil {
		logger.Error(ctx, err)
		return
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(cfg.StartTimeout):
		}
		logger.Debugf(ctx, "waited %v, checking if the remote platform accepted the stream", cfg.StartTimeout)

		for {
			streamOK, err := s.PlatformsController.CheckStreamStartedByPlatformID(
				memoize.SetNoCache(ctx, true),
				youtube.ID,
			)
			logger.Debugf(ctx, "the result of checking the stream on the remote platform: %v %v", streamOK, err)
			if err != nil {
				logger.Errorf(ctx, "unable to check if the stream with URL '%s' is started: %v", fwd.ActiveForwarding.URL, err)
				time.Sleep(time.Second)
				continue
			}
			if streamOK {
				logger.Debugf(ctx, "waiting %v to recheck if the stream will be still OK", cfg.StopStartDelay)
				select {
				case <-ctx.Done():
					return
				case <-time.After(cfg.StopStartDelay):
				}
				streamOK, err := s.PlatformsController.CheckStreamStartedByPlatformID(
					memoize.SetNoCache(ctx, true),
					youtube.ID,
				)
				logger.Debugf(ctx, "the result of checking the stream on the remote platform: %v %v", streamOK, err)
				if err != nil {
					logger.Errorf(ctx, "unable to check if the stream with URL '%s' is started: %v", fwd.ActiveForwarding.URL, err)
					time.Sleep(time.Second)
					continue
				}
				if streamOK {
					return
				}
			}
			break
		}

		logger.Infof(ctx, "the remote platform still does not see the stream, restarting the stream forwarding: stopping...")

		err := fwd.ActiveForwarding.Stop()
		if err != nil {
			logger.Errorf(ctx, "unable to stop stream forwarding: %v", err)
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(cfg.StopStartDelay):
		}

		logger.Infof(ctx, "the remote platform still does not see the stream, restarting the stream forwarding: starting...")

		err = fwd.ActiveForwarding.Start(ctx)
		if err != nil {
			logger.Errorf(ctx, "unable to start stream forwarding: %v", err)
		}
	}
}

func (s *StreamServer) UpdateStreamForward(
	ctx context.Context,
	streamID types.StreamID,
	destinationID types.DestinationID,
	enabled bool,
	quirks types.ForwardingQuirks,
) (*StreamForward, error) {
	ctx = belt.WithField(ctx, "module", "StreamServer")
	return xsync.DoR2(ctx, &s.Mutex, func() (*StreamForward, error) {
		streamConfig := s.Config.Streams[streamID]
		fwdCfg, ok := streamConfig.Forwardings[destinationID]
		if !ok {
			return nil, fmt.Errorf("the forwarding %s->%s does not exist", streamID, destinationID)
		}

		var fwd *StreamForward
		if fwdCfg.Disabled && enabled {
			var err error
			fwd, err = s.addStreamForward(ctx, streamID, destinationID, quirks)
			if err != nil {
				return nil, fmt.Errorf("unable to active the stream: %w", err)
			}
		}
		if !fwdCfg.Disabled && !enabled {
			err := s.removeStreamForward(ctx, streamID, destinationID)
			if err != nil {
				return nil, fmt.Errorf("unable to deactivate the stream: %w", err)
			}
		}
		streamConfig.Forwardings[destinationID] = types.ForwardingConfig{
			Disabled: !enabled,
			Quirks:   quirks,
		}

		r := &StreamForward{
			StreamID:      streamID,
			DestinationID: destinationID,
			Enabled:       enabled,
			Quirks:        quirks,
			NumBytesWrote: 0,
			NumBytesRead:  0,
		}
		if fwd != nil {
			r.ActiveForwarding = fwd.ActiveForwarding
		}
		return r, nil
	})
}

func (s *StreamServer) ListStreamForwards(
	ctx context.Context,
) (_ret []StreamForward, _err error) {
	ctx = belt.WithField(ctx, "module", "StreamServer")
	defer func() {
		logger.Tracef(ctx, "/ListStreamForwards(): %#+v %v", _ret, _err)
	}()

	return xsync.DoR2(ctx, &s.Mutex, func() ([]StreamForward, error) {
		return s.getStreamForwards(ctx, func(si types.StreamID, di ordered.Optional[types.DestinationID]) bool {
			return true
		})
	})
}

func (s *StreamServer) getStreamForwards(
	ctx context.Context,
	filterFunc func(types.StreamID, ordered.Optional[types.DestinationID]) bool,
) (_ret []StreamForward, _err error) {
	activeStreamForwards, err := s.listActiveStreamForwards(ctx)
	if err != nil {
		return nil, fmt.Errorf("unable to get the list of active stream forwardings: %w", err)
	}
	logger.Tracef(ctx, "len(activeStreamForwards) == %d", len(activeStreamForwards))

	type fwdID struct {
		StreamID types.StreamID
		DestID   types.DestinationID
	}
	m := map[fwdID]*StreamForward{}
	for idx := range activeStreamForwards {
		fwd := &activeStreamForwards[idx]
		if !filterFunc(fwd.StreamID, ordered.Opt(fwd.DestinationID)) {
			continue
		}
		m[fwdID{
			StreamID: fwd.StreamID,
			DestID:   fwd.DestinationID,
		}] = fwd
	}

	logger.Tracef(ctx, "len(s.Config.Streams) == %d", len(s.Config.Streams))
	var result []StreamForward
	for streamID, stream := range s.Config.Streams {
		if !filterFunc(streamID, ordered.Optional[types.DestinationID]{}) {
			continue
		}
		logger.Tracef(ctx, "len(s.Config.Streams[%s].Forwardings) == %d", streamID, len(stream.Forwardings))
		for dstID, cfg := range stream.Forwardings {
			if !filterFunc(streamID, ordered.Opt(dstID)) {
				continue
			}
			item := StreamForward{
				StreamID:      streamID,
				DestinationID: dstID,
				Enabled:       !cfg.Disabled,
				Quirks:        cfg.Quirks,
			}
			if activeFwd, ok := m[fwdID{
				StreamID: streamID,
				DestID:   dstID,
			}]; ok {
				item.NumBytesWrote = activeFwd.NumBytesWrote
				item.NumBytesRead = activeFwd.NumBytesRead
			}
			logger.Tracef(ctx, "stream forwarding '%s->%s': %#+v", streamID, dstID, cfg)
			result = append(result, item)
		}
	}
	return result, nil
}

func (s *StreamServer) listActiveStreamForwards(
	_ context.Context,
) ([]StreamForward, error) {
	var result []StreamForward
	for _, fwd := range s.ActiveStreamForwardings {
		result = append(result, StreamForward{
			StreamID:      fwd.StreamID,
			DestinationID: fwd.DestinationID,
			Enabled:       true,
			NumBytesWrote: fwd.WriteCount.Load(),
			NumBytesRead:  fwd.ReadCount.Load(),
		})
	}
	return result, nil
}

func (s *StreamServer) RemoveStreamForward(
	ctx context.Context,
	streamID types.StreamID,
	dstID types.DestinationID,
) error {
	ctx = belt.WithField(ctx, "module", "StreamServer")
	return xsync.DoR1(ctx, &s.Mutex, func() error {
		streamCfg := s.Config.Streams[streamID]
		if _, ok := streamCfg.Forwardings[dstID]; !ok {
			return fmt.Errorf("the forwarding %s->%s does not exist", streamID, dstID)
		}
		delete(streamCfg.Forwardings, dstID)
		return s.removeStreamForward(ctx, streamID, dstID)
	})
}

func (s *StreamServer) removeStreamForward(
	_ context.Context,
	streamID types.StreamID,
	dstID types.DestinationID,
) error {
	key := ForwardingKey{
		StreamID:      streamID,
		DestinationID: dstID,
	}

	fwd := s.ActiveStreamForwardings[key]
	if fwd == nil {
		return nil
	}

	delete(s.ActiveStreamForwardings, key)
	err := fwd.Close()
	if err != nil {
		return fmt.Errorf("unable to close stream forwarding: %w", err)
	}

	return nil
}

func (s *StreamServer) GetStreamForwardsByDestination(
	ctx context.Context,
	destID types.DestinationID,
) (_ret []StreamForward, _err error) {
	ctx = belt.WithField(ctx, "module", "StreamServer")
	logger.Debugf(ctx, "GetStreamForwardsByDestination()")
	defer func() {
		logger.Debugf(ctx, "/GetStreamForwardsByDestination(): %#+v %v", _ret, _err)
	}()

	return xsync.DoR2(ctx, &s.Mutex, func() ([]StreamForward, error) {
		return s.getStreamForwards(ctx, func(streamID types.StreamID, dstID ordered.Optional[types.DestinationID]) bool {
			return !dstID.IsSet() || dstID.Get() == destID
		})
	})
}

func (s *StreamServer) ListStreamDestinations(
	ctx context.Context,
) ([]types.StreamDestination, error) {
	ctx = belt.WithField(ctx, "module", "StreamServer")
	return xsync.DoA1R2(ctx, &s.Mutex, s.listStreamDestinations, ctx)
}

func (s *StreamServer) listStreamDestinations(
	_ context.Context,
) ([]types.StreamDestination, error) {
	c := make([]types.StreamDestination, len(s.StreamDestinations))
	copy(c, s.StreamDestinations)
	return c, nil
}

func (s *StreamServer) AddStreamDestination(
	ctx context.Context,
	destinationID types.DestinationID,
	url string,
) error {
	ctx = belt.WithField(ctx, "module", "StreamServer")
	return xsync.DoR1(ctx, &s.Mutex, func() error {
		err := s.addStreamDestination(ctx, destinationID, url)
		if err != nil {
			return err
		}
		s.Config.Destinations[destinationID] = &types.DestinationConfig{URL: url}
		return nil
	})
}

func (s *StreamServer) addStreamDestination(
	_ context.Context,
	destinationID types.DestinationID,
	url string,
) error {
	s.StreamDestinations = append(s.StreamDestinations, types.StreamDestination{
		ID:  destinationID,
		URL: url,
	})
	return nil
}

func (s *StreamServer) RemoveStreamDestination(
	ctx context.Context,
	destinationID types.DestinationID,
) error {
	ctx = belt.WithField(ctx, "module", "StreamServer")
	return xsync.DoR1(ctx, &s.Mutex, func() error {
		for _, streamCfg := range s.Config.Streams {
			delete(streamCfg.Forwardings, destinationID)
		}
		delete(s.Config.Destinations, destinationID)
		return s.removeStreamDestination(ctx, destinationID)
	})
}

func (s *StreamServer) removeStreamDestination(
	ctx context.Context,
	destinationID types.DestinationID,
) error {
	streamForwards, err := s.getStreamForwards(
		ctx,
		func(
			streamID types.StreamID,
			dstID ordered.Optional[types.DestinationID],
		) bool {
			return !dstID.IsSet() || dstID.Get() == destinationID
		})
	if err != nil {
		return fmt.Errorf("unable to list stream forwardings: %w", err)
	}
	for _, fwd := range streamForwards {
		if fwd.DestinationID == destinationID {
			s.removeStreamForward(ctx, fwd.StreamID, fwd.DestinationID)
		}
	}

	for i := range s.StreamDestinations {
		if s.StreamDestinations[i].ID == destinationID {
			s.StreamDestinations = append(s.StreamDestinations[:i], s.StreamDestinations[i+1:]...)
			return nil
		}
	}

	return fmt.Errorf("have not found stream destination with id %s", destinationID)
}

func (s *StreamServer) findStreamDestinationByID(
	_ context.Context,
	destinationID types.DestinationID,
) (types.StreamDestination, error) {
	for _, dst := range s.StreamDestinations {
		if dst.ID == destinationID {
			return dst, nil
		}
	}
	return types.StreamDestination{}, fmt.Errorf("unable to find a stream destination by StreamID '%s'", destinationID)
}

func (s *StreamServer) WaitPublisherChan(
	ctx context.Context,
	streamID types.StreamID,
) (<-chan struct{}, error) {

	ch := make(chan struct{})
	observability.Go(ctx, func() {
		s.RelayService.WaitPubsub(ctx, types.StreamID2LocalAppName(streamID))
		close(ch)
	})
	return ch, nil
}

func (s *StreamServer) GetPortServers(
	ctx context.Context,
) ([]streamplayer.StreamPortServer, error) {
	srvs := s.ListServers(ctx)

	result := make([]streamplayer.StreamPortServer, 0, len(srvs))
	for _, srv := range srvs {
		result = append(result, streamplayer.StreamPortServer{
			Addr: srv.ListenAddr(),
			Type: srv.Type(),
		})
	}

	return result, nil
}

func (s *StreamServer) GetStreamPlayer(
	ctx context.Context,
	streamID types.StreamID,
) (*types.StreamPlayer, error) {
	streamCfg, ok := s.Config.Streams[streamID]
	if !ok {
		return nil, fmt.Errorf("no stream '%s'", streamID)
	}
	playerCfg := streamCfg.Player
	if playerCfg == nil {
		return nil, fmt.Errorf("no stream player defined for '%s'", streamID)
	}
	return &api.StreamPlayer{
		StreamID:             streamID,
		PlayerType:           playerCfg.Player,
		Disabled:             playerCfg.Disabled,
		StreamPlaybackConfig: playerCfg.StreamPlayback,
	}, nil
}

func (s *StreamServer) ListStreamPlayers(
	ctx context.Context,
) ([]types.StreamPlayer, error) {
	var result []api.StreamPlayer
	if s == nil {
		return nil, fmt.Errorf("StreamServer == nil")
	}
	if s.Config == nil {
		return nil, fmt.Errorf("s.Config == nil")
	}
	if s.Config.Streams == nil {
		return nil, fmt.Errorf("s.Config.Streams == nil")
	}
	for streamID, streamCfg := range s.Config.Streams {
		playerCfg := streamCfg.Player
		if playerCfg == nil {
			continue
		}

		result = append(result, api.StreamPlayer{
			StreamID:             streamID,
			PlayerType:           playerCfg.Player,
			Disabled:             playerCfg.Disabled,
			StreamPlaybackConfig: playerCfg.StreamPlayback,
		})
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].StreamID < result[j].StreamID
	})
	return result, nil
}

func (s *StreamServer) AddStreamPlayer(
	ctx context.Context,
	streamID types.StreamID,
	playerType player.Backend,
	disabled bool,
	streamPlaybackConfig sptypes.Config,
	opts ...types.StreamPlayerOption,
) error {
	return s.setStreamPlayer(
		ctx,
		streamID,
		playerType,
		disabled,
		streamPlaybackConfig,
		opts...,
	)
}

func (s *StreamServer) GetActiveStreamPlayer(
	ctx context.Context,
	streamID types.StreamID,
) (player.Player, error) {
	p := s.StreamPlayers.Get(streamID)
	if p == nil {
		return nil, fmt.Errorf("there is no player setup for '%s'", streamID)
	}
	return p.Player, nil
}

func (s *StreamServer) RemoveStreamPlayer(
	ctx context.Context,
	streamID types.StreamID,
) error {
	if _, ok := s.Config.Streams[streamID]; !ok {
		return nil
	}
	s.Config.Streams[streamID].Player = nil
	return s.setupStreamPlayers(ctx)
}

func (s *StreamServer) UpdateStreamPlayer(
	ctx context.Context,
	streamID types.StreamID,
	playerType player.Backend,
	disabled bool,
	streamPlaybackConfig sptypes.Config,
	opts ...types.StreamPlayerOption,
) error {
	return s.setStreamPlayer(
		ctx,
		streamID,
		playerType,
		disabled,
		streamPlaybackConfig,
		opts...,
	)
}

func (s *StreamServer) setStreamPlayer(
	ctx context.Context,
	streamID streamtypes.StreamID,
	playerType player.Backend,
	disabled bool,
	streamPlaybackConfig sptypes.Config,
	opts ...types.StreamPlayerOption,
) (_err error) {
	defer func() { logger.Debugf(ctx, "setStreamPlayer result: %v", _err) }()

	cfg := types.StreamPlayerOptions(opts).Config()

	s.Config.Streams[streamID].Player = &types.PlayerConfig{
		Player:         playerType,
		Disabled:       disabled,
		StreamPlayback: streamPlaybackConfig,
	}

	{
		var opts setupStreamPlayersOptions
		if cfg.DefaultStreamPlayerOptions != nil {
			opts = append(opts, setupStreamPlayersOptionDefaultStreamPlayerOptions(cfg.DefaultStreamPlayerOptions))
		}
		if err := s.setupStreamPlayers(ctx, opts...); err != nil {
			return fmt.Errorf("unable to setup the stream players: %w", err)
		}
	}

	return nil
}
