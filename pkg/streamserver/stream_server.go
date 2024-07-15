package streamserver

import (
	"context"
	"fmt"
	"sync"

	rtmpserver "github.com/xaionaro-go/streamctl/pkg/streamserver/server/rtmp"
	rtspserver "github.com/xaionaro-go/streamctl/pkg/streamserver/server/rtsp"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/streams"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/types"
)

type StreamServer struct {
	sync.Mutex
	Config             types.Config
	StreamHandler      *streams.StreamHandler
	ServerHandlers     []types.ServerHandler
	StreamDestinations []types.StreamDestination
}

func New(cfg *types.Config) *StreamServer {
	if cfg == nil {
		cfg = &types.Config{}
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
		Config:        *cfg,
	}
}

func (s *StreamServer) Init(ctx context.Context) error {
	cfg := s.Config

	for _, srv := range cfg.Servers {
		err := s.StartServer(ctx, srv.Type, srv.Listen)
		if err != nil {
			return fmt.Errorf("unable to initialize %s server at %s: %w", srv.Type, srv.Listen, err)
		}
	}

	for dstID, dstCfg := range cfg.Destinations {
		err := s.AddStreamDestination(ctx, dstID, dstCfg.URL)
		if err != nil {
			return fmt.Errorf("unable to initialize stream destination '%s' to %#+v: %w", dstID, dstCfg, err)
		}
	}

	for streamID, streamCfg := range cfg.Streams {
		err := s.AddIncomingStream(ctx, streamID)
		if err != nil {
			return fmt.Errorf("unable to initialize stream '%s': %w", streamID, err)
		}

		for _, fwd := range streamCfg.Forwardings {
			err := s.AddStreamForward(ctx, streamID, fwd)
			if err != nil {
				return fmt.Errorf("unable to launch stream forward from '%s' to '%s': %w", streamID, fwd, err)
			}
		}
	}

	return nil
}

func (s *StreamServer) ListServers(
	ctx context.Context,
) []types.ServerHandler {
	s.Lock()
	defer s.Unlock()
	c := make([]types.ServerHandler, len(s.ServerHandlers))
	copy(c, s.ServerHandlers)
	return c
}

func (s *StreamServer) StartServer(
	ctx context.Context,
	serverType types.ServerType,
	listenAddr string,
) error {
	var srv types.ServerHandler
	var err error
	switch serverType {
	case types.ServerTypeRTMP:
		srv, err = rtmpserver.New(ctx, rtmpserver.Config{
			Listen: listenAddr,
		}, s.StreamHandler)
	case types.ServerTypeRTSP:
		srv, err = rtspserver.New(ctx, rtspserver.Config{
			ListenAddr: listenAddr,
		}, s.StreamHandler)
	default:
		return fmt.Errorf("unexpected server type %v", serverType)
	}
	if err != nil {
		return err
	}

	s.Lock()
	defer s.Unlock()
	s.ServerHandlers = append(s.ServerHandlers, srv)
	s.Config.Servers = append(s.Config.Servers, types.Server{
		Type:   serverType,
		Listen: listenAddr,
	})
	return nil
}

func (s *StreamServer) findServer(
	_ context.Context,
	server types.ServerHandler,
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
	server types.ServerHandler,
) error {
	s.Lock()
	defer s.Unlock()
	return s.stopServer(ctx, server)
}

func (s *StreamServer) stopServer(
	ctx context.Context,
	server types.ServerHandler,
) error {
	for idx, srv := range s.Config.Servers {
		if srv.Listen == server.ListenAddr() {
			s.Config.Servers = append(s.Config.Servers[:idx], s.Config.Servers[idx+1:]...)
			break
		}
	}

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
	s.Lock()
	defer s.Unlock()
	return s.addIncomingStream(ctx, streamID)
}

