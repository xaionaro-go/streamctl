package streamserver

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/url"
	"time"

	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/lockmap"
	"github.com/xaionaro-go/streamctl/pkg/observability"
	"github.com/xaionaro-go/streamctl/pkg/player"
	playertypes "github.com/xaionaro-go/streamctl/pkg/player/types"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/youtube"
	"github.com/xaionaro-go/streamctl/pkg/streamd/memoize"
	"github.com/xaionaro-go/streamctl/pkg/streamplayer"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/types"
	"github.com/xaionaro-go/streamctl/pkg/streamtypes"
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
	Config                     *types.Config
	RelayService               *RelayService
	ServerHandlers             []types.PortServer
	StreamDestinations         []types.StreamDestination
	ActiveStreamForwardings    map[ForwardingKey]*ActiveStreamForwarding
	PlatformsController        PlatformsController
	BrowserOpener              BrowserOpener
	StreamPlayers              *streamplayer.StreamPlayers
	DestinationStreamingLocker *lockmap.LockMap
}

func New(
	cfg *types.Config,
	platformsController PlatformsController,
	browserOpener BrowserOpener,
) *StreamServer {
	s := &StreamServer{
		RelayService: NewRelayService(),
		Config:       cfg,

		ActiveStreamForwardings:    map[ForwardingKey]*ActiveStreamForwarding{},
		PlatformsController:        platformsController,
		BrowserOpener:              browserOpener,
		DestinationStreamingLocker: lockmap.NewLockMap(),
	}
	s.StreamPlayers = streamplayer.New(
		NewStreamPlayerStreamServer(s),
		player.NewManager(playertypes.OptionPathToMPV(cfg.VideoPlayer.MPV.Path)),
	)
	return s
}

type InitConfig struct {
	DefaultStreamPlayerOptions streamplayer.Options
}

type InitOption interface {
	apply(*InitConfig)
}

type InitOptions []InitOption

func (s InitOptions) Config() InitConfig {
	cfg := InitConfig{}
	for _, opt := range s {
		opt.apply(&cfg)
	}
	return cfg
}

type InitOptionDefaultStreamPlayerOptions streamplayer.Options

func (opt InitOptionDefaultStreamPlayerOptions) apply(cfg *InitConfig) {
	cfg.DefaultStreamPlayerOptions = (streamplayer.Options)(opt)
}

func (s *StreamServer) Init(
	ctx context.Context,
	opts ...InitOption,
) error {
	initCfg := InitOptions(opts).Config()

	ctx = belt.WithField(ctx, "module", "StreamServer")
	s.Lock()
	defer s.Unlock()

	cfg := s.Config
	logger.Debugf(ctx, "config == %#+v", *cfg)

	for _, srv := range cfg.Servers {
		err := s.startServer(ctx, srv.Type, srv.Listen)
		if err != nil {
			return fmt.Errorf("unable to initialize %s server at %s: %w", srv.Type, srv.Listen, err)
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
			opts = append(opts, setupStreamPlayersOptionDefaultStreamPlayerOptions(initCfg.DefaultStreamPlayerOptions))
		}
		err := s.setupStreamPlayers(ctx, opts...)
		if err != nil {
			logger.Error(ctx, err)
		}
	})

	return nil
}

func (s *StreamServer) ListServers(
	ctx context.Context,
) (_ret []types.PortServer) {
	ctx = belt.WithField(ctx, "module", "StreamServer")
	logger.Tracef(ctx, "ListServers")
	defer func() { logger.Tracef(ctx, "/ListServers: %d servers", len(_ret)) }()
	s.Lock()
	defer s.Unlock()
	c := make([]types.PortServer, len(s.ServerHandlers))
	copy(c, s.ServerHandlers)
	return c
}

