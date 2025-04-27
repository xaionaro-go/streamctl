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
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/youtube"
	"github.com/xaionaro-go/streamctl/pkg/streamd/memoize"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/types"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/types/streamportserver"
	"github.com/xaionaro-go/streamctl/pkg/streamtypes"
	"github.com/xaionaro-go/typing/ordered"
	"github.com/xaionaro-go/xsync"
)

type ForwardingKey struct {
	StreamID      types.StreamID
	DestinationID types.DestinationID
}

type StreamServer interface {
	types.WithConfiger
	types.WaitPublisherChaner
	types.ActiveIncomingStreamIDser
	types.GetPortServerser
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
	streamID streamtypes.StreamID,
	waitForNext bool,
) (_ <-chan Publisher, _err error) {
	logger.Tracef(ctx, "WaitPublisherChan(ctx, '%s', %t)", streamID, waitForNext)
	defer func(){ logger.Tracef(ctx, "/WaitPublisherChan(ctx, '%s', %t): %v", streamID, waitForNext, _err) }()
}

func (srv *) ActiveIncomingStreamIDs() (_ret []StreamID, _err error) {
	ctx := context.TODO()
	logger.Tracef(ctx, "ActiveIncomingStreamIDs()")
	defer func(){ logger.Tracef(ctx, "/ActiveIncomingStreamIDs(): %v %v", _ret, _err) }()
}

func (srv *) GetPortServers(ctx context.Context) (_ret []Config, _err error) {
	logger.Tracef(ctx, "GetPortServers()")
	defer func(){ logger.Tracef(ctx, "/GetPortServers(): %v %v", _ret, _err) }()
}
*/

type StreamForwards struct {
	StreamServer
	types.PlatformsController
	Mutex                      xsync.Mutex
	DestinationStreamingLocker *lockmap.LockMap
	ActiveStreamForwardings    map[ForwardingKey]*ActiveStreamForwarding
	StreamDestinations         []types.StreamDestination
	RecoderFactory             func(context.Context) (recoder.Factory, error)
}

