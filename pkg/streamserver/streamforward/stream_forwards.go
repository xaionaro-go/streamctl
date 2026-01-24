// Package streamforward provides functionality for forwarding streams between sources and sinks.
package streamforward

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/lockmap"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/recoder"
	"github.com/xaionaro-go/streamctl/pkg/secret"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamd/memoize"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/types"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/types/streamportserver"
	"github.com/xaionaro-go/streamctl/pkg/streamtypes"
	"github.com/xaionaro-go/xsync"
)

type ForwardingKey struct {
	StreamSourceID types.StreamSourceID
	StreamSinkID   types.StreamSinkIDFullyQualified
}

type StreamServer interface {
	types.WithConfiger
	types.WaitPublisherChaner
	types.ActiveStreamSourceIDsProvider
	types.GetPortServerser
	ListStreamSources(ctx context.Context) []types.StreamSource
}

/* for easier copy&paste

func (srv *) WithConfig(
	ctx context.Context,
	callback func(context.Context, *Config),
) {
	logger.Tracef(ctx, "WithConfig")
	defer func(){ logger.Tracef(ctx, "WithConfig") }()
}

func (srv *) WaitPublisherChan(
	ctx context.Context,
	streamSourceID streamtypes.StreamSourceID,
	waitForNext bool,
) (_ <-chan Publisher, _err error) {
	logger.Tracef(ctx, "WaitPublisherChan(ctx, '%s', %t)", streamSourceID, waitForNext)
	defer func(){ logger.Tracef(ctx, "/WaitPublisherChan(ctx, '%s', %t): %v", streamSourceID, waitForNext, _err) }()
}

func (srv *) ActiveStreamSourceIDs() (_ret []StreamSourceID, _err error) {
	ctx := context.TODO()
	logger.Tracef(ctx, "ActiveStreamSourceIDs()")
	defer func(){ logger.Tracef(ctx, "/ActiveStreamSourceIDs(): %v %v", _ret, _err) }()
}

func (srv *) GetPortServers(ctx context.Context) (_ret []Config, _err error) {
	logger.Tracef(ctx, "GetPortServers()")
	defer func(){ logger.Tracef(ctx, "/GetPortServers(): %v %v", _ret, _err) }()
}
*/

type StreamForwards struct {
	StreamServer
	types.PlatformsController
	Mutex                     xsync.Mutex
	StreamSinkStreamingLocker *lockmap.LockMap
	ActiveStreamForwardings   map[ForwardingKey]*ActiveStreamForwarding
	StreamSinks               []types.StreamSink
	RecoderFactory            func(context.Context) (recoder.Factory, error)
}

func NewStreamForwards(
	s StreamServer,
	recoderFactory func(ctx context.Context) (recoder.Factory, error),
	pc types.PlatformsController,
) *StreamForwards {
	return &StreamForwards{
		StreamServer:              s,
		RecoderFactory:            recoderFactory,
		PlatformsController:       pc,
		StreamSinkStreamingLocker: lockmap.NewLockMap(),
		ActiveStreamForwardings:   map[ForwardingKey]*ActiveStreamForwarding{},
	}
}

func (s *StreamForwards) Init(
	ctx context.Context,
	opts ...types.InitOption,
) error {
	return xsync.DoR1(ctx, &s.Mutex, func() error {
		return s.init(ctx, opts...)
	})
}