func (s *StreamServer) StartServer(
	ctx context.Context,
	serverType streamtypes.ServerType,
	listenAddr string,
) error {
	ctx = belt.WithField(ctx, "module", "StreamServer")
	s.Lock()
	defer s.Unlock()
	err := s.startServer(ctx, serverType, listenAddr)
	if err != nil {
		return err
	}
	s.Config.Servers = append(s.Config.Servers, types.Server{
		Type:   serverType,
		Listen: listenAddr,
	})
	return nil
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
				h := &Handler{
					relayService: s.RelayService,
				}
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
	s.Lock()
	defer s.Unlock()
	for idx, srv := range s.Config.Servers {
		if srv.Listen == server.ListenAddr() {
			s.Config.Servers = append(s.Config.Servers[:idx], s.Config.Servers[idx+1:]...)
			break
		}
	}
	return s.stopServer(ctx, server)
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
	s.Lock()
	defer s.Unlock()
	err := s.addIncomingStream(ctx, streamID)
	if err != nil {
		return err
	}
	return nil
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

type IncomingStream struct {
	StreamID types.StreamID

	NumBytesWrote uint64
	NumBytesRead  uint64
}

func (s *StreamServer) ListIncomingStreams(
	ctx context.Context,
) []IncomingStream {
	ctx = belt.WithField(ctx, "module", "StreamServer")
	_ = ctx
	s.Lock()
	defer s.Unlock()
	return s.listIncomingStreams(ctx)
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
	s.Lock()
	defer s.Unlock()
	return s.removeIncomingStream(ctx, streamID)
}

func (s *StreamServer) removeIncomingStream(
	_ context.Context,
	streamID types.StreamID,
) error {
	delete(s.Config.Streams, streamID)
	return nil
}

type StreamForward struct {
	StreamID         types.StreamID
	DestinationID    types.DestinationID
	Enabled          bool
	Quirks           types.ForwardingQuirks
	ActiveForwarding *ActiveStreamForwarding
	NumBytesWrote    uint64
	NumBytesRead     uint64
}

func (s *StreamServer) AddStreamForward(
	ctx context.Context,
	streamID types.StreamID,
	destinationID types.DestinationID,
	enabled bool,
	quirks types.ForwardingQuirks,
) (*StreamForward, error) {
	ctx = belt.WithField(ctx, "module", "StreamServer")
	s.Lock()
	defer s.Unlock()
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

	fwd, err := NewActiveStreamForward(
		ctx,
		s,
		streamID,
		destinationID,
		urlParsed.String(),
		func(ctx context.Context, fwd *ActiveStreamForwarding) {
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
	s.Lock()
	defer s.Unlock()
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
}

func (s *StreamServer) ListStreamForwards(
	ctx context.Context,
) (_ret []StreamForward, _err error) {
	ctx = belt.WithField(ctx, "module", "StreamServer")
	defer func() {
		logger.Tracef(ctx, "/ListStreamForwards(): %#+v %v", _ret, _err)
	}()
	s.Lock()
	defer s.Unlock()

	return s.getStreamForwards(ctx, func(si types.StreamID, di ordered.Optional[types.DestinationID]) bool {
		return true
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
	s.Lock()
	defer s.Unlock()
	streamCfg := s.Config.Streams[streamID]
	if _, ok := streamCfg.Forwardings[dstID]; !ok {
		return fmt.Errorf("the forwarding %s->%s does not exist", streamID, dstID)
	}
	delete(streamCfg.Forwardings, dstID)
	return s.removeStreamForward(ctx, streamID, dstID)
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
	s.Lock()
	defer s.Unlock()

	return s.getStreamForwards(ctx, func(streamID types.StreamID, dstID ordered.Optional[types.DestinationID]) bool {
		return !dstID.IsSet() || dstID.Get() == destID
	})
}

func (s *StreamServer) ListStreamDestinations(
	ctx context.Context,
) ([]types.StreamDestination, error) {
	ctx = belt.WithField(ctx, "module", "StreamServer")
	s.Mutex.Lock()
	defer s.Mutex.Unlock()
	return s.listStreamDestinations(ctx)
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
	s.Mutex.Lock()
	defer s.Mutex.Unlock()
	err := s.addStreamDestination(ctx, destinationID, url)
	if err != nil {
		return err
	}
	s.Config.Destinations[destinationID] = &types.DestinationConfig{URL: url}
	return nil
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
	s.Mutex.Lock()
	defer s.Mutex.Unlock()
	for _, streamCfg := range s.Config.Streams {
		delete(streamCfg.Forwardings, destinationID)
	}
	delete(s.Config.Destinations, destinationID)
	return s.removeStreamDestination(ctx, destinationID)
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
