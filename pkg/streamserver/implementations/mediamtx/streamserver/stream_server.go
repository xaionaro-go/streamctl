// This implementation does not stream more than 280 minutes, because of
// some bug with implementing the extended timestamp of RTMP.

package streamserver

import (
	"context"
	"fmt"
	"time"

	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/mediamtx/pkg/conf"
	"github.com/xaionaro-go/mediamtx/pkg/defs"
	"github.com/xaionaro-go/mediamtx/pkg/externalcmd"
	"github.com/xaionaro-go/mediamtx/pkg/pathmanager"
	"github.com/xaionaro-go/streamctl/pkg/observability"
	"github.com/xaionaro-go/streamctl/pkg/player"
	playertypes "github.com/xaionaro-go/streamctl/pkg/player/types"
	"github.com/xaionaro-go/streamctl/pkg/streamplayer"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/implementations/libav/streamforward"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/streamplayers"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/types"
	"github.com/xaionaro-go/streamctl/pkg/streamtypes"
	"github.com/xaionaro-go/streamctl/pkg/xsync"
)

type BrowserOpener interface {
	OpenURL(ctx context.Context, url string) error
}

type StreamServer struct {
	*streamplayers.StreamPlayers
	*streamforward.StreamForwards

	mutex          xsync.Gorex
	config         *types.Config
	pathManager    *pathmanager.PathManager
	serverHandlers []types.PortServer
	isInitialized  bool

	streamsStatusLocker xsync.Mutex
	publishers          map[types.AppKey]*PublisherClosedNotifier
	streamsChanged      chan struct{}
}

var _ streamforward.StreamServer = (*StreamServer)(nil)