func (s *StreamForwards) init(
	ctx context.Context,
	_ ...types.InitOption,
) (_ret error) {
	s.WithConfig(ctx, func(ctx context.Context, cfg *types.Config) {
		for sinkID, sinkCfg := range cfg.StaticSinks {
			sinkID := types.NewStreamSinkIDFullyQualified(
				types.StreamSinkTypeCustom,
				sinkID,
			)
			err := s.addActiveStreamSink(ctx, sinkID, sinkCfg.URL, sinkCfg.StreamKey, sinkCfg.StreamSourceID)
			if err != nil {
				_ret = fmt.Errorf(
					"unable to initialize stream sink '%s' to %#+v: %w",
					sinkID,
					sinkCfg,
					err,
				)
				return
			}
		}

		for streamSourceID, streamCfg := range cfg.Streams {
			for sinkID, fwd := range streamCfg.Forwardings {
				if fwd.Disabled {
					continue
				}
				_, err := s.newActiveStreamForward(ctx, streamSourceID, sinkID, fwd.Encode, fwd.Quirks)
				if err != nil {
					_ret = fmt.Errorf(
						"unable to launch stream forward from '%s' to '%s': %w",
						streamSourceID,
						sinkID,
						err,
					)
					return
				}
			}
		}
	})
	return
}

func (s *StreamForwards) AddStreamForward(
	ctx context.Context,
	streamSourceID types.StreamSourceID,
	streamSinkID types.StreamSinkIDFullyQualified,
	enabled bool,
	encode types.EncodeConfig,
	quirks types.ForwardingQuirks,
) (*StreamForward, error) {
	return xsync.DoR2(ctx, &s.Mutex, func() (*StreamForward, error) {
		return s.addStreamForward(ctx, streamSourceID, streamSinkID, enabled, encode, quirks)
	})
}

func (s *StreamForwards) addStreamForward(
	ctx context.Context,
	streamSourceID types.StreamSourceID,
	streamSinkID types.StreamSinkIDFullyQualified,
	enabled bool,
	encode types.EncodeConfig,
	quirks types.ForwardingQuirks,
) (*StreamForward, error) {
	ctx = belt.WithField(ctx, "module", "StreamServer")

	var (
		streamConfig *types.StreamConfig
		err          error
	)
	s.WithConfig(ctx, func(ctx context.Context, cfg *types.Config) {
		streamConfig = cfg.Streams[streamSourceID]
		if streamConfig == nil {
			err = fmt.Errorf("stream '%s' does not exist", streamSourceID)
			return
		}

		if streamConfig.Forwardings == nil {
			streamConfig.Forwardings = make(map[streamtypes.StreamSinkIDFullyQualified]types.ForwardingConfig)
		}

		if _, ok := streamConfig.Forwardings[streamSinkID]; ok {
			err = fmt.Errorf("the forwarding %s->%s already exists", streamSourceID, streamSinkID)
			return
		}

		streamConfig.Forwardings[streamSinkID] = types.ForwardingConfig{
			Disabled: !enabled,
			Encode:   encode,
			Quirks:   quirks,
		}
	})
	if err != nil {
		return nil, err
	}

	if enabled {
		fwd, err := s.newActiveStreamForward(ctx, streamSourceID, streamSinkID, encode, quirks)
		if err != nil {
			return nil, err
		}
		return fwd, nil
	}
	return &StreamForward{
		StreamSourceID: streamSourceID,
		StreamSinkID:   streamSinkID,
		Enabled:        enabled,
		Encode:         encode,
		Quirks:         quirks,
	}, nil
}

func (s *StreamForwards) getLocalhostURL(ctx context.Context, streamSourceID types.StreamSourceID) (*url.URL, error) {
	return streamportserver.GetURLForLocalStreamID(
		ctx,
		s.StreamServer, streamSourceID,
		nil,
	)
}

