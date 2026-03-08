package streamserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"reflect"

	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/gwuhaolin/livego/configure"
	"github.com/gwuhaolin/livego/protocol/rtmp"
	"github.com/spf13/viper"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/player/pkg/player"
	"github.com/xaionaro-go/streamctl/pkg/streamplayer"
	sptypes "github.com/xaionaro-go/streamctl/pkg/streamplayer/types"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/types"
	"github.com/xaionaro-go/streamctl/pkg/streamtypes"
	"github.com/xaionaro-go/xsync"
)

type StreamServer struct {
	xsync.Mutex
	Config                  *types.Config
	ServerHandlers          []types.PortServer
	StreamIDs               map[types.StreamSourceID]struct{}
	StreamSinks             []types.StreamSink
	ActiveStreamForwardings map[types.StreamSinkID]*ActiveStreamForwarding
}

func New(
	cfg *types.Config,
	platformsController types.PlatformsController,
	browserOpener types.BrowserOpener,
) *StreamServer {
	return &StreamServer{
		Config:    cfg,
		StreamIDs: map[types.StreamSourceID]struct{}{},

		ActiveStreamForwardings: map[types.StreamSinkID]*ActiveStreamForwarding{},
	}
}

func (s *StreamServer) Init(
	ctx context.Context,
	opts ...types.InitOption,
) error {
	return xsync.DoA1R1(ctx, &s.Mutex, s.init, ctx)
}

func (s *StreamServer) init(ctx context.Context) error {
	cfg := s.Config
	logger.Debugf(ctx, "config == %#+v", *cfg)

	for _, srv := range cfg.Servers {
		err := s.startServer(ctx, srv.Type, srv.Listen)
		if err != nil {
			return fmt.Errorf("unable to initialize %s server at %s: %w", srv.Type, srv.Listen, err)
		}
	}

	for dstID, dstCfg := range cfg.Destinations {
		err := s.addStreamSink(ctx, dstID, dstCfg.URL)
		if err != nil {
			return fmt.Errorf(
				"unable to initialize stream destination '%s' to %#+v: %w",
				dstID,
				dstCfg,
				err,
			)
		}
	}

	for streamSourceID, streamCfg := range cfg.Streams {
		err := s.addStreamSource(ctx, streamSourceID)
		if err != nil {
			return fmt.Errorf("unable to initialize stream '%s': %w", streamSourceID, err)
		}

		for dstID, fwd := range streamCfg.Forwardings {
			if !fwd.Disabled {
				_, err := s.addStreamForward(ctx, streamSourceID, dstID, fwd.Quirks)
				if err != nil {
					return fmt.Errorf(
						"unable to launch stream forward from '%s' to '%s': %w",
						streamSourceID,
						dstID,
						err,
					)
				}
			}
		}
	}

	return nil
}

