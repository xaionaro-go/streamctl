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
	StreamIDs               map[types.StreamID]struct{}
	StreamDestinations      []types.StreamDestination
	ActiveStreamForwardings map[types.DestinationID]*ActiveStreamForwarding
}

func New(
	cfg *types.Config,
	platformsController types.PlatformsController,
	browserOpener types.BrowserOpener,
) *StreamServer {
	return &StreamServer{
		Config:    cfg,
		StreamIDs: map[types.StreamID]struct{}{},

		ActiveStreamForwardings: map[types.DestinationID]*ActiveStreamForwarding{},
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
		err := s.addStreamDestination(ctx, dstID, dstCfg.URL)
		if err != nil {
			return fmt.Errorf(
				"unable to initialize stream destination '%s' to %#+v: %w",
				dstID,
				dstCfg,
				err,
			)
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
					return fmt.Errorf(
						"unable to launch stream forward from '%s' to '%s': %w",
						streamID,
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

func (s *StreamServer) AddIncomingStream(
	ctx context.Context,
	streamID types.StreamID,
) error {
	return xsync.DoR1(ctx, &s.Mutex, func() error {
		err := s.addIncomingStream(ctx, streamID)
		if err != nil {
			return err
		}
		s.Config.Streams[streamID] = &types.StreamConfig{}
		return nil
	})
}

func assertEqual(a, b any) {
	if !reflect.DeepEqual(a, b) {
		panic(fmt.Errorf("%#+v and %#+v are supposed to be equal", a, b))
	}
}

func (s *StreamServer) addIncomingStream(
	ctx context.Context,
	streamID types.StreamID,
) error {
	if _, ok := s.StreamIDs[streamID]; ok {
		return fmt.Errorf("stream '%s' already exists", streamID)
	}

	s.StreamIDs[streamID] = struct{}{}

	if err := s.updateNewStreamIDs(ctx); err != nil {
		return fmt.Errorf("unable to update the livego config: %w", err)
	}
	return nil
}

func (s *StreamServer) updateNewStreamIDs(
	ctx context.Context,
) error {
	apps := make(configure.Applications, 0, len(s.StreamIDs))
	for streamID := range s.StreamIDs {
		apps = append(apps, configure.Application{
			Appname: string(streamID),
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

func (s *StreamServer) ListIncomingStreams(
	ctx context.Context,
) []types.IncomingStream {
	return xsync.DoA1R1(ctx, &s.Mutex, s.listIncomingStreams, ctx)
}

func (s *StreamServer) listIncomingStreams(
	_ context.Context,
) []types.IncomingStream {
	var result []types.IncomingStream
	for streamID := range s.StreamIDs {
		result = append(
			result,
			types.IncomingStream{
				StreamID:      streamID,
				NumBytesWrote: 0, // TODO: fill the value
				NumBytesRead:  0, // TODO: fill the value
			},
		)
	}
	return result
}

func (s *StreamServer) RemoveIncomingStream(
	ctx context.Context,
	streamID types.StreamID,
) error {
	return xsync.DoR1(ctx, &s.Mutex, func() error {
		delete(s.Config.Streams, streamID)
		return s.removeIncomingStream(ctx, streamID)
	})
}

func (s *StreamServer) removeIncomingStream(
	ctx context.Context,
	streamID types.StreamID,
) error {
	if _, ok := s.StreamIDs[streamID]; !ok {
		return fmt.Errorf("stream '%s' does not exist", streamID)
	}
	delete(s.StreamIDs, streamID)

	if err := s.updateNewStreamIDs(ctx); err != nil {
		return fmt.Errorf("unable to update the livego config: %w", err)
	}

	return nil
}

type StreamForward = types.StreamForward[*ActiveStreamForwarding]

func (s *StreamServer) AddStreamForward(
	ctx context.Context,
	streamID types.StreamID,
	destinationID types.DestinationID,
	enabled bool,
	quirks types.ForwardingQuirks,
) (*types.StreamForward[*ActiveStreamForwarding], error) {
	return xsync.DoR2(ctx, &s.Mutex, func() (*types.StreamForward[*ActiveStreamForwarding], error) {
		streamConfig := s.Config.Streams[streamID]
		if streamConfig.Forwardings == nil {
			streamConfig.Forwardings = map[types.DestinationID]types.ForwardingConfig{}
		}

		if _, ok := streamConfig.Forwardings[destinationID]; ok {
			return nil, fmt.Errorf("the forwarding %s->%s already exists", streamID, destinationID)
		}

		cfg := types.ForwardingConfig{
			Disabled: !enabled,
			Quirks:   quirks,
		}

		var fwd *types.StreamForward[*ActiveStreamForwarding]
		if enabled {
			var err error
			fwd, err = s.addStreamForward(ctx, streamID, destinationID, quirks)
			if err != nil {
				return fwd, err
			}
		} else {
			fwd = buildStreamForward(streamID, destinationID, cfg, nil)
		}
		streamConfig.Forwardings[destinationID] = cfg
		return fwd, nil
	})
}

func (s *StreamServer) addStreamForward(
	ctx context.Context,
	streamID types.StreamID,
	destinationID types.DestinationID,
	quirks types.ForwardingQuirks,
) (*types.StreamForward[*ActiveStreamForwarding], error) {
	cfg := types.ForwardingConfig{
		Disabled: true,
		Quirks:   quirks,
	}

	ctx = belt.WithField(ctx, "stream_forward", fmt.Sprintf("%s->%s", streamID, destinationID))
	if actFwd, ok := s.ActiveStreamForwardings[destinationID]; ok {
		return buildStreamForward(
				streamID,
				destinationID,
				cfg,
				actFwd,
			), fmt.Errorf(
				"there is already an active stream forwarding to '%s'",
				destinationID,
			)
	}

	dst, err := s.findStreamDestinationByID(ctx, destinationID)
	if err != nil {
		return nil, fmt.Errorf("unable to find stream destination '%s': %w", destinationID, err)
	}

	if len(s.ServerHandlers) == 0 {
		return nil, fmt.Errorf("no open ports")
	}
	h := s.ServerHandlers[0]

	urlSrc := "rtmp://" + h.ListenAddr() + "/" + string(streamID)
	actFwd, err := newActiveStreamForward(ctx, streamID, destinationID, urlSrc, dst.URL)
	if err != nil {
		return nil, fmt.Errorf("unable to run the stream forwarding: %w", err)
	}
	s.ActiveStreamForwardings[destinationID] = actFwd

	return buildStreamForward(streamID, destinationID, cfg, actFwd), nil
}

func buildStreamForward(
	streamID types.StreamID,
	destinationID types.DestinationID,
	cfg types.ForwardingConfig,
	actFwd *ActiveStreamForwarding,
) *types.StreamForward[*ActiveStreamForwarding] {
	return &types.StreamForward[*ActiveStreamForwarding]{
		StreamID:         streamID,
		DestinationID:    destinationID,
		Enabled:          !cfg.Disabled,
		Quirks:           cfg.Quirks,
		ActiveForwarding: actFwd,
		NumBytesWrote:    0, // TODO: fill this value
		NumBytesRead:     0, // TODO: fill this value
	}
}

func (a *StreamServer) ActiveIncomingStreamIDs() []types.StreamID {
}

func (a *StreamServer) WaitPublisherChan(
	ctx context.Context,
	streamID types.StreamID,
) (<-chan types.Publisher, error) {
}

func (s *StreamServer) UpdateStreamForward(
	ctx context.Context,
	streamID types.StreamID,
	destinationID types.DestinationID,
	enabled bool,
	quirks types.ForwardingQuirks,
) (*types.StreamForward[*ActiveStreamForwarding], error) {
	return xsync.DoR1(ctx, &s.Mutex, func() error {
		streamConfig := s.Config.Streams[streamID]
		fwdCfg, ok := streamConfig.Forwardings[destinationID]
		if !ok {
			return fmt.Errorf("the forwarding %s->%s does not exist", streamID, destinationID)
		}

		if fwdCfg.Disabled && enabled {
			err := s.addStreamForward(ctx, streamID, destinationID)
			if err != nil {
				return err
			}
		}
		if !fwdCfg.Disabled && !enabled {
			err := s.removeStreamForward(ctx, streamID, destinationID)
			if err != nil {
				return err
			}
		}
		streamConfig.Forwardings[destinationID] = types.ForwardingConfig{
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
			StreamID types.StreamID
			DestID   types.DestinationID
		}
		m := map[fwdID]*StreamForward{}
		for idx := range activeStreamForwards {
			fwd := &activeStreamForwards[idx]
			m[fwdID{
				StreamID: fwd.StreamID,
				DestID:   fwd.DestinationID,
			}] = fwd
		}

		var result []StreamForward
		for streamID, stream := range s.Config.Streams {
			for dstID, cfg := range stream.Forwardings {
				item := StreamForward{
					StreamID:      streamID,
					DestinationID: dstID,
					Enabled:       !cfg.Disabled,
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
	})
}

func (s *StreamServer) listStreamForwards(
	_ context.Context,
) ([]StreamForward, error) {
	var result []StreamForward
	for _, fwd := range s.ActiveStreamForwardings {
		result = append(result, StreamForward{
			StreamID:      fwd.StreamID,
			DestinationID: fwd.DestinationID,
			Enabled:       true,
			NumBytesWrote: 0,
			NumBytesRead:  0,
		})
	}
	return result, nil
}

func (s *StreamServer) RemoveStreamForward(
	ctx context.Context,
	streamID types.StreamID,
	dstID types.DestinationID,
) error {
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
	_ types.StreamID,
	dstID types.DestinationID,
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

func (s *StreamServer) ListStreamDestinations(
	ctx context.Context,
) ([]types.StreamDestination, error) {
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
	streamForwards, err := s.listStreamForwards(ctx)
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
	return types.StreamDestination{}, fmt.Errorf(
		"unable to find a stream destination by StreamID '%s'",
		destinationID,
	)
}

func (s *StreamServer) AddStreamPlayer(
	ctx context.Context,
	streamID types.StreamID,
	playerType player.Backend,
	disabled bool,
	streamPlaybackConfig sptypes.Config,
	opts ...types.StreamPlayerOption,
) error {

}

func (s *StreamServer) UpdateStreamPlayer(
	ctx context.Context,
	streamID types.StreamID,
	playerType player.Backend,
	disabled bool,
	streamPlaybackConfig sptypes.Config,
	opts ...types.StreamPlayerOption,
) error {

}

func (s *StreamServer) RemoveStreamPlayer(
	ctx context.Context,
	streamID types.StreamID,
) error {

}

func (s *StreamServer) ListStreamPlayers(
	ctx context.Context,
) ([]types.StreamPlayer, error) {

}

func (s *StreamServer) GetStreamPlayer(
	ctx context.Context,
	streamID types.StreamID,
) (*types.StreamPlayer, error) {

}

func (s *StreamServer) GetActiveStreamPlayer(
	ctx context.Context,
	streamID types.StreamID,
) (player.Player, error) {

}

func (s *StreamServer) GetPortServers(
	ctx context.Context,
) ([]streamplayer.StreamPortServer, error) {

}
