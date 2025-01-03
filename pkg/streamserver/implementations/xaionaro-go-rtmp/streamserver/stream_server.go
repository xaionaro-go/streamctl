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
	"github.com/xaionaro-go/go-rtmp"
	"github.com/xaionaro-go/streamctl/pkg/observability"
	"github.com/xaionaro-go/streamctl/pkg/player"
	playertypes "github.com/xaionaro-go/streamctl/pkg/player/types"
	xaionarogortmp "github.com/xaionaro-go/streamctl/pkg/recoder/xaionaro-go-rtmp"
	"github.com/xaionaro-go/streamctl/pkg/streamplayer"
	yutoppgortmp "github.com/xaionaro-go/streamctl/pkg/streamserver/implementations/xaionaro-go-rtmp"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/implementations/xaionaro-go-rtmp/streamforward"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/streamplayers"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/types"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/types/streamportserver"
	"github.com/xaionaro-go/streamctl/pkg/streamtypes"
	"github.com/xaionaro-go/streamctl/pkg/xlogger"
	"github.com/xaionaro-go/streamctl/pkg/xsync"
	flvtag "github.com/yutopp/go-flv/tag"
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
	ServerHandlers []streamportserver.Server
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

	for _, srv := range cfg.PortServers {
		{
			srv := srv
			observability.Go(ctx, func() {
				_, err := s.startServer(ctx, srv.Type, srv.ListenAddr)
				if err != nil {
					logger.Errorf(
						ctx,
						"unable to initialize %s server at %s: %w",
						srv.Type,
						srv.ListenAddr,
						err,
					)
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
) xaionarogortmp.Pubsub {
	return &pubsubAdapter{s.RelayService.WaitPubsub(ctx, appKey, false)}
}

type pubsubAdapter struct {
	*yutoppgortmp.Pubsub
}

var _ xaionarogortmp.Pubsub = (*pubsubAdapter)(nil)

func (pubsub *pubsubAdapter) Sub(
	conn io.Closer,
	callback func(ctx context.Context, flv *flvtag.FlvTag) error,
) xaionarogortmp.Sub {
	return pubsub.Pubsub.Sub(conn, callback)
}

func (s *StreamServer) PubsubNames() (types.AppKeys, error) {
	return s.RelayService.PubsubNames(), nil
}

func (s *StreamServer) ListServers(
	ctx context.Context,
) (_ret []streamportserver.Server) {
	ctx = belt.WithField(ctx, "module", "StreamServer")
	logger.Tracef(ctx, "ListServers")
	defer func() { logger.Tracef(ctx, "/ListServers: %d servers", len(_ret)) }()

	return xsync.DoR1(ctx, &s.Mutex, func() []streamportserver.Server {
		c := make([]streamportserver.Server, len(s.ServerHandlers))
		copy(c, s.ServerHandlers)
		return c
	})
}

func (s *StreamServer) StartServer(
	ctx context.Context,
	serverType streamtypes.ServerType,
	listenAddr string,
	opts ...streamportserver.Option,
) (streamportserver.Server, error) {
	ctx = belt.WithField(ctx, "module", "StreamServer")
	return xsync.DoR2(ctx, &s.Mutex, func() (streamportserver.Server, error) {
		srv, err := s.startServer(ctx, serverType, listenAddr, opts...)
		if err != nil {
			return nil, err
		}
		s.Config.PortServers = append(s.Config.PortServers, streamportserver.Config{
			ProtocolSpecificConfig: srv.ProtocolSpecificConfig().Options().ProtocolSpecificConfig(ctx),
			Type:                   serverType,
			ListenAddr:             listenAddr,
		})
		return srv, nil
	})
}

func (s *StreamServer) startServer(
	ctx context.Context,
	serverType streamtypes.ServerType,
	listenAddr string,
	opts ...streamportserver.Option,
) (_ streamportserver.Server, _ret error) {
	logger.Tracef(ctx, "startServer(%s, '%s')", serverType, listenAddr)
	defer func() { logger.Tracef(ctx, "/startServer(%s, '%s'): %v", serverType, listenAddr, _ret) }()

	cfg := streamportserver.Options(opts).ProtocolSpecificConfig(ctx)
	if cfg.IsTLS {
		return nil, fmt.Errorf("this implementation of the stream server does not support TLS")
	}

	var srv streamportserver.Server
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
				wrcc := types.NewReaderWriterCloseCounter(
					conn,
					&portSrv.ReadCount,
					&portSrv.WriteCount,
				)
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
				err = fmt.Errorf(
					"unable to start serving RTMP at '%s': %w",
					listener.Addr().String(),
					err,
				)
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
	server streamportserver.Server,
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
	server streamportserver.Server,
) error {
	ctx = belt.WithField(ctx, "module", "StreamServer")
	return xsync.DoR1(ctx, &s.Mutex, func() error {
		for idx, srv := range s.Config.PortServers {
			if srv.ListenAddr == server.ListenAddr() {
				s.Config.PortServers = append(s.Config.PortServers[:idx], s.Config.PortServers[idx+1:]...)
				break
			}
		}
		return s.stopServer(ctx, server)
	})
}

func (s *StreamServer) stopServer(
	ctx context.Context,
	server streamportserver.Server,
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
	waitForNext bool,
) (<-chan types.Publisher, error) {

	ch := make(chan types.Publisher, 1)
	observability.Go(ctx, func() {
		ch <- s.RelayService.WaitPubsub(ctx, types.StreamID2LocalAppName(streamID), waitForNext)
		close(ch)
	})
	return ch, nil
}

func (s *StreamServer) GetPortServers(
	ctx context.Context,
) ([]streamportserver.Config, error) {
	srvs := s.ListServers(ctx)

	result := make([]streamportserver.Config, 0, len(srvs))
	for _, srv := range srvs {
		result = append(result, streamportserver.Config{
			ListenAddr:             srv.ListenAddr(),
			Type:                   srv.Type(),
			ProtocolSpecificConfig: srv.ProtocolSpecificConfig(),
		})
	}

	return result, nil
}