func (s *StreamForwards) newActiveStreamForward(
	ctx context.Context,
	streamSourceID types.StreamSourceID,
	streamSinkID types.StreamSinkIDFullyQualified,
	encode types.EncodeConfig,
	quirks types.ForwardingQuirks,
	opts ...ActiveStreamForwardingOption,
) (*StreamForward, error) {
	ctx = belt.WithField(ctx, "stream_forward", fmt.Sprintf("%s->%s", streamSourceID, streamSinkID))
	key := ForwardingKey{
		StreamSourceID: streamSourceID,
		StreamSinkID:   streamSinkID,
	}
	if _, ok := s.ActiveStreamForwardings[key]; ok {
		return nil, fmt.Errorf(
			"there is already an active stream forwarding to '%s'",
			streamSinkID,
		)
	}

	sink, err := s.findStreamSinkByID(ctx, streamSinkID)
	if err != nil {
		return nil, fmt.Errorf("unable to find stream sink '%s': %w", streamSinkID, err)
	}

	urlParsed, err := url.Parse(sink.URL)
	if err != nil {
		return nil, fmt.Errorf("unable to parse URL '%s': %w", sink.URL, err)
	}

	if urlParsed.Host == "" {
		urlParsed, err = s.getLocalhostURL(ctx, types.StreamSourceID(urlParsed.Path))
		if err != nil {
			return nil, fmt.Errorf("unable to get the URL of the sink: %w", err)
		}
	}

	platID := streamcontrol.PlatformID("")
	if sink.StreamID != nil {
		platID = sink.StreamID.PlatformID
	}

	res, err := s.NewActiveStreamForward(
		ctx,
		streamSourceID,
		streamSinkID,
		encode,
		quirks,
		func(
			ctx context.Context,
			fwd *ActiveStreamForwarding,
		) {
			if sink.StreamID != nil {
				logger.Debugf(ctx, "fwd %s->%s is waiting for %s to be enabled", streamSourceID, streamSinkID, *sink.StreamID)
				t := time.NewTicker(time.Second)
				defer t.Stop()
				for {
					started, err := s.PlatformsController.CheckStreamStartedByStreamSourceID(
						memoize.SetNoCache(ctx, true),
						*sink.StreamID,
					)
					logger.Debugf(ctx, "%s status check: %v %v", *sink.StreamID, started, err)
					if started {
						break
					}
					select {
					case <-ctx.Done():
						return
					case <-t.C:
					}
				}
			}

			if quirks.WaitUntilPlatformRecognizesStream.Enabled {
				if platID == "" {
					logger.Errorf(ctx, "WaitUntilPlatformRecognizesStream is enabled, but PlatformID is not specified (StreamSourceID is nil in sink)")
					return
				}
				if quirks.RestartUntilPlatformRecognizesStream.Enabled {
					logger.Errorf(
						ctx,
						"WaitUntilPlatformRecognizesStream should not be used together with RestartUntilPlatformRecognizesStream",
					)
				} else {
					logger.Debugf(ctx, "fwd %s->%s is waiting for %s to recognize the stream", streamSourceID, streamSinkID, platID)
					started, err := s.PlatformsController.CheckStreamStartedByPlatformID(
						memoize.SetNoCache(ctx, true),
						platID,
					)
					logger.Debugf(ctx, "%s status check: %v %v", platID, started, err)
					if started {
						return
					}
					t := time.NewTicker(time.Second)
					defer t.Stop()
					for {
						select {
						case <-ctx.Done():
							return
						case <-t.C:
						}
						started, err := s.PlatformsController.CheckStreamStartedByPlatformID(
							ctx,
							platID,
						)
						logger.Debugf(ctx, "%s status check: %v %v", platID, started, err)
						if started {
							return
						}
					}
				}
			}
		},
		opts...,
	)
	if err != nil {
		return nil, fmt.Errorf("unable to run the stream forwarding: %w", err)
	}
	s.ActiveStreamForwardings[key] = res.ActiveForwarding

	if quirks.RestartUntilPlatformRecognizesStream.Enabled {
		observability.Go(ctx, func(ctx context.Context) {
			s.restartUntilPlatformRecognizesStream(
				ctx,
				res,
				quirks.RestartUntilPlatformRecognizesStream,
			)
		})
	}

	return res, nil
}

