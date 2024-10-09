package streamserver

import (
	"context"
	"fmt"
	"strings"

	"github.com/facebookincubator/go-belt/tool/logger"
	rtmpserver "github.com/xaionaro-go/streamctl/pkg/streamserver/implementations/go2rtc/streamserver/server/rtmp"
	rtspserver "github.com/xaionaro-go/streamctl/pkg/streamserver/implementations/go2rtc/streamserver/server/rtsp"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/implementations/go2rtc/streamserver/streams"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/implementations/xaionaro-go-rtmp/streamforward"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/types"
	"github.com/xaionaro-go/streamctl/pkg/streamtypes"
	"github.com/xaionaro-go/streamctl/pkg/xsync"
)

type StreamServer struct {
	xsync.Mutex
	Config             *types.Config
	StreamHandler      *streams.StreamHandler
	ServerHandlers     []types.PortServer
	StreamDestinations []types.StreamDestination
}

var _ streamforward.StreamServer = (*StreamServer)(nil)

func New(
	cfg *types.Config,
	platformsController types.PlatformsController,
	browserOpener types.BrowserOpener,
) *StreamServer {
	assert(cfg != nil)
	logger.Default().Debugf("config == %#+v", *cfg)

	if cfg.Streams == nil {
		cfg.Streams = map[types.StreamID]*types.StreamConfig{}
	}
	if cfg.Destinations == nil {
		cfg.Destinations = map[types.DestinationID]*types.DestinationConfig{}
	}
	s := streams.NewStreamHandler()

	s.HandleFunc("rtmp", rtmpserver.StreamsHandle)
	s.HandleFunc("rtmps", rtmpserver.StreamsHandle)
	s.HandleFunc("rtmpx", rtmpserver.StreamsHandle)
	s.HandleConsumerFunc("rtmp", rtmpserver.StreamsConsumerHandle)
	s.HandleConsumerFunc("rtmps", rtmpserver.StreamsConsumerHandle)
	s.HandleConsumerFunc("rtmpx", rtmpserver.StreamsConsumerHandle)
	s.HandleFunc("rtsp", rtspserver.Handler)
	s.HandleFunc("rtsps", rtspserver.Handler)
	s.HandleFunc("rtspx", rtspserver.Handler)

	return &StreamServer{
		StreamHandler: s,
		Config:        cfg,
	}
}