func (s *StreamServer) ListServers(
	ctx context.Context,
) (_ret []types.PortServer) {
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
		portServer := &PortServer{
			Stream:   rtmp.NewRtmpStream(),
			Listener: listener,
		}
		portServer.Server = rtmp.NewRtmpServer(portServer.Stream, nil)
		observability.Go(ctx, func() {
			err = portServer.Server.Serve(listener)
			if err != nil {
				err = fmt.Errorf(
					"unable to start serving RTMP at '%s': %w",
					listener.Addr().String(),
					err,
				)
				logger.Error(ctx, err)
			}
		})
		srv = portServer
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

func (s *StreamServer) AddStreamSource(
	ctx context.Context,
	streamSourceID types.StreamSourceID,
) error {
	return xsync.DoR1(ctx, &s.Mutex, func() error {
		err := s.addStreamSource(ctx, streamSourceID)
		if err != nil {
			return err
		}
		s.Config.Streams[streamSourceID] = &types.StreamConfig{}
		return nil
	})
}

func assertEqual(a, b any) {
	if !reflect.DeepEqual(a, b) {
		panic(fmt.Errorf("%#+v and %#+v are supposed to be equal", a, b))
	}
}

func (s *StreamServer) addStreamSource(
	ctx context.Context,
	streamSourceID types.StreamSourceID,
) error {
	if _, ok := s.StreamIDs[streamSourceID]; ok {
		return fmt.Errorf("stream '%s' already exists", streamSourceID)
	}

	s.StreamIDs[streamSourceID] = struct{}{}

	if err := s.updateNewStreamIDs(ctx); err != nil {
		return fmt.Errorf("unable to update the livego config: %w", err)
	}
	return nil
}

func (s *StreamServer) updateNewStreamIDs(
	ctx context.Context,
) error {
	apps := make(configure.Applications, 0, len(s.StreamIDs))
	for streamSourceID := range s.StreamIDs {
		apps = append(apps, configure.Application{
			Appname: string(streamSourceID),
			Live:    true,
		})
	}
	configure.RoomKeys.SetKey("nopass")
	logger.Debugf(ctx, "new apps == %#+v", apps)
	b, err := json.Marshal(configure.ServerCfg{
		RTMPNoAuth: true,
		Server:     apps,
	})
	if err != nil {
		panic(err)
	}
	defaultConfig := bytes.NewReader(b)
	viper.SetConfigType("json")
	err = viper.ReadConfig(defaultConfig)
	if err != nil {
		panic(err)
	}
	configure.Config.MergeConfigMap(viper.AllSettings())

	var recheckApps configure.Applications
	configure.Config.UnmarshalKey("server", &recheckApps)
	assertEqual(apps, recheckApps)
	return nil
}

func (s *StreamServer) ListStreamSources(
	ctx context.Context,
) []types.StreamSource {
	return xsync.DoA1R1(ctx, &s.Mutex, s.listStreamSources, ctx)
}

func (s *StreamServer) listStreamSources(
	_ context.Context,
) []types.StreamSource {
	var result []types.StreamSource
	for streamSourceID := range s.StreamIDs {
		result = append(
			result,
			types.StreamSource{
				StreamSourceID: streamSourceID,
				NumBytesWrote:  0, // TODO: fill the value
				NumBytesRead:   0, // TODO: fill the value
			},
		)
	}
	return result
}

func (s *StreamServer) RemoveStreamSource(
	ctx context.Context,
	streamSourceID types.StreamSourceID,
) error {
	return xsync.DoR1(ctx, &s.Mutex, func() error {
		delete(s.Config.Streams, streamSourceID)
		return s.removeStreamSource(ctx, streamSourceID)
	})
}

func (s *StreamServer) removeStreamSource(
	ctx context.Context,
	streamSourceID types.StreamSourceID,
) error {
	if _, ok := s.StreamIDs[streamSourceID]; !ok {
		return fmt.Errorf("stream '%s' does not exist", streamSourceID)
	}
	delete(s.StreamIDs, streamSourceID)

	if err := s.updateNewStreamIDs(ctx); err != nil {
		return fmt.Errorf("unable to update the livego config: %w", err)
	}

	return nil
}

type StreamForward = types.StreamForward[*ActiveStreamForwarding]

func (s *StreamServer) AddStreamForward(
	ctx context.Context,
	streamSourceID types.StreamSourceID,
	streamSinkID types.StreamSinkID,
	enabled bool,
	quirks types.ForwardingQuirks,
) (*types.StreamForward[*ActiveStreamForwarding], error) {
	return xsync.DoR2(ctx, &s.Mutex, func() (*types.StreamForward[*ActiveStreamForwarding], error) {
		streamConfig := s.Config.Streams[streamSourceID]
		if streamConfig.Forwardings == nil {
			streamConfig.Forwardings = map[types.StreamSinkID]types.ForwardingConfig{}
		}

		if _, ok := streamConfig.Forwardings[streamSinkID]; ok {
			return nil, fmt.Errorf("the forwarding %s->%s already exists", streamSourceID, streamSinkID)
		}

		cfg := types.ForwardingConfig{
			Disabled: !enabled,
			Quirks:   quirks,
		}

		var fwd *types.StreamForward[*ActiveStreamForwarding]
		if enabled {
			var err error
			fwd, err = s.addStreamForward(ctx, streamSourceID, streamSinkID, quirks)
			if err != nil {
				return fwd, err
			}
		} else {
			fwd = buildStreamForward(streamSourceID, streamSinkID, cfg, nil)
		}
		streamConfig.Forwardings[streamSinkID] = cfg
		return fwd, nil
	})
}

func (s *StreamServer) addStreamForward(
	ctx context.Context,
	streamSourceID types.StreamSourceID,
	streamSinkID types.StreamSinkID,
	quirks types.ForwardingQuirks,
) (*types.StreamForward[*ActiveStreamForwarding], error) {
	cfg := types.ForwardingConfig{
		Disabled: true,
		Quirks:   quirks,
	}

	ctx = belt.WithField(ctx, "stream_forward", fmt.Sprintf("%s->%s", streamSourceID, streamSinkID))
	if actFwd, ok := s.ActiveStreamForwardings[streamSinkID]; ok {
		return buildStreamForward(
				streamSourceID,
				streamSinkID,
				cfg,
				actFwd,
			), fmt.Errorf(
				"there is already an active stream forwarding to '%s'",
				streamSinkID,
			)
	}

	dst, err := s.findStreamSinkByID(ctx, streamSinkID)
	if err != nil {
		return nil, fmt.Errorf("unable to find stream destination '%s': %w", streamSinkID, err)
	}

	if len(s.ServerHandlers) == 0 {
		return nil, fmt.Errorf("no open ports")
	}
	h := s.ServerHandlers[0]

	urlSrc := "rtmp://" + h.ListenAddr() + "/" + string(streamSourceID)
	actFwd, err := newActiveStreamForward(ctx, streamSourceID, streamSinkID, urlSrc, dst.URL)
	if err != nil {
		return nil, fmt.Errorf("unable to run the stream forwarding: %w", err)
	}
	s.ActiveStreamForwardings[streamSinkID] = actFwd

	return buildStreamForward(streamSourceID, streamSinkID, cfg, actFwd), nil
}

func buildStreamForward(
	streamSourceID types.StreamSourceID,
	streamSinkID types.StreamSinkID,
	cfg types.ForwardingConfig,
	actFwd *ActiveStreamForwarding,
) *types.StreamForward[*ActiveStreamForwarding] {
	return &types.StreamForward[*ActiveStreamForwarding]{
		StreamSourceID:   streamSourceID,
		StreamSinkID:     streamSinkID,
		Enabled:          !cfg.Disabled,
		Quirks:           cfg.Quirks,
		ActiveForwarding: actFwd,
		NumBytesWrote:    0, // TODO: fill this value
		NumBytesRead:     0, // TODO: fill this value
	}
}

func (a *StreamServer) ActiveStreamSourceIDs() []types.StreamSourceID {
}

func (a *StreamServer) WaitPublisherChan(
	ctx context.Context,
	streamSourceID types.StreamSourceID,
) (<-chan types.Publisher, error) {
}

func (s *StreamServer) UpdateStreamForward(
	ctx context.Context,
	streamSourceID types.StreamSourceID,
	streamSinkID types.StreamSinkID,
	enabled bool,
	quirks types.ForwardingQuirks,
) (*types.StreamForward[*ActiveStreamForwarding], error) {
	return xsync.DoR1(ctx, &s.Mutex, func() error {
		streamConfig := s.Config.Streams[streamSourceID]
		fwdCfg, ok := streamConfig.Forwardings[streamSinkID]
		if !ok {
			return fmt.Errorf("the forwarding %s->%s does not exist", streamSourceID, streamSinkID)
		}

		if fwdCfg.Disabled && enabled {
			err := s.addStreamForward(ctx, streamSourceID, streamSinkID)
			if err != nil {
				return err
			}
		}
		if !fwdCfg.Disabled && !enabled {
			err := s.removeStreamForward(ctx, streamSourceID, streamSinkID)
			if err != nil {
				return err
			}
		}
		streamConfig.Forwardings[streamSinkID] = types.ForwardingConfig{
			Disabled: !enabled,
		}
		return nil
	})
}

func (s *StreamServer) ListStreamForwards(
	ctx context.Context,
) ([]StreamForward, error) {
	return xsync.DoR2(ctx, &s.Mutex, func() ([]StreamForward, error) {
		activeStreamForwards, err := s.listStreamForwards(ctx)
		if err != nil {
			return nil, fmt.Errorf("unable to get the list of active stream forwardings: %w", err)
		}

		type fwdID struct {
			StreamSourceID types.StreamSourceID
			DestID         types.StreamSinkID
		}
		m := map[fwdID]*StreamForward{}
		for idx := range activeStreamForwards {
			fwd := &activeStreamForwards[idx]
			m[fwdID{
				StreamSourceID: fwd.StreamSourceID,
				DestID:         fwd.StreamSinkID,
			}] = fwd
		}

		var result []StreamForward
		for streamSourceID, stream := range s.Config.Streams {
			for dstID, cfg := range stream.Forwardings {
				item := StreamForward{
					StreamSourceID: streamSourceID,
					StreamSinkID:   dstID,
					Enabled:        !cfg.Disabled,
				}
				if activeFwd, ok := m[fwdID{
					StreamSourceID: streamSourceID,
					DestID:         dstID,
				}]; ok {
					item.NumBytesWrote = activeFwd.NumBytesWrote
					item.NumBytesRead = activeFwd.NumBytesRead
				}
				logger.Tracef(ctx, "stream forwarding '%s->%s': %#+v", streamSourceID, dstID, cfg)
				result = append(result, item)
			}
		}
		return result, nil
	})
}

func (s *StreamServer) listStreamForwards(
	_ context.Context,
) ([]StreamForward, error) {
	var result []StreamForward
	for _, fwd := range s.ActiveStreamForwardings {
		result = append(result, StreamForward{
			StreamSourceID: fwd.StreamSourceID,
			StreamSinkID:   fwd.StreamSinkID,
			Enabled:        true,
			NumBytesWrote:  0,
			NumBytesRead:   0,
		})
	}
	return result, nil
}

func (s *StreamServer) RemoveStreamForward(
	ctx context.Context,
	streamSourceID types.StreamSourceID,
	dstID types.StreamSinkID,
) error {
	return xsync.DoR1(ctx, &s.Mutex, func() error {
		streamCfg := s.Config.Streams[streamSourceID]
		if _, ok := streamCfg.Forwardings[dstID]; !ok {
			return fmt.Errorf("the forwarding %s->%s does not exist", streamSourceID, dstID)
		}
		delete(streamCfg.Forwardings, dstID)
		return s.removeStreamForward(ctx, streamSourceID, dstID)
	})
}

func (s *StreamServer) removeStreamForward(
	_ context.Context,
	_ types.StreamSourceID,
	dstID types.StreamSinkID,
) error {
	fwd := s.ActiveStreamForwardings[dstID]
	if fwd == nil {
		return nil
	}
	delete(s.ActiveStreamForwardings, dstID)
	err := fwd.Close()
	if err != nil {
		return fmt.Errorf("unable to close stream forwarding to '%s': %w", dstID, err)
	}
	return nil
}

func (s *StreamServer) ListStreamSinks(
	ctx context.Context,
) ([]types.StreamSink, error) {
	return xsync.DoA1R2(ctx, &s.Mutex, s.listStreamSinks, ctx)
}

func (s *StreamServer) listStreamSinks(
	_ context.Context,
) ([]types.StreamSink, error) {
	c := make([]types.StreamSink, len(s.StreamSinks))
	copy(c, s.StreamSinks)
	return c, nil
}

func (s *StreamServer) AddStreamSink(
	ctx context.Context,
	streamSinkID types.StreamSinkID,
	url string,
) error {
	return xsync.DoR1(ctx, &s.Mutex, func() error {
		err := s.addStreamSink(ctx, streamSinkID, url)
		if err != nil {
			return err
		}
		s.Config.Destinations[streamSinkID] = &types.StreamSinkConfig{URL: url}
		return nil
	})
}

func (s *StreamServer) addStreamSink(
	_ context.Context,
	streamSinkID types.StreamSinkID,
	url string,
) error {
	s.StreamSinks = append(s.StreamSinks, types.StreamSink{
		ID:  streamSinkID,
		URL: url,
	})
	return nil
}

func (s *StreamServer) RemoveStreamSink(
	ctx context.Context,
	streamSinkID types.StreamSinkID,
) error {
	return xsync.DoR1(ctx, &s.Mutex, func() error {
		for _, streamCfg := range s.Config.Streams {
			delete(streamCfg.Forwardings, streamSinkID)
		}
		delete(s.Config.Destinations, streamSinkID)
		return s.removeStreamSink(ctx, streamSinkID)
	})
}

func (s *StreamServer) removeStreamSink(
	ctx context.Context,
	streamSinkID types.StreamSinkID,
) error {
	streamForwards, err := s.listStreamForwards(ctx)
	if err != nil {
		return fmt.Errorf("unable to list stream forwardings: %w", err)
	}
	for _, fwd := range streamForwards {
		if fwd.StreamSinkID == streamSinkID {
			s.removeStreamForward(ctx, fwd.StreamSourceID, fwd.StreamSinkID)
		}
	}

	for i := range s.StreamSinks {
		if s.StreamSinks[i].ID == streamSinkID {
			s.StreamSinks = append(s.StreamSinks[:i], s.StreamSinks[i+1:]...)
			return nil
		}
	}

	return fmt.Errorf("have not found stream destination with id %s", streamSinkID)
}

func (s *StreamServer) findStreamSinkByID(
	_ context.Context,
	streamSinkID types.StreamSinkID,
) (types.StreamSink, error) {
	for _, dst := range s.StreamSinks {
		if dst.ID == streamSinkID {
			return dst, nil
		}
	}
	return types.StreamSink{}, fmt.Errorf(
		"unable to find a stream destination by StreamSourceID '%s'",
		streamSinkID,
	)
}

func (s *StreamServer) AddStreamPlayer(
	ctx context.Context,
	streamSourceID types.StreamSourceID,
	playerType player.Backend,
	disabled bool,
	streamPlaybackConfig sptypes.Config,
	opts ...types.StreamPlayerOption,
) error {

}

func (s *StreamServer) UpdateStreamPlayer(
	ctx context.Context,
	streamSourceID types.StreamSourceID,
	playerType player.Backend,
	disabled bool,
	streamPlaybackConfig sptypes.Config,
	opts ...types.StreamPlayerOption,
) error {

}

func (s *StreamServer) RemoveStreamPlayer(
	ctx context.Context,
	streamSourceID types.StreamSourceID,
) error {

}

func (s *StreamServer) ListStreamPlayers(
	ctx context.Context,
) ([]types.StreamPlayer, error) {

}

func (s *StreamServer) GetStreamPlayer(
	ctx context.Context,
	streamSourceID types.StreamSourceID,
) (*types.StreamPlayer, error) {

}

func (s *StreamServer) GetActiveStreamPlayer(
	ctx context.Context,
	streamSourceID types.StreamSourceID,
) (player.Player, error) {

}

func (s *StreamServer) GetPortServers(
	ctx context.Context,
) ([]streamplayer.StreamPortServer, error) {

}