func (s *StreamForwards) restartUntilPlatformRecognizesStream(
	ctx context.Context,
	fwd *StreamForward,
	cfg types.RestartUntilPlatformRecognizesStream,
) {
	ctx = belt.WithField(ctx, "module", "restartUntilPlatformRecognizesStream")
	ctx = belt.WithField(
		ctx,
		"stream_forward",
		fmt.Sprintf("%s->%s", fwd.StreamSourceID, fwd.StreamSinkID),
	)

	logger.Debugf(ctx, "restartUntilPlatformRecognizesStream(ctx, %#+v, %#+v)", fwd, cfg)
	defer func() { logger.Debugf(ctx, "restartUntilPlatformRecognizesStream(ctx, %#+v, %#+v)", fwd, cfg) }()

	if !cfg.Enabled {
		logger.Errorf(
			ctx,
			"an attempt to start restartUntilPlatformRecognizesStream when the hack is disabled for this stream forwarder: %#+v",
			cfg,
		)
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
		logger.Debugf(
			ctx,
			"waited %v, checking if the remote platform accepted the stream",
			cfg.StartTimeout,
		)

		for {
			sink, err := s.findStreamSinkByID(ctx, fwd.StreamSinkID)
			if err != nil {
				logger.Errorf(ctx, "unable to find sink '%s': %v", fwd.StreamSinkID, err)
				return
			}
			if sink.StreamID == nil {
				logger.Errorf(ctx, "StreamSourceID is nil in sink, cannot check stream status on platform")
				return
			}
			platID := sink.StreamID.PlatformID
			streamOK, err := s.PlatformsController.CheckStreamStartedByPlatformID(
				memoize.SetNoCache(ctx, true),
				platID,
			)
			logger.Debugf(
				ctx,
				"the result of checking the stream on the remote platform: %v %v",
				streamOK,
				err,
			)
			if err != nil {
				logger.Errorf(
					ctx,
					"unable to check if the stream with URL '%s' is started: %v",
					fwd.ActiveForwarding.StreamSinkURL,
					err,
				)
				time.Sleep(time.Second)
				continue
			}
			if streamOK {
				logger.Debugf(
					ctx,
					"waiting %v to recheck if the stream will be still OK",
					cfg.StopStartDelay,
				)
				select {
				case <-ctx.Done():
					return
				case <-time.After(cfg.StopStartDelay):
				}
				platID := sink.StreamID.PlatformID
				streamOK, err := s.PlatformsController.CheckStreamStartedByPlatformID(
					memoize.SetNoCache(ctx, true),
					platID,
				)
				logger.Debugf(
					ctx,
					"the result of checking the stream on the remote platform: %v %v",
					streamOK,
					err,
				)
				if err != nil {
					logger.Errorf(
						ctx,
						"unable to check if the stream with URL '%s' is started: %v",
						fwd.ActiveForwarding.StreamSinkURL,
						err,
					)
					time.Sleep(time.Second)
					continue
				}
				if streamOK {
					return
				}
			}
			break
		}

		logger.Infof(
			ctx,
			"the remote platform still does not see the stream, restarting the stream forwarding: stopping...",
		)

		err := fwd.ActiveForwarding.Stop()
		if err != nil {
			logger.Errorf(ctx, "unable to stop stream forwarding: %v", err)
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(cfg.StopStartDelay):
		}

		logger.Infof(
			ctx,
			"the remote platform still does not see the stream, restarting the stream forwarding: starting...",
		)

		err = fwd.ActiveForwarding.Start(ctx)
		if err != nil {
			logger.Errorf(ctx, "unable to start stream forwarding: %v", err)
		}
	}
}

