// This implementation does not stream more than 280 minutes, because of
// some bug with implementing the extended timestamp of RTMP.

package streamserver

import (
	"context"
	"fmt"
	"io"
	"net"

	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/observability"
	"github.com/xaionaro-go/streamctl/pkg/player"
	playertypes "github.com/xaionaro-go/streamctl/pkg/player/types"
	"github.com/xaionaro-go/streamctl/pkg/streamplayer"
	yutoppgortmp "github.com/xaionaro-go/streamctl/pkg/streamserver/implementations/yutopp-go-rtmp"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/implementations/yutopp-go-rtmp/streamforward"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/streamplayers"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/types"
	"github.com/xaionaro-go/streamctl/pkg/streamtypes"
	"github.com/xaionaro-go/streamctl/pkg/xlogger"
	"github.com/xaionaro-go/streamctl/pkg/xsync"
	flvtag "github.com/yutopp/go-flv/tag"
	"github.com/yutopp/go-rtmp"
)

type BrowserOpener interface {
	OpenURL(ctx context.Context, url string) error
}

type StreamServer struct {
	Mutex xsync.Gorex
	*streamplayers.StreamPlayers
	*streamforward.StreamForwards
	Config         *types.Config
	RelayService   *yutoppgortmp.RelayService
	ServerHandlers []types.PortServer
}

var _ streamforward.StreamServer = (*StreamServer)(nil)

func New(
	cfg *types.Config,
	platformsController types.PlatformsController,
) *StreamServer {
	s := &StreamServer{
		RelayService: yutoppgortmp.NewRelayService(),
		Config:       cfg,
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
	return xsync.DoR1(ctx, &s.Mutex, func() error {
		return s.init(ctx, opts...)
	})
}

func (s *StreamServer) init(
	ctx context.Context,
	_ ...types.InitOption,
) (_err error) {
	cfg := s.Config
	logger.Debugf(ctx, "config == %#+v", *cfg)

	for _, srv := range cfg.Servers {
		{
			srv := srv
			observability.Go(ctx, func() {
				_, err := s.startServer(ctx, srv.Type, srv.Listen)
				if err != nil {
					logger.Errorf(ctx, "unable to initialize %s server at %s: %w", srv.Type, srv.Listen, err)
				}
			})
		}
	}

	for streamID := range cfg.Streams {
		err := s.addIncomingStream(ctx, streamID)
		if err != nil {
			return fmt.Errorf("unable to initialize stream '%s': %w", streamID, err)
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

func (s *StreamServer) WithConfig(
	ctx context.Context,
	callback func(context.Context, *types.Config),
) {
	s.Mutex.Do(ctx, func() {
		callback(ctx, s.Config)
	})
}

func (s *StreamServer) WaitPubsub(
	ctx context.Context,
	appKey types.AppKey,
) streamforward.Pubsub {
	return &pubsubAdapter{s.RelayService.WaitPubsub(ctx, appKey)}
}

type pubsubAdapter struct {
	*yutoppgortmp.Pubsub
}

var _ streamforward.Pubsub = (*pubsubAdapter)(nil)

func (pubsub *pubsubAdapter) Sub(
	conn io.Closer,
	callback func(ctx context.Context, flv *flvtag.FlvTag) error,
) streamforward.Sub {
	return pubsub.Pubsub.Sub(conn, callback)
}

func (s *StreamServer) PubsubNames() (types.AppKeys, error) {
	return s.RelayService.PubsubNames(), nil
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
	opts ...types.ServerOption,
) (types.PortServer, error) {
	ctx = belt.WithField(ctx, "module", "StreamServer")
	return xsync.DoR2(ctx, &s.Mutex, func() (types.PortServer, error) {
		srv, err := s.startServer(ctx, serverType, listenAddr, opts...)
		if err != nil {
			return nil, err
		}
		s.Config.Servers = append(s.Config.Servers, types.Server{
			Type:   serverType,
			Listen: listenAddr,
		})
		return srv, nil
	})
}

func (s *StreamServer) startServer(
	ctx context.Context,
	serverType streamtypes.ServerType,
	listenAddr string,
	_ ...types.ServerOption,
) (_ types.PortServer, _ret error) {
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
		portSrv := &yutoppgortmp.PortServer{
			Listener: listener,
		}
		portSrv.Server = rtmp.NewServer(&rtmp.ServerConfig{
			OnConnect: func(conn net.Conn) (io.ReadWriteCloser, *rtmp.ConnConfig) {
				ctx := belt.WithField(ctx, "client", conn.RemoteAddr().String())
				h := yutoppgortmp.NewHandler(s.RelayService)
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
		return nil, fmt.Errorf("RTSP is not supported, yet")
	default:
		return nil, fmt.Errorf("unexpected server type %v", serverType)
	}
	if err != nil {
		return nil, err
	}

	s.ServerHandlers = append(s.ServerHandlers, srv)
	return srv, nil
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

func (s *StreamServer) WaitPublisherChan(
	ctx context.Context,
	streamID types.StreamID,
) (<-chan types.Publisher, error) {

	ch := make(chan types.Publisher, 1)
	observability.Go(ctx, func() {
		ch <- s.RelayService.WaitPubsub(ctx, types.StreamID2LocalAppName(streamID))
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