func NewStreamForwards(
	s StreamServer,
	recoderFactory func(ctx context.Context) (recoder.Factory, error),
	pc types.PlatformsController,
) *StreamForwards {
	return &StreamForwards{
		StreamServer:               s,
		RecoderFactory:             recoderFactory,
		PlatformsController:        pc,
		DestinationStreamingLocker: lockmap.NewLockMap(),
		ActiveStreamForwardings:    map[ForwardingKey]*ActiveStreamForwarding{},
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
		for dstID, dstCfg := range cfg.Destinations {
			err := s.addActiveStreamDestination(ctx, dstID, dstCfg.URL, dstCfg.StreamKey)
			if err != nil {
				_ret = fmt.Errorf(
					"unable to initialize stream destination '%s' to %#+v: %w",
					dstID,
					dstCfg,
					err,
				)
				return
			}
		}

		for streamID, streamCfg := range cfg.Streams {
			for dstID, fwd := range streamCfg.Forwardings {
				if fwd.Disabled {
					continue
				}
				_, err := s.newActiveStreamForward(ctx, streamID, dstID, fwd.Encode, fwd.Quirks)
				if err != nil {
					_ret = fmt.Errorf(
						"unable to launch stream forward from '%s' to '%s': %w",
						streamID,
						dstID,
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
	streamID types.StreamID,
	destinationID types.DestinationID,
	enabled bool,
	encode types.EncodeConfig,
	quirks types.ForwardingQuirks,
) (*StreamForward, error) {
	return xsync.DoR2(ctx, &s.Mutex, func() (*StreamForward, error) {
		return s.addStreamForward(ctx, streamID, destinationID, enabled, encode, quirks)
	})
}

func (s *StreamForwards) addStreamForward(
	ctx context.Context,
	streamID types.StreamID,
	destinationID types.DestinationID,
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
		streamConfig = cfg.Streams[streamID]
		if streamConfig == nil {
			err = fmt.Errorf("stream '%s' does not exist", streamID)
			return
		}

		if streamConfig.Forwardings == nil {
			streamConfig.Forwardings = make(map[streamtypes.DestinationID]types.ForwardingConfig)
		}

		if _, ok := streamConfig.Forwardings[destinationID]; ok {
			err = fmt.Errorf("the forwarding %s->%s already exists", streamID, destinationID)
			return
		}

		streamConfig.Forwardings[destinationID] = types.ForwardingConfig{
			Disabled: !enabled,
			Encode:   encode,
			Quirks:   quirks,
		}
	})
	if err != nil {
		return nil, err
	}

	if enabled {
		fwd, err := s.newActiveStreamForward(ctx, streamID, destinationID, encode, quirks)
		if err != nil {
			return nil, err
		}
		return fwd, nil
	}
	return &StreamForward{
		StreamID:      streamID,
		DestinationID: destinationID,
		Enabled:       enabled,
		Encode:        encode,
		Quirks:        quirks,
	}, nil
}

func (s *StreamForwards) getLocalhostURL(ctx context.Context, streamID types.StreamID) (*url.URL, error) {
	return streamportserver.GetURLForLocalStreamID(ctx, s.StreamServer, streamID)
}

func (s *StreamForwards) newActiveStreamForward(
	ctx context.Context,
	streamID types.StreamID,
	destinationID types.DestinationID,
	encode types.EncodeConfig,
	quirks types.ForwardingQuirks,
	opts ...Option,
) (*StreamForward, error) {
	ctx = belt.WithField(ctx, "stream_forward", fmt.Sprintf("%s->%s", streamID, destinationID))
	key := ForwardingKey{
		StreamID:      streamID,
		DestinationID: destinationID,
	}
	if _, ok := s.ActiveStreamForwardings[key]; ok {
		return nil, fmt.Errorf(
			"there is already an active stream forwarding to '%s'",
			destinationID,
		)
	}

	dst, err := s.findStreamDestinationByID(ctx, destinationID)
	if err != nil {
		return nil, fmt.Errorf("unable to find stream destination '%s': %w", destinationID, err)
	}

	urlParsed, err := url.Parse(dst.URL)
	if err != nil {
		return nil, fmt.Errorf("unable to parse URL '%s': %w", dst.URL, err)
	}

	if urlParsed.Host == "" {
		urlParsed, err = s.getLocalhostURL(ctx, types.StreamID(urlParsed.Path))
		if err != nil {
			return nil, fmt.Errorf("unable to get the URL of the output endpoint: %w", err)
		}
	}

	result := &StreamForward{
		StreamID:      streamID,
		DestinationID: destinationID,
		Enabled:       true,
		Encode:        encode,
		Quirks:        quirks,
		NumBytesWrote: 0,
		NumBytesRead:  0,
	}

	fwd, err := s.NewActiveStreamForward(
		ctx,
		streamID,
		urlParsed.String(),
		dst.StreamKey.Get(),
		encode,
		func(
			ctx context.Context,
			fwd *ActiveStreamForwarding,
		) {
			if quirks.StartAfterYoutubeRecognizedStream.Enabled {
				if quirks.RestartUntilYoutubeRecognizesStream.Enabled {
					logger.Errorf(
						ctx,
						"StartAfterYoutubeRecognizedStream should not be used together with RestartUntilYoutubeRecognizesStream",
					)
				} else {
					logger.Debugf(ctx, "fwd %s->%s is waiting for YouTube to recognize the stream", streamID, destinationID)
					started, err := s.PlatformsController.CheckStreamStartedByPlatformID(
						memoize.SetNoCache(ctx, true),
						youtube.ID,
					)
					logger.Debugf(ctx, "youtube status check: %v %v", started, err)
					if started {
						return
					}
					t := time.NewTicker(time.Second)
					for {
						select {
						case <-ctx.Done():
							return
						case <-t.C:
						}
						started, err := s.PlatformsController.CheckStreamStartedByPlatformID(
							ctx,
							youtube.ID,
						)
						logger.Debugf(ctx, "youtube status check: %v %v", started, err)
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
	s.ActiveStreamForwardings[key] = fwd
	result.ActiveForwarding = fwd

	if quirks.RestartUntilYoutubeRecognizesStream.Enabled {
		observability.Go(ctx, func() {
			s.restartUntilYoutubeRecognizesStream(
				ctx,
				result,
				quirks.RestartUntilYoutubeRecognizesStream,
			)
		})
	}

	return result, nil
}

func (s *StreamForwards) restartUntilYoutubeRecognizesStream(
	ctx context.Context,
	fwd *StreamForward,
	cfg types.RestartUntilYoutubeRecognizesStream,
) {
	ctx = belt.WithField(ctx, "module", "restartUntilYoutubeRecognizesStream")
	ctx = belt.WithField(
		ctx,
		"stream_forward",
		fmt.Sprintf("%s->%s", fwd.StreamID, fwd.DestinationID),
	)

	logger.Debugf(ctx, "restartUntilYoutubeRecognizesStream(ctx, %#+v, %#+v)", fwd, cfg)
	defer func() { logger.Debugf(ctx, "restartUntilYoutubeRecognizesStream(ctx, %#+v, %#+v)", fwd, cfg) }()

	if !cfg.Enabled {
		logger.Errorf(
			ctx,
			"an attempt to start restartUntilYoutubeRecognizesStream when the hack is disabled for this stream forwarder: %#+v",
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
			streamOK, err := s.PlatformsController.CheckStreamStartedByPlatformID(
				memoize.SetNoCache(ctx, true),
				youtube.ID,
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
					fwd.ActiveForwarding.DestinationURL,
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
				streamOK, err := s.PlatformsController.CheckStreamStartedByPlatformID(
					memoize.SetNoCache(ctx, true),
					youtube.ID,
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
						fwd.ActiveForwarding.DestinationURL,
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
	streamID types.StreamID,
	destinationID types.DestinationID,
	enabled bool,
	encode types.EncodeConfig,
	quirks types.ForwardingQuirks,
) (_ret *StreamForward, _err error) {
	logger.Debugf(ctx, "UpdateStreamForward(ctx, '%s', '%s', %t, %#+v, %#+v)", streamID, destinationID, enabled, encode, quirks)
	defer func() {
		logger.Debugf(ctx, "/UpdateStreamForward(ctx, '%s', '%s', %t, %#+v, %#+v): %#+v %v", streamID, destinationID, enabled, encode, quirks, _ret, _err)
	}()
	return xsync.DoR2(ctx, &s.Mutex, func() (*StreamForward, error) {
		return s.updateStreamForward(ctx, streamID, destinationID, enabled, encode, quirks)
	})
}

func (s *StreamForwards) updateStreamForward(
	ctx context.Context,
	streamID types.StreamID,
	destinationID types.DestinationID,
	enabled bool,
	encode types.EncodeConfig,
	quirks types.ForwardingQuirks,
) (_ret *StreamForward, _err error) {
	s.WithConfig(ctx, func(ctx context.Context, cfg *types.Config) {
		streamConfig := cfg.Streams[streamID]
		fwdCfg, ok := streamConfig.Forwardings[destinationID]
		if !ok {
			_err = fmt.Errorf("the forwarding %s->%s does not exist", streamID, destinationID)
			return
		}

		var fwd *StreamForward
		if fwdCfg.Disabled && enabled {
			var err error
			fwd, err = s.newActiveStreamForward(ctx, streamID, destinationID, encode, quirks)
			if err != nil {
				_err = fmt.Errorf("unable to active the stream: %w", err)
				return
			}
		}
		if !fwdCfg.Disabled && !enabled {
			err := s.removeActiveStreamForward(ctx, streamID, destinationID)
			if err != nil {
				_err = fmt.Errorf("unable to deactivate the stream: %w", err)
				return
			}
		}
		streamConfig.Forwardings[destinationID] = types.ForwardingConfig{
			Disabled: !enabled,
			Encode:   encode,
			Quirks:   quirks,
		}

		r := &StreamForward{
			StreamID:      streamID,
			DestinationID: destinationID,
			Enabled:       enabled,
			Encode:        encode,
			Quirks:        quirks,
			NumBytesWrote: 0,
			NumBytesRead:  0,
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
			func(si types.StreamID, di ordered.Optional[types.DestinationID]) bool {
				return true
			},
		)
	})
}

func (s *StreamForwards) getStreamForwards(
	ctx context.Context,
	filterFunc func(types.StreamID, ordered.Optional[types.DestinationID]) bool,
) (_ret []StreamForward, _err error) {
	activeStreamForwards, err := s.listActiveStreamForwards(ctx)
	if err != nil {
		return nil, fmt.Errorf("unable to get the list of active stream forwardings: %w", err)
	}
	logger.Tracef(ctx, "len(activeStreamForwards) == %d", len(activeStreamForwards))

	type fwdID struct {
		StreamID types.StreamID
		DestID   types.DestinationID
	}
	m := map[fwdID]*StreamForward{}
	for idx := range activeStreamForwards {
		fwd := &activeStreamForwards[idx]
		if !filterFunc(fwd.StreamID, ordered.Opt(fwd.DestinationID)) {
			continue
		}
		m[fwdID{
			StreamID: fwd.StreamID,
			DestID:   fwd.DestinationID,
		}] = fwd
	}

	var result []StreamForward
	s.WithConfig(ctx, func(ctx context.Context, cfg *types.Config) {
		logger.Tracef(ctx, "len(s.Config.Streams) == %d", len(cfg.Streams))
		for streamID, stream := range cfg.Streams {
			if !filterFunc(streamID, ordered.Optional[types.DestinationID]{}) {
				continue
			}
			logger.Tracef(
				ctx,
				"len(s.Config.Streams[%s].Forwardings) == %d",
				streamID,
				len(stream.Forwardings),
			)
			for dstID, cfg := range stream.Forwardings {
				if !filterFunc(streamID, ordered.Opt(dstID)) {
					continue
				}
				logger.Tracef(ctx, "stream forwarding '%s->%s': %#+v", streamID, dstID, cfg)
				item := StreamForward{
					StreamID:      streamID,
					DestinationID: dstID,
					Enabled:       !cfg.Disabled,
					Encode:        cfg.Encode,
					Quirks:        cfg.Quirks,
				}
				if activeFwd, ok := m[fwdID{
					StreamID: streamID,
					DestID:   dstID,
				}]; ok {
					item.NumBytesWrote = activeFwd.NumBytesWrote
					item.NumBytesRead = activeFwd.NumBytesRead
				}
				logger.Tracef(ctx, "stream forwarding '%s->%s': converted %#+v", streamID, dstID, item)
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
			StreamID:      key.StreamID,
			DestinationID: key.DestinationID,
			Enabled:       true,
			Encode:        fwd.Encode,
			NumBytesWrote: fwd.WriteCount.Load(),
			NumBytesRead:  fwd.ReadCount.Load(),
		})
	}
	return result, nil
}

func (s *StreamForwards) RemoveStreamForward(
	ctx context.Context,
	streamID types.StreamID,
	dstID types.DestinationID,
) error {
	ctx = belt.WithField(ctx, "module", "StreamServer")
	return xsync.DoA3R1(ctx, &s.Mutex, s.removeStreamForward, ctx, streamID, dstID)
}

func (s *StreamForwards) removeStreamForward(
	ctx context.Context,
	streamID types.StreamID,
	dstID types.DestinationID,
) (err error) {
	s.WithConfig(ctx, func(ctx context.Context, cfg *types.Config) {
		streamCfg := cfg.Streams[streamID]
		if _, ok := streamCfg.Forwardings[dstID]; !ok {
			err = fmt.Errorf("the forwarding %s->%s does not exist", streamID, dstID)
			return
		}
		delete(streamCfg.Forwardings, dstID)
		err = s.removeActiveStreamForward(ctx, streamID, dstID)
	})
	return
}

func (s *StreamForwards) removeActiveStreamForward(
	_ context.Context,
	streamID types.StreamID,
	dstID types.DestinationID,
) error {
	key := ForwardingKey{
		StreamID:      streamID,
		DestinationID: dstID,
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

func (s *StreamForwards) GetStreamForwardsByDestination(
	ctx context.Context,
	destID types.DestinationID,
) (_ret []StreamForward, _err error) {
	ctx = belt.WithField(ctx, "module", "StreamServer")
	logger.Debugf(ctx, "GetStreamForwardsByDestination()")
	defer func() {
		logger.Debugf(ctx, "/GetStreamForwardsByDestination(): %#+v %v", _ret, _err)
	}()

	return xsync.DoR2(ctx, &s.Mutex, func() ([]StreamForward, error) {
		return s.getStreamForwards(
			ctx,
			func(streamID types.StreamID, dstID ordered.Optional[types.DestinationID]) bool {
				return !dstID.IsSet() || dstID.Get() == destID
			},
		)
	})
}

func (s *StreamForwards) ListStreamDestinations(
	ctx context.Context,
) ([]types.StreamDestination, error) {
	ctx = belt.WithField(ctx, "module", "StreamServer")
	return xsync.DoA1R2(ctx, &s.Mutex, s.listStreamDestinations, ctx)
}

func (s *StreamForwards) listStreamDestinations(
	_ context.Context,
) ([]types.StreamDestination, error) {
	c := make([]types.StreamDestination, len(s.StreamDestinations))
	copy(c, s.StreamDestinations)
	return c, nil
}

func (s *StreamForwards) AddStreamDestination(
	ctx context.Context,
	destinationID types.DestinationID,
	url string,
	streamKey string,
) error {
	ctx = belt.WithField(ctx, "module", "StreamServer")
	return xsync.DoA4R1(ctx, &s.Mutex, s.addStreamDestination, ctx, destinationID, url, streamKey)
}

func (s *StreamForwards) addStreamDestination(
	ctx context.Context,
	destinationID types.DestinationID,
	url string,
	streamKey string,
) (_ret error) {
	s.WithConfig(ctx, func(ctx context.Context, cfg *types.Config) {
		err := s.addActiveStreamDestination(ctx, destinationID, url, streamKey)
		if err != nil {
			_ret = fmt.Errorf("unable to add an active stream destination: %w", err)
			return
		}
		cfg.Destinations[destinationID] = &types.DestinationConfig{
			URL:       url,
			StreamKey: streamKey,
		}
	})
	return
}

func (s *StreamForwards) UpdateStreamDestination(
	ctx context.Context,
	destinationID types.DestinationID,
	url string,
	streamKey string,
) error {
	ctx = belt.WithField(ctx, "module", "StreamServer")
	return xsync.DoA4R1(
		ctx,
		&s.Mutex,
		s.updateStreamDestination,
		ctx,
		destinationID,
		url,
		streamKey,
	)
}

func (s *StreamForwards) updateStreamDestination(
	ctx context.Context,
	destinationID types.DestinationID,
	url string,
	streamKey string,
) (_ret error) {
	s.WithConfig(ctx, func(ctx context.Context, cfg *types.Config) {
		for key := range s.ActiveStreamForwardings {
			if key.DestinationID == destinationID {
				_ret = fmt.Errorf(
					"there is already an active stream forwarding to '%s'",
					destinationID,
				)
				return
			}
		}

		err := s.removeActiveStreamDestination(ctx, destinationID)
		if err != nil {
			_ret = fmt.Errorf(
				"unable to remove (to then re-add) the active stream destination: %w",
				err,
			)
			return
		}

		err = s.addActiveStreamDestination(ctx, destinationID, url, streamKey)
		if err != nil {
			_ret = fmt.Errorf("unable to re-add the active stream destination: %w", err)
			return
		}

		cfg.Destinations[destinationID] = &types.DestinationConfig{
			URL:       url,
			StreamKey: streamKey,
		}
	})
	return
}

// TODO: delete this function, we already store the exact same information in the config
func (s *StreamForwards) addActiveStreamDestination(
	_ context.Context,
	destinationID types.DestinationID,
	url string,
	streamKey string,
) error {
	s.StreamDestinations = append(s.StreamDestinations, types.StreamDestination{
		ID:        destinationID,
		URL:       url,
		StreamKey: secret.New(streamKey),
	})
	return nil
}

func (s *StreamForwards) RemoveStreamDestination(
	ctx context.Context,
	destinationID types.DestinationID,
) error {
	ctx = belt.WithField(ctx, "module", "StreamServer")
	return xsync.DoA2R1(ctx, &s.Mutex, s.removeStreamDestination, ctx, destinationID)
}

func (s *StreamForwards) removeStreamDestination(
	ctx context.Context,
	destinationID types.DestinationID,
) (err error) {
	s.WithConfig(ctx, func(ctx context.Context, cfg *types.Config) {
		for _, streamCfg := range cfg.Streams {
			delete(streamCfg.Forwardings, destinationID)
		}
		delete(cfg.Destinations, destinationID)
		err = s.removeActiveStreamDestination(ctx, destinationID)
	})
	return
}

func (s *StreamForwards) removeActiveStreamDestination(
	ctx context.Context,
	destinationID types.DestinationID,
) error {
	streamForwards, err := s.getStreamForwards(
		ctx,
		func(si types.StreamID, di ordered.Optional[types.DestinationID]) bool {
			return true
		},
	)
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

func (s *StreamForwards) findStreamDestinationByID(
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