func (s *StreamForwards) UpdateStreamForward(
	ctx context.Context,
	streamSourceID types.StreamSourceID,
	streamSinkID types.StreamSinkIDFullyQualified,
	enabled bool,
	encode types.EncodeConfig,
	quirks types.ForwardingQuirks,
) (_ret *StreamForward, _err error) {
	logger.Debugf(ctx, "UpdateStreamForward(ctx, '%s', '%s', %t, %#+v, %#+v)", streamSourceID, streamSinkID, enabled, encode, quirks)
	defer func() {
		logger.Debugf(ctx, "/UpdateStreamForward(ctx, '%s', '%s', %t, %#+v, %#+v): %#+v %v", streamSourceID, streamSinkID, enabled, encode, quirks, _ret, _err)
	}()
	return xsync.DoR2(ctx, &s.Mutex, func() (*StreamForward, error) {
		return s.updateStreamForward(ctx, streamSourceID, streamSinkID, enabled, encode, quirks)
	})
}

func (s *StreamForwards) updateStreamForward(
	ctx context.Context,
	streamSourceID types.StreamSourceID,
	streamSinkID types.StreamSinkIDFullyQualified,
	enabled bool,
	encode types.EncodeConfig,
	quirks types.ForwardingQuirks,
) (_ret *StreamForward, _err error) {
	s.WithConfig(ctx, func(ctx context.Context, cfg *types.Config) {
		streamConfig := cfg.Streams[streamSourceID]
		fwdCfg, ok := streamConfig.Forwardings[streamSinkID]
		if !ok {
			_err = fmt.Errorf("the forwarding %s->%s does not exist", streamSourceID, streamSinkID)
			return
		}

		var fwd *StreamForward
		if fwdCfg.Disabled && enabled {
			var err error
			fwd, err = s.newActiveStreamForward(ctx, streamSourceID, streamSinkID, encode, quirks)
			if err != nil {
				_err = fmt.Errorf("unable to active the stream: %w", err)
				return
			}
		}
		if !fwdCfg.Disabled && !enabled {
			err := s.removeActiveStreamForward(ctx, streamSourceID, streamSinkID)
			if err != nil {
				_err = fmt.Errorf("unable to deactivate the stream: %w", err)
				return
			}
		}
		streamConfig.Forwardings[streamSinkID] = types.ForwardingConfig{
			Disabled: !enabled,
			Encode:   encode,
			Quirks:   quirks,
		}

		r := &StreamForward{
			StreamSourceID: streamSourceID,
			StreamSinkID:   streamSinkID,
			Enabled:        enabled,
			Encode:         encode,
			Quirks:         quirks,
			NumBytesWrote:  0,
			NumBytesRead:   0,
		}
		if fwd != nil {
			r.ActiveForwarding = fwd.ActiveForwarding
		}
		_ret = r
	})
	return
}

func (s *StreamForwards) ListStreamForwards(
	ctx context.Context,
) (_ret []StreamForward, _err error) {
	defer func() {
		logger.Tracef(ctx, "/ListStreamForwards(): %#+v %v", _ret, _err)
	}()

	return xsync.DoR2(ctx, &s.Mutex, func() ([]StreamForward, error) {
		return s.getStreamForwards(
			ctx,
			func(si types.StreamSourceID, di *types.StreamSinkIDFullyQualified) bool {
				return true
			},
		)
	})
}