func New(
	cfg *types.Config,
	platformsController types.PlatformsController,
) *StreamServer {

	s := &StreamServer{
		config:         cfg,
		publishers:     make(map[types.AppKey]*PublisherClosedNotifier),
		streamsChanged: make(chan struct{}),
	}
	s.StreamForwards = streamforward.NewStreamForwards(s, platformsController)
	s.StreamPlayers = streamplayers.NewStreamPlayers(
		streamplayer.New(
			s,
			player.NewManager(playertypes.OptionPathToMPV(cfg.VideoPlayer.MPV.Path)),
		),
		s,
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
	return xsync.DoR1(ctx, &s.mutex, func() error {
		return s.init(ctx, opts...)
	})
}

func (s *StreamServer) init(
	ctx context.Context,
	_ ...types.InitOption,
) (_err error) {
	if s.isInitialized {
		return fmt.Errorf("already initialized")
	}
	s.isInitialized = true

	cfg := s.config
	logger.Debugf(ctx, "config == %#+v", *cfg)

	s.pathManager = pathmanager.New(
		toConfLoggerLevel(logger.Default().Level()),
		&dummyAuthManager{},
		"",
		conf.StringDuration(10*time.Second),
		conf.StringDuration(10*time.Second),
		512, // a power of 2, that is close to 10 seconds * 60 FPS
		1472,
		make(map[string]*conf.Path),
		externalcmd.NewPool(),
		newMediamtxLogger(logger.FromCtx(ctx)),
	)
	s.pathManager.Initialize(ctx)
	s.pathManager.SetHLSServer(s)

	s.reloadPathConfs(ctx)

	for _, srv := range cfg.Servers {
		{
			srv := srv
			observability.Go(ctx, func() {
				s.mutex.Do(ctx, func() {
					_, err := s.startServer(ctx, srv.Type, srv.Listen)
					if err != nil {
						logger.Errorf(ctx, "unable to initialize %s server at %s: %w", srv.Type, srv.Listen, err)
					}
				})
			})
		}
	}

	if err := s.StreamForwards.Init(ctx); err != nil {
		return fmt.Errorf("unable to initialize stream forwardings: %w", err)
	}

	if err := s.StreamPlayers.Init(ctx); err != nil {
		return fmt.Errorf("unable to initialize stream players: %w", err)
	}

	return nil
}

func (s *StreamServer) PathReady(path defs.Path) {
	ctx := context.TODO()
	logger.Debugf(ctx, "PathReady(%s)", path.Name())
	defer logger.Debugf(ctx, "/PathReady(%s)", path.Name())

	appKey := types.AppKey(path.Name())

	s.streamsStatusLocker.Do(context.Background(), func() {
		s.publishers[appKey] = newPublisherClosedNotifier()

		var oldCh chan struct{}
		oldCh, s.streamsChanged = s.streamsChanged, make(chan struct{})
		close(oldCh)
	})
}

func (s *StreamServer) PathNotReady(path defs.Path) {
	ctx := context.TODO()
	logger.Debugf(ctx, "PathNotReady(%s)", path.Name())
	defer logger.Debugf(ctx, "/PathNotReady(%s)", path.Name())

	appKey := types.AppKey(path.Name())

	s.streamsStatusLocker.Do(context.Background(), func() {
		publisher := s.publishers[appKey]
		// TODO: add an assert
		if publisher != nil {
			publisher.Close()
			delete(s.publishers, appKey)
		}

		var oldCh chan struct{}
		oldCh, s.streamsChanged = s.streamsChanged, make(chan struct{})
		close(oldCh)
	})
}

func (s *StreamServer) WithConfig(
	ctx context.Context,
	callback func(context.Context, *types.Config),
) {
	s.mutex.Do(ctx, func() {
		callback(ctx, s.config)
	})
}

func (s *StreamServer) PubsubNames() (types.AppKeys, error) {
	pathList, err := s.pathManager.APIPathsList()
	if err != nil {
		return nil, fmt.Errorf("unable to query the list of available pubsub names: %w", err)
	}

	var result types.AppKeys
	for _, item := range pathList.Items {
		if item.Ready {
			result = append(result, types.AppKey(item.Name))
		}
	}

	return result, nil
}

func (s *StreamServer) ListServers(
	ctx context.Context,
) (_ret []types.PortServer) {
	ctx = belt.WithField(ctx, "module", "StreamServer")
	logger.Tracef(ctx, "ListServers")
	defer func() { logger.Tracef(ctx, "/ListServers: %d servers", len(_ret)) }()

	return xsync.DoR1(ctx, &s.mutex, func() []types.PortServer {
		c := make([]types.PortServer, len(s.serverHandlers))
		copy(c, s.serverHandlers)
		return c
	})
}

func (s *StreamServer) StartServer(
	ctx context.Context,
	serverType streamtypes.ServerType,
	listenAddr string,
	opts ...types.ServerOption,
) (types.PortServer, error) {
	ctx = belt.WithField(ctx, "module", "StreamServer")
	return xsync.DoR2(ctx, &s.mutex, func() (types.PortServer, error) {
		portSrv, err := s.startServer(ctx, serverType, listenAddr, opts...)
		if err != nil {
			return nil, err
		}
		s.config.Servers = append(s.config.Servers, types.Server{
			Type:   serverType,
			Listen: listenAddr,
		})
		return portSrv, nil
	})
}

func (s *StreamServer) findServer(
	_ context.Context,
	server types.PortServer,
) (int, error) {
	for i := range s.serverHandlers {
		if s.serverHandlers[i] == server {
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
	return xsync.DoR1(ctx, &s.mutex, func() error {
		for idx, srv := range s.config.Servers {
			if srv.Listen == server.ListenAddr() {
				s.config.Servers = append(s.config.Servers[:idx], s.config.Servers[idx+1:]...)
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

	s.serverHandlers = append(s.serverHandlers[:idx], s.serverHandlers[idx+1:]...)
	return server.Close()
}

func (s *StreamServer) AddIncomingStream(
	ctx context.Context,
	streamID types.StreamID,
) error {
	ctx = belt.WithField(ctx, "module", "StreamServer")
	return xsync.DoR1(ctx, &s.mutex, func() error {
		err := s.addIncomingStream(ctx, streamID)
		if err != nil {
			return err
		}
		return nil
	})
}

func (s *StreamServer) addIncomingStream(
	ctx context.Context,
	streamID types.StreamID,
) error {
	if _, ok := s.config.Streams[streamID]; ok {
		return nil
	}
	s.config.Streams[streamID] = &types.StreamConfig{}
	s.reloadPathConfs(ctx)
	return nil
}

func (s *StreamServer) reloadPathConfs(
	ctx context.Context,
) {
	// TODO: fix race condition with a client connecting to a server
	pathConfs := make(map[string]*conf.Path, len(s.config.Streams))

	for streamID := range s.config.Streams {
		pathConfs[string(streamID)] = &conf.Path{
			Name:   string(types.StreamID2LocalAppName(streamID)),
			Source: "publisher",
		}
	}

	logger.Debugf(ctx, "new pathConfs is %#+v", pathConfs)

	s.pathManager.ReloadPathConfs(pathConfs)
}

type IncomingStream = types.IncomingStream

func (s *StreamServer) ListIncomingStreams(
	ctx context.Context,
) []IncomingStream {
	ctx = belt.WithField(ctx, "module", "StreamServer")
	_ = ctx
	return xsync.DoA1R1(ctx, &s.mutex, s.listIncomingStreams, ctx)
}

func (s *StreamServer) listIncomingStreams(
	_ context.Context,
) []IncomingStream {
	var result []IncomingStream
	for streamID := range s.config.Streams {
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
	return xsync.DoA2R1(ctx, &s.mutex, s.removeIncomingStream, ctx, streamID)
}

func (s *StreamServer) removeIncomingStream(
	ctx context.Context,
	streamID types.StreamID,
) error {
	delete(s.config.Streams, streamID)
	s.reloadPathConfs(ctx)
	return nil
}

func (s *StreamServer) WaitPublisherChan(
	ctx context.Context,
	streamID types.StreamID,
) (<-chan types.Publisher, error) {
	appKey := types.AppKey(streamID)

	ch := make(chan types.Publisher, 1)
	observability.Go(ctx, func() {
		for {
			publisher, waitCh := xsync.DoR2(ctx, &s.mutex, func() (*PublisherClosedNotifier, chan struct{}) {
				return s.publishers[appKey], s.streamsChanged
			})

			logger.Debugf(ctx, "WaitPublisherChan(%s): publisher==%v", appKey, publisher)
			if publisher != nil {
				ch <- publisher
				close(ch)
				return
			}
			logger.Debugf(ctx, "WaitPublisherChan(%s): waiting...", appKey)
			select {
			case <-ctx.Done():
				logger.Debugf(ctx, "WaitPublisherChan(%s): cancelled", appKey)
				return
			case <-waitCh:
				logger.Debugf(ctx, "WaitPublisherChan(%s): an event happened, rechecking", appKey)
			}
		}
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

func (s *StreamServer) startServer(
	ctx context.Context,
	serverType streamtypes.ServerType,
	listenAddr string,
	opts ...types.ServerOption,
) (_ types.PortServer, _ret error) {
	logger.Tracef(ctx, "startServer(%s, '%s')", serverType, listenAddr)
	defer func() { logger.Tracef(ctx, "/startServer(%s, '%s'): %v", serverType, listenAddr, _ret) }()

	for _, portSrv := range s.serverHandlers {
		if portSrv.ListenAddr() == listenAddr {
			return nil, fmt.Errorf("we already have an port server %#+v instance at '%s'", portSrv, listenAddr)
		}
	}

	portSrv, err := s.newServer(ctx, serverType, listenAddr, opts...)
	if err != nil {
		return nil, fmt.Errorf("unable to initialize a new instance of a port server %s at %s with options %v: %w", serverType, listenAddr, opts, err)
	}

	s.serverHandlers = append(s.serverHandlers, portSrv)
	return nil, err
}

func (s *StreamServer) newServer(
	ctx context.Context,
	serverType streamtypes.ServerType,
	listenAddr string,
	opts ...types.ServerOption,
) (_ types.PortServer, _ret error) {
	switch serverType {
	case streamtypes.ServerTypeRTSP:
		return s.newServerRTSP(ctx, listenAddr, opts...)
	case streamtypes.ServerTypeSRT:
		return s.newServerSRT(ctx, listenAddr, opts...)
	case streamtypes.ServerTypeRTMP:
		return s.newServerRTMP(ctx, listenAddr, opts...)
	case streamtypes.ServerTypeHLS:
		return s.newServerHLS(ctx, listenAddr, opts...)
	case streamtypes.ServerTypeWebRTC:
		return s.newServerWebRTC(ctx, listenAddr, opts...)
	default:
		return nil, fmt.Errorf("unsupported server type '%s'", serverType)
	}
}

func (s *StreamServer) newServerRTMP(
	ctx context.Context,
	listenAddr string,
	opts ...types.ServerOption,
) (_ types.PortServer, _ret error) {
	rtmpSrv := newRTMPServer(
		s.pathManager,
		listenAddr,
		newMediamtxLogger(logger.FromCtx(ctx)),
		opts...,
	)
	if err := rtmpSrv.Initialize(); err != nil {
		return nil, fmt.Errorf("unable to initialize the RTMP server %#+v: %w", rtmpSrv, err)
	}
	return &portServerWrapperRTMP{Server: rtmpSrv}, nil
}
