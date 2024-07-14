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
	StreamHandler      *streams.StreamHandler
	ServerHandlers     []types.ServerHandler
	StreamDestinations []types.StreamDestination
}

func New() *StreamServer {
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
	}
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
	return nil
}

func (s *StreamServer) StopServer(
	ctx context.Context,
	server types.ServerHandler,
) error {
	s.Lock()
	defer s.Unlock()

	for i := range s.ServerHandlers {
		if s.ServerHandlers[i] == server {
			s.ServerHandlers = append(s.ServerHandlers[:i], s.ServerHandlers[i+1:]...)
			return server.Close()
		}
	}

	return fmt.Errorf("server not found")
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
	return nil
}

type StreamForward struct {
	StreamIDSrc types.StreamID
	StreamIDDst types.StreamID
}

func (s *StreamServer) AddStreamForward(
	ctx context.Context,
	streamIDSrc types.StreamID,
	streamIDDst types.StreamID,
) error {
	s.Lock()
	defer s.Unlock()
	return s.addStreamForward(ctx, streamIDSrc, streamIDDst)
}

func (s *StreamServer) addStreamForward(
	ctx context.Context,
	streamIDSrc types.StreamID,
	streamIDDst types.StreamID,
) error {
	streamSrc := s.StreamHandler.Get(string(streamIDSrc))
	if streamSrc != nil {
		return fmt.Errorf("unable to find stream ID '%s'", streamIDSrc)
	}
	dst, err := s.findStreamDestinationByID(ctx, streamIDDst)
	if err != nil {
		return fmt.Errorf("unable to find stream destination '%s': %w", streamIDDst, err)
	}
	_, err = streamSrc.Publish(ctx, dst.URL)
	if err != nil {
		return fmt.Errorf("unable to start publishing '%s' to '%s': %w", streamIDSrc, dst.URL, err)
	}
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
				StreamIDSrc: streamIDSrc,
				StreamIDDst: streamDst.StreamID,
			})
		}
	}
	return result, nil
}

func (s *StreamServer) RemoveStreamForward(
	ctx context.Context,
	streamIDSrc types.StreamID,
	streamIDDst types.StreamID,
) error {
	s.Lock()
	defer s.Unlock()
	return s.removeStreamForward(ctx, streamIDSrc, streamIDDst)
}

func (s *StreamServer) removeStreamForward(
	ctx context.Context,
	streamIDSrc types.StreamID,
	streamIDDst types.StreamID,
) error {
	stream := s.StreamHandler.Get(string(streamIDSrc))
	if stream == nil {
		return fmt.Errorf("unable to find a source stream with ID '%s'", streamIDSrc)
	}
	for _, fwd := range stream.Forwardings() {
		streamDst, err := s.findStreamDestinationByURL(ctx, fwd.URL)
		if err != nil {
			return fmt.Errorf("unable to convert URL '%s' to a stream ID: %w", fwd.URL, err)
		}
		if streamDst.StreamID != streamIDDst {
			continue
		}

		err = fwd.Close()
		if err != nil {
			return fmt.Errorf("unable to close forwarding from %s to %s (%s): %w", streamIDSrc, streamIDDst, fwd.URL, err)
		}
		stream.Cleanup()
		return nil
	}
	return fmt.Errorf("unable to find stream forwarding from '%s' to '%s'", streamIDSrc, streamIDDst)
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
	streamID types.StreamID,
	url string,
) error {
	s.Mutex.Lock()
	defer s.Mutex.Unlock()
	return s.addStreamDestination(ctx, streamID, url)
}

func (s *StreamServer) addStreamDestination(
	_ context.Context,
	streamID types.StreamID,
	url string,
) error {
	s.StreamDestinations = append(s.StreamDestinations, types.StreamDestination{
		StreamID: streamID,
		URL:      url,
	})
	return nil
}

func (s *StreamServer) RemoveStreamDestination(
	ctx context.Context,
	streamID types.StreamID,
) error {
	s.Mutex.Lock()
	defer s.Mutex.Unlock()

	streamForwards, err := s.listStreamForwards(ctx)
	if err != nil {
		return fmt.Errorf("unable to list stream forwardings: %w", err)
	}
	for _, fwd := range streamForwards {
		if fwd.StreamIDDst == streamID {
			s.removeStreamForward(ctx, fwd.StreamIDSrc, fwd.StreamIDDst)
		}
	}

	for i := range s.StreamDestinations {
		if s.StreamDestinations[i].StreamID == streamID {
			s.StreamDestinations = append(s.StreamDestinations[:i], s.StreamDestinations[i+1:]...)
			return nil
		}
	}

	return fmt.Errorf("have not found stream destination with id %s", streamID)
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
	streamID types.StreamID,
) (types.StreamDestination, error) {
	for _, dst := range s.StreamDestinations {
		if dst.StreamID == streamID {
			return dst, nil
		}
	}
	return types.StreamDestination{}, fmt.Errorf("unable to find a stream destination by StreamID '%s'", streamID)
}