func (s *StreamForwards) getStreamForwards(
	ctx context.Context,
	filterFunc func(types.StreamSourceID, *types.StreamSinkIDFullyQualified) bool,
) (_ret []StreamForward, _err error) {
	activeStreamForwards, err := s.listActiveStreamForwards(ctx)
	if err != nil {
		return nil, fmt.Errorf("unable to get the list of active stream forwardings: %w", err)
	}
	logger.Tracef(ctx, "len(activeStreamForwards) == %d", len(activeStreamForwards))

	type fwdID struct {
		StreamSourceID types.StreamSourceID
		SinkID         types.StreamSinkIDFullyQualified
	}
	m := map[fwdID]*StreamForward{}
	for idx := range activeStreamForwards {
		fwd := &activeStreamForwards[idx]
		if !filterFunc(fwd.StreamSourceID, &fwd.StreamSinkID) {
			continue
		}
		m[fwdID{
			StreamSourceID: fwd.StreamSourceID,
			SinkID:         fwd.StreamSinkID,
		}] = fwd
	}

	var result []StreamForward
	s.WithConfig(ctx, func(ctx context.Context, cfg *types.Config) {
		logger.Tracef(ctx, "len(s.Config.Streams) == %d", len(cfg.Streams))
		for streamSourceID, stream := range cfg.Streams {
			if !filterFunc(streamSourceID, nil) {
				continue
			}
			logger.Tracef(
				ctx,
				"len(s.Config.Streams[%s].Forwardings) == %d",
				streamSourceID,
				len(stream.Forwardings),
			)
			for sinkID, cfg := range stream.Forwardings {
				sinkID := sinkID
				if !filterFunc(streamSourceID, &sinkID) {
					continue
				}
				logger.Tracef(ctx, "stream forwarding '%s->%s': %#+v", streamSourceID, sinkID, cfg)
				item := StreamForward{
					StreamSourceID: streamSourceID,
					StreamSinkID:   sinkID,
					Enabled:        !cfg.Disabled,
					Encode:         cfg.Encode,
					Quirks:         cfg.Quirks,
				}
				if activeFwd, ok := m[fwdID{
					StreamSourceID: streamSourceID,
					SinkID:         sinkID,
				}]; ok {
					item.NumBytesWrote = activeFwd.NumBytesWrote
					item.NumBytesRead = activeFwd.NumBytesRead
				}
				logger.Tracef(ctx, "stream forwarding '%s->%s': converted %#+v", streamSourceID, sinkID, item)
				result = append(result, item)
			}
		}
	})
	return result, nil
}

func (s *StreamForwards) listActiveStreamForwards(
	_ context.Context,
) ([]StreamForward, error) {
	var result []StreamForward
	for key, fwd := range s.ActiveStreamForwardings {
		result = append(result, StreamForward{
			StreamSourceID: key.StreamSourceID,
			StreamSinkID:   key.StreamSinkID,
			Enabled:        true,
			Encode:         fwd.Encode,
			NumBytesWrote:  fwd.WriteCount.Load(),
			NumBytesRead:   fwd.ReadCount.Load(),
		})
	}
	return result, nil
}

func (s *StreamForwards) RemoveStreamForward(
	ctx context.Context,
	streamSourceID types.StreamSourceID,
	streamSinkID types.StreamSinkIDFullyQualified,
) error {
	ctx = belt.WithField(ctx, "module", "StreamServer")
	return xsync.DoA3R1(ctx, &s.Mutex, s.removeStreamForward, ctx, streamSourceID, streamSinkID)
}

func (s *StreamForwards) removeStreamForward(
	ctx context.Context,
	streamSourceID types.StreamSourceID,
	streamSinkID types.StreamSinkIDFullyQualified,
) (err error) {
	s.WithConfig(ctx, func(ctx context.Context, cfg *types.Config) {
		streamCfg := cfg.Streams[streamSourceID]
		if _, ok := streamCfg.Forwardings[streamSinkID]; !ok {
			err = fmt.Errorf("the forwarding %s->%s does not exist", streamSourceID, streamSinkID)
			return
		}
		delete(streamCfg.Forwardings, streamSinkID)
		err = s.removeActiveStreamForward(ctx, streamSourceID, streamSinkID)
	})
	return
}