func (s *StreamServer) Init(ctx context.Context) error {
	return xsync.DoA1R1(ctx, &s.Mutex, s.initNoLock, ctx)
}
func (s *StreamServer) initNoLock(ctx context.Context) error {
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
				err := s.addStreamForward(ctx, streamID, dstID)
				if err != nil {
					return fmt.Errorf("unable to launch stream forward from '%s' to '%s': %w", streamID, dstID, err)
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
		srv, err = rtmpserver.New(ctx, rtmpserver.Config{
			Listen: listenAddr,
		}, s.StreamHandler)
	case streamtypes.ServerTypeRTSP:
		srv, err = rtspserver.New(ctx, rtspserver.Config{
			ListenAddr: listenAddr,
		}, s.StreamHandler)
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

func (s *StreamServer) addIncomingStream(
	_ context.Context,
	streamID types.StreamID,
) error {
	if s.StreamHandler.Get(string(streamID)) != nil {
		return fmt.Errorf("stream '%s' already exists", streamID)
	}
	_, err := s.StreamHandler.New(string(streamID), nil)
	if err != nil {
		return fmt.Errorf("unable to create the stream '%s': %w", streamID, err)
	}
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
	return xsync.DoA1R1(ctx, &s.Mutex, s.listIncomingStreams, ctx)
}

func (s *StreamServer) listIncomingStreams(
	_ context.Context,
) []IncomingStream {
	var result []IncomingStream
	for _, name := range s.StreamHandler.GetAll() {
		result = append(
			result,
			IncomingStream{
				StreamID: types.StreamID(name),
			},
		)
	}
	return result
}

func (s *StreamServer) RemoveIncomingStream(
	ctx context.Context,
	streamID types.StreamID,
) error {
	var err error
	s.Do(ctx, func() {
		delete(s.Config.Streams, streamID)
		err = s.removeIncomingStream(ctx, streamID)
	})
	return err
}

func (s *StreamServer) removeIncomingStream(
	_ context.Context,
	streamID types.StreamID,
) error {
	if s.StreamHandler.Get(string(streamID)) == nil {
		return fmt.Errorf("stream '%s' does not exist", streamID)
	}
	s.StreamHandler.Delete(string(streamID))
	return nil
}

type StreamForward struct {
	StreamID      types.StreamID
	DestinationID types.DestinationID
	Enabled       bool
	NumBytesWrote uint64
	NumBytesRead  uint64
}

func (s *StreamServer) AddStreamForward(
	ctx context.Context,
	streamID types.StreamID,
	destinationID types.DestinationID,
	enabled bool,
) error {
	return xsync.DoR1(ctx, &s.Mutex, func() error {
		streamConfig := s.Config.Streams[streamID]
		if _, ok := streamConfig.Forwardings[destinationID]; ok {
			return fmt.Errorf("the forwarding %s->%s already exists", streamID, destinationID)
		}

		if enabled {
			err := s.addStreamForward(ctx, streamID, destinationID)
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

func (s *StreamServer) addStreamForward(
	ctx context.Context,
	streamID types.StreamID,
	destinationID types.DestinationID,
) error {
	streamSrc := s.StreamHandler.Get(string(streamID))
	if streamSrc == nil {
		return fmt.Errorf("unable to find stream ID '%s', available stream IDs: %s", streamID, strings.Join(s.StreamHandler.GetAll(), ", "))
	}
	dst, err := s.findStreamDestinationByID(ctx, destinationID)
	if err != nil {
		return fmt.Errorf("unable to find stream destination '%s': %w", destinationID, err)
	}
	_, err = streamSrc.Publish(ctx, dst.URL)
	if err != nil {
		return fmt.Errorf("unable to start publishing '%s' to '%s': %w", streamID, dst.URL, err)
	}
	return nil
}

func (s *StreamServer) UpdateStreamForward(
	ctx context.Context,
	streamID types.StreamID,
	destinationID types.DestinationID,
	enabled bool,
) error {
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
	ctx context.Context,
) ([]StreamForward, error) {
	var result []StreamForward
	for _, name := range s.StreamHandler.GetAll() {
		stream := s.StreamHandler.Get(name)
		if stream == nil {
			continue
		}
		for _, fwd := range stream.Forwardings() {
			streamIDSrc := types.StreamID(name)
			streamDst, err := s.findStreamDestinationByURL(ctx, fwd.URL)
			if err != nil {
				return nil, fmt.Errorf("unable to convert URL '%s' to a stream ID: %w", fwd.URL, err)
			}
			result = append(result, StreamForward{
				StreamID:      streamIDSrc,
				DestinationID: streamDst.ID,
				Enabled:       true,
				NumBytesWrote: fwd.TrafficCounter.NumBytesWrote(),
				NumBytesRead:  fwd.TrafficCounter.NumBytesRead(),
			})
		}
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
	ctx context.Context,
	streamID types.StreamID,
	dstID types.DestinationID,
) error {
	stream := s.StreamHandler.Get(string(streamID))
	if stream == nil {
		return fmt.Errorf("unable to find a source stream with ID '%s'", streamID)
	}
	for _, fwd := range stream.Forwardings() {
		streamDst, err := s.findStreamDestinationByURL(ctx, fwd.URL)
		if err != nil {
			return fmt.Errorf("unable to convert URL '%s' to a stream ID: %w", fwd.URL, err)
		}
		if streamDst.ID != dstID {
			continue
		}

		err = fwd.Close()
		if err != nil {
			return fmt.Errorf("unable to close forwarding from %s to %s (%s): %w", streamID, dstID, fwd.URL, err)
		}
		stream.Cleanup()
		return nil
	}
	return fmt.Errorf("unable to find stream forwarding from '%s' to '%s'", streamID, dstID)
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

func (s *StreamServer) findStreamDestinationByURL(
	_ context.Context,
	url string,
) (types.StreamDestination, error) {
	for _, dst := range s.StreamDestinations {
		if dst.URL == url {
			return dst, nil
		}
	}
	return types.StreamDestination{}, fmt.Errorf("unable to find a stream destination by URL '%s'", url)
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