func (s *StreamServer) addIncomingStream(
	_ context.Context,
	streamID types.StreamID,
) error {
	if s.StreamHandler.Get(string(streamID)) != nil {
		return fmt.Errorf("stream '%s' already exists", streamID)
	}
	_, err := s.StreamHandler.New(string(streamID), "")
	if err != nil {
		return fmt.Errorf("unable to create the stream '%s': %w", streamID, err)
	}
	s.Config.Streams[streamID] = &types.StreamConfig{}
	return nil
}

type IncomingStream struct {
	StreamID types.StreamID
}

func (s *StreamServer) ListIncomingStreams(
	ctx context.Context,
) []IncomingStream {
	s.Lock()
	defer s.Unlock()
	return s.listIncomingStreams(ctx)
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
	s.Lock()
	defer s.Unlock()
	return s.removeIncomingStream(ctx, streamID)
}

func (s *StreamServer) removeIncomingStream(
	_ context.Context,
	streamID types.StreamID,
) error {
	if s.StreamHandler.Get(string(streamID)) == nil {
		return fmt.Errorf("stream '%s' does not exist", streamID)
	}
	s.StreamHandler.Delete(string(streamID))
	delete(s.Config.Streams, streamID)
	return nil
}

type StreamForward struct {
	StreamID      types.StreamID
	DestinationID types.DestinationID
}

func (s *StreamServer) AddStreamForward(
	ctx context.Context,
	streamID types.StreamID,
	destinationID types.DestinationID,
) error {
	s.Lock()
	defer s.Unlock()
	return s.addStreamForward(ctx, streamID, destinationID)
}

func (s *StreamServer) addStreamForward(
	ctx context.Context,
	streamID types.StreamID,
	destinationID types.DestinationID,
) error {
	streamSrc := s.StreamHandler.Get(string(streamID))
	if streamSrc != nil {
		return fmt.Errorf("unable to find stream ID '%s'", streamID)
	}
	dst, err := s.findStreamDestinationByID(ctx, destinationID)
	if err != nil {
		return fmt.Errorf("unable to find stream destination '%s': %w", destinationID, err)
	}
	_, err = streamSrc.Publish(ctx, dst.URL)
	if err != nil {
		return fmt.Errorf("unable to start publishing '%s' to '%s': %w", streamID, dst.URL, err)
	}
	streamConfig := s.Config.Streams[streamID]
	streamConfig.Forwardings = append(streamConfig.Forwardings, destinationID)
	return nil
}

func (s *StreamServer) ListStreamForwards(
	ctx context.Context,
) ([]StreamForward, error) {
	s.Lock()
	defer s.Unlock()
	return s.listStreamForwards(ctx)
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
	s.Lock()
	defer s.Unlock()
	return s.removeStreamForward(ctx, streamID, dstID)
}

func (s *StreamServer) removeStreamForward(
	ctx context.Context,
	streamID types.StreamID,
	dstID types.DestinationID,
) error {
	streamCfg := s.Config.Streams[streamID]
	for idx, _dstID := range streamCfg.Forwardings {
		if _dstID != dstID {
			continue
		}
		streamCfg.Forwardings = append(streamCfg.Forwardings[:idx], streamCfg.Forwardings[idx+1:]...)
		break
	}

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
	s.Mutex.Lock()
	defer s.Mutex.Unlock()
	return s.addStreamDestination(ctx, destinationID, url)
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
	s.Config.Destinations[destinationID] = &types.DestinationConfig{URL: url}
	return nil
}

func (s *StreamServer) RemoveStreamDestination(
	ctx context.Context,
	destinationID types.DestinationID,
) error {
	s.Mutex.Lock()
	defer s.Mutex.Unlock()

	streamForwards, err := s.listStreamForwards(ctx)
	if err != nil {
		return fmt.Errorf("unable to list stream forwardings: %w", err)
	}
	for _, fwd := range streamForwards {
		if fwd.DestinationID == destinationID {
			s.removeStreamForward(ctx, fwd.StreamID, fwd.DestinationID)
		}
	}

	delete(s.Config.Destinations, destinationID)

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