func (s *StreamForwards) removeActiveStreamForward(
	_ context.Context,
	streamSourceID types.StreamSourceID,
	streamSinkID types.StreamSinkIDFullyQualified,
) error {
	key := ForwardingKey{
		StreamSourceID: streamSourceID,
		StreamSinkID:   streamSinkID,
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

func (s *StreamForwards) GetStreamForwardsByStreamSink(
	ctx context.Context,
	streamSinkID types.StreamSinkIDFullyQualified,
) (_ret []StreamForward, _err error) {
	ctx = belt.WithField(ctx, "module", "StreamServer")
	logger.Debugf(ctx, "GetStreamForwardsByStreamSink()")
	defer func() {
		logger.Debugf(ctx, "/GetStreamForwardsByStreamSink(): %#+v %v", _ret, _err)
	}()

	return xsync.DoR2(ctx, &s.Mutex, func() ([]StreamForward, error) {
		return s.getStreamForwards(
			ctx,
			func(streamSourceID types.StreamSourceID, sinkID *types.StreamSinkIDFullyQualified) bool {
				return sinkID == nil || *sinkID == streamSinkID
			},
		)
	})
}

func (s *StreamForwards) ListStreamSinks(
	ctx context.Context,
) ([]types.StreamSink, error) {
	ctx = belt.WithField(ctx, "module", "StreamServer")
	return xsync.DoA1R2(ctx, &s.Mutex, s.listStreamSinks, ctx)
}

func (s *StreamForwards) listStreamSinks(
	ctx context.Context,
) ([]types.StreamSink, error) {
	c := make([]types.StreamSink, 0, len(s.StreamSinks))
	c = append(c, s.StreamSinks...)

	platformStreamIDs, err := s.PlatformsController.GetActiveStreamIDs(ctx)
	if err != nil {
		logger.Errorf(ctx, "unable to get active stream source IDs from platforms: %v", err)
	} else {
		for _, id := range platformStreamIDs {
			c = append(c, types.StreamSink{
				ID: types.NewStreamSinkIDFullyQualified(
					types.StreamSinkTypeExternalPlatform,
					types.StreamSinkID(id.String()),
				),
				Name:     id.String(),
				StreamID: id.Ptr(),
			})
		}
	}

	for _, src := range s.StreamServer.ListStreamSources(ctx) {
		c = append(c, types.StreamSink{
			ID: types.NewStreamSinkIDFullyQualified(
				types.StreamSinkTypeCustom,
				types.StreamSinkID(src.StreamSourceID),
			),
			Name: string(src.StreamSourceID),
		})
	}

	return c, nil
}

func (s *StreamForwards) AddStreamSink(
	ctx context.Context,
	streamSinkID types.StreamSinkIDFullyQualified,
	sink types.StreamSinkConfig,
) error {
	ctx = belt.WithField(ctx, "module", "StreamServer")
	return xsync.DoR1(ctx, &s.Mutex, func() error {
		return s.addStreamSink(ctx, streamSinkID, sink)
	})
}

func (s *StreamForwards) addStreamSink(
	ctx context.Context,
	streamSinkID types.StreamSinkIDFullyQualified,
	sink types.StreamSinkConfig,
) (_ret error) {
	s.WithConfig(ctx, func(ctx context.Context, cfg *types.Config) {
		err := s.addActiveStreamSink(ctx, streamSinkID, sink.URL, sink.StreamKey, sink.StreamSourceID)
		if err != nil {
			_ret = fmt.Errorf("unable to add an active stream sink: %w", err)
			return
		}
		if streamSinkID.Type == types.StreamSinkTypeCustom {
			cfg.StaticSinks[streamSinkID.ID] = &sink
		}
	})
	return
}

func (s *StreamForwards) UpdateStreamSink(
	ctx context.Context,
	streamSinkID types.StreamSinkIDFullyQualified,
	sink types.StreamSinkConfig,
) error {
	ctx = belt.WithField(ctx, "module", "StreamServer")
	return xsync.DoR1(ctx, &s.Mutex, func() error {
		return s.updateStreamSink(ctx, streamSinkID, sink)
	})
}

func (s *StreamForwards) updateStreamSink(
	ctx context.Context,
	streamSinkID types.StreamSinkIDFullyQualified,
	sink types.StreamSinkConfig,
) (_ret error) {
	s.WithConfig(ctx, func(ctx context.Context, cfg *types.Config) {
		for key := range s.ActiveStreamForwardings {
			if key.StreamSinkID == streamSinkID {
				_ret = fmt.Errorf(
					"there is already an active stream forwarding to '%s'",
					streamSinkID,
				)
				return
			}
		}

		err := s.removeActiveStreamSink(ctx, streamSinkID)
		if err != nil {
			_ret = fmt.Errorf(
				"unable to remove (to then re-add) the active stream sink: %w",
				err,
			)
			return
		}

		err = s.addActiveStreamSink(ctx, streamSinkID, sink.URL, sink.StreamKey, sink.StreamSourceID)
		if err != nil {
			_ret = fmt.Errorf("unable to re-add the active stream sink: %w", err)
			return
		}

		if streamSinkID.Type == types.StreamSinkTypeCustom {
			cfg.StaticSinks[streamSinkID.ID] = &sink
		}
	})
	return
}

// TODO: delete this function, we already store the exact same information in the config
func (s *StreamForwards) addActiveStreamSink(
	ctx context.Context,
	streamSinkID types.StreamSinkIDFullyQualified,
	url string,
	streamKey secret.String,
	streamSourceID *streamcontrol.StreamIDFullyQualified,
) error {
	logger.Debugf(ctx, "addActiveStreamSink(ctx, '%s', '%s', <secret>, %#+v)", streamSinkID, url, streamSourceID)
	s.StreamSinks = append(s.StreamSinks, types.StreamSink{
		ID:        streamSinkID,
		URL:       url,
		StreamKey: streamKey,
		StreamID:  streamSourceID,
	})
	return nil
}

func (s *StreamForwards) RemoveStreamSink(
	ctx context.Context,
	streamSinkID types.StreamSinkIDFullyQualified,
) error {
	ctx = belt.WithField(ctx, "module", "StreamServer")
	return xsync.DoA2R1(ctx, &s.Mutex, s.removeStreamSink, ctx, streamSinkID)
}

func (s *StreamForwards) removeStreamSink(
	ctx context.Context,
	streamSinkID types.StreamSinkIDFullyQualified,
) (err error) {
	s.WithConfig(ctx, func(ctx context.Context, cfg *types.Config) {
		for _, streamCfg := range cfg.Streams {
			delete(streamCfg.Forwardings, streamSinkID)
		}
		if streamSinkID.Type == types.StreamSinkTypeCustom {
			delete(cfg.StaticSinks, streamSinkID.ID)
		}
		err = s.removeActiveStreamSink(ctx, streamSinkID)
	})
	return
}

func (s *StreamForwards) removeActiveStreamSink(
	ctx context.Context,
	streamSinkID types.StreamSinkIDFullyQualified,
) error {
	streamForwards, err := s.getStreamForwards(
		ctx,
		func(si types.StreamSourceID, di *types.StreamSinkIDFullyQualified) bool {
			return true
		},
	)
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

	return fmt.Errorf("have not found stream sink with id %s", streamSinkID)
}

func (s *StreamForwards) findStreamSinkByID(
	ctx context.Context,
	streamSinkID types.StreamSinkIDFullyQualified,
) (_ret types.StreamSink, _err error) {
	logger.Debugf(ctx, "findStreamSinkByID: %q", streamSinkID)
	defer func() { logger.Debugf(ctx, "/findStreamSinkByID: %q: %#+v %v", streamSinkID, _ret, _err) }()
	sinks, err := s.listStreamSinks(ctx)
	if err != nil {
		return types.StreamSink{}, fmt.Errorf("unable to list stream sinks: %w", err)
	}
	for _, sink := range sinks {
		if sink.ID == streamSinkID {
			return sink, nil
		}
	}
	return types.StreamSink{}, fmt.Errorf(
		"unable to find a stream sink by StreamSinkID '%s'",
		streamSinkID,
	)
}
