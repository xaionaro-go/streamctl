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
	"github.com/xaionaro-go/xsync"
)

type StreamServer struct {
	xsync.Mutex
	Config             *types.Config
	StreamHandler      *streams.StreamHandler
	ServerHandlers     []types.PortServer
	StreamSinks []types.StreamSink
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
		cfg.Streams = map[types.StreamSourceID]*types.StreamConfig{}
	}
	if cfg.Destinations == nil {
		cfg.Destinations = map[types.StreamSinkID]*types.StreamSinkConfig{}
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
				err := s.addStreamForward(ctx, streamSourceID, dstID)
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

func (s *StreamServer) addStreamSource(
	_ context.Context,
	streamSourceID types.StreamSourceID,
) error {
	if s.StreamHandler.Get(string(streamSourceID)) != nil {
		return fmt.Errorf("stream '%s' already exists", streamSourceID)
	}
	_, err := s.StreamHandler.New(string(streamSourceID), nil)
	if err != nil {
		return fmt.Errorf("unable to create the stream '%s': %w", streamSourceID, err)
	}
	return nil
}

type StreamSource struct {
	StreamSourceID types.StreamSourceID

	NumBytesWrote uint64
	NumBytesRead  uint64
}

func (s *StreamServer) ListStreamSources(
	ctx context.Context,
) []StreamSource {
	return xsync.DoA1R1(ctx, &s.Mutex, s.listStreamSources, ctx)
}

func (s *StreamServer) listStreamSources(
	_ context.Context,
) []StreamSource {
	var result []StreamSource
	for _, name := range s.StreamHandler.GetAll() {
		result = append(
			result,
			StreamSource{
				StreamSourceID: types.StreamSourceID(name),
			},
		)
	}
	return result
}

func (s *StreamServer) RemoveStreamSource(
	ctx context.Context,
	streamSourceID types.StreamSourceID,
) error {
	var err error
	s.Do(ctx, func() {
		delete(s.Config.Streams, streamSourceID)
		err = s.removeStreamSource(ctx, streamSourceID)
	})
	return err
}

func (s *StreamServer) removeStreamSource(
	_ context.Context,
	streamSourceID types.StreamSourceID,
) error {
	if s.StreamHandler.Get(string(streamSourceID)) == nil {
		return fmt.Errorf("stream '%s' does not exist", streamSourceID)
	}
	s.StreamHandler.Delete(string(streamSourceID))
	return nil
}

type StreamForward struct {
	StreamSourceID      types.StreamSourceID
	StreamSinkID types.StreamSinkID
	Enabled       bool
	NumBytesWrote uint64
	NumBytesRead  uint64
}

func (s *StreamServer) AddStreamForward(
	ctx context.Context,
	streamSourceID types.StreamSourceID,
	streamSinkID types.StreamSinkID,
	enabled bool,
) error {
	return xsync.DoR1(ctx, &s.Mutex, func() error {
		streamConfig := s.Config.Streams[streamSourceID]
		if _, ok := streamConfig.Forwardings[streamSinkID]; ok {
			return fmt.Errorf("the forwarding %s->%s already exists", streamSourceID, streamSinkID)
		}

		if enabled {
			err := s.addStreamForward(ctx, streamSourceID, streamSinkID)
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

func (s *StreamServer) addStreamForward(
	ctx context.Context,
	streamSourceID types.StreamSourceID,
	streamSinkID types.StreamSinkID,
) error {
	streamSrc := s.StreamHandler.Get(string(streamSourceID))
	if streamSrc == nil {
		return fmt.Errorf(
			"unable to find stream ID '%s', available stream IDs: %s",
			streamSourceID,
			strings.Join(s.StreamHandler.GetAll(), ", "),
		)
	}
	dst, err := s.findStreamSinkByID(ctx, streamSinkID)
	if err != nil {
		return fmt.Errorf("unable to find stream destination '%s': %w", streamSinkID, err)
	}
	_, err = streamSrc.Publish(ctx, dst.URL)
	if err != nil {
		return fmt.Errorf("unable to start publishing '%s' to '%s': %w", streamSourceID, dst.URL, err)
	}
	return nil
}

func (s *StreamServer) UpdateStreamForward(
	ctx context.Context,
	streamSourceID types.StreamSourceID,
	streamSinkID types.StreamSinkID,
	enabled bool,
) error {
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
			DestID   types.StreamSinkID
		}
		m := map[fwdID]*StreamForward{}
		for idx := range activeStreamForwards {
			fwd := &activeStreamForwards[idx]
			m[fwdID{
				StreamSourceID: fwd.StreamSourceID,
				DestID:   fwd.StreamSinkID,
			}] = fwd
		}

		var result []StreamForward
		for streamSourceID, stream := range s.Config.Streams {
			for dstID, cfg := range stream.Forwardings {
				item := StreamForward{
					StreamSourceID:      streamSourceID,
					StreamSinkID: dstID,
					Enabled:       !cfg.Disabled,
				}
				if activeFwd, ok := m[fwdID{
					StreamSourceID: streamSourceID,
					DestID:   dstID,
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
	ctx context.Context,
) ([]StreamForward, error) {
	var result []StreamForward
	for _, name := range s.StreamHandler.GetAll() {
		stream := s.StreamHandler.Get(name)
		if stream == nil {
			continue
		}
		for _, fwd := range stream.Forwardings() {
			streamIDSrc := types.StreamSourceID(name)
			streamDst, err := s.findStreamSinkByURL(ctx, fwd.URL)
			if err != nil {
				return nil, fmt.Errorf(
					"unable to convert URL '%s' to a stream ID: %w",
					fwd.URL,
					err,
				)
			}
			result = append(result, StreamForward{
				StreamSourceID:      streamIDSrc,
				StreamSinkID: streamDst.ID,
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
	ctx context.Context,
	streamSourceID types.StreamSourceID,
	dstID types.StreamSinkID,
) error {
	stream := s.StreamHandler.Get(string(streamSourceID))
	if stream == nil {
		return fmt.Errorf("unable to find a source stream with ID '%s'", streamSourceID)
	}
	for _, fwd := range stream.Forwardings() {
		streamDst, err := s.findStreamSinkByURL(ctx, fwd.URL)
		if err != nil {
			return fmt.Errorf("unable to convert URL '%s' to a stream ID: %w", fwd.URL, err)
		}
		if streamDst.ID != dstID {
			continue
		}

		err = fwd.Close()
		if err != nil {
			return fmt.Errorf(
				"unable to close forwarding from %s to %s (%s): %w",
				streamSourceID,
				dstID,
				fwd.URL,
				err,
			)
		}
		stream.Cleanup()
		return nil
	}
	return fmt.Errorf("unable to find stream forwarding from '%s' to '%s'", streamSourceID, dstID)
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

func (s *StreamServer) findStreamSinkByURL(
	_ context.Context,
	url string,
) (types.StreamSink, error) {
	for _, dst := range s.StreamSinks {
		if dst.URL == url {
			return dst, nil
		}
	}
	return types.StreamSink{}, fmt.Errorf(
		"unable to find a stream destination by URL '%s'",
		url,
	)
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
