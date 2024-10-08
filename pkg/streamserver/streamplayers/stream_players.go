package streamplayers

import (
	"context"
	"fmt"
	"sort"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/hashicorp/go-multierror"
	"github.com/xaionaro-go/streamctl/pkg/observability"
	"github.com/xaionaro-go/streamctl/pkg/player"
	"github.com/xaionaro-go/streamctl/pkg/streamd/api"
	"github.com/xaionaro-go/streamctl/pkg/streamplayer"
	sptypes "github.com/xaionaro-go/streamctl/pkg/streamplayer/types"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/types"
	"github.com/xaionaro-go/streamctl/pkg/streamtypes"
	"github.com/xaionaro-go/streamctl/pkg/xcontext"
	"github.com/xaionaro-go/streamctl/pkg/xsync"
)

type StreamPlayers struct {
	xsync.Mutex
	StreamServer
	*streamplayer.StreamPlayers
}

type StreamServer interface {
	types.WithConfiger
	types.WaitPublisherChaner
}

func NewStreamPlayers(
	sp *streamplayer.StreamPlayers,
	streamServer StreamServer,
) *StreamPlayers {
	return &StreamPlayers{
		StreamPlayers: sp,
		StreamServer:  streamServer,
	}
}

func (s *StreamPlayers) Init(
	ctx context.Context,
	opts ...types.InitOption,
) error {
	initCfg := types.InitOptions(opts).Config()
	s.WithConfig(ctx, func(ctx context.Context, cfg *types.Config) {
		observability.Go(ctx, func() {
			var opts SetupOptions
			if initCfg.DefaultStreamPlayerOptions != nil {
				opts = append(
					opts,
					SetupOptionDefaultStreamPlayerOptions(
						initCfg.DefaultStreamPlayerOptions,
					),
				)
			}
			err := s.SetupStreamPlayers(ctx, opts...)
			if err != nil {
				logger.Error(ctx, err)
			}
		})
	})

	return nil
}

func copyMapUnref[K comparable, V any](in map[K]*V) map[K]*V {
	r := make(map[K]*V, len(in))
	for k, v := range in {
		r[k] = ptr(*v)
	}
	return r
}

func ptr[T any](in T) *T {
	return &in
}

type setupConfig struct {
	DefaultStreamPlayerOptions streamplayer.Options
}

type SetupOption interface {
	apply(*setupConfig)
}

type SetupOptions []SetupOption

func (s SetupOptions) Config() setupConfig {
	cfg := setupConfig{}
	for _, opt := range s {
		opt.apply(&cfg)
	}
	return cfg
}

type SetupOptionDefaultStreamPlayerOptions streamplayer.Options

func (opt SetupOptionDefaultStreamPlayerOptions) apply(cfg *setupConfig) {
	cfg.DefaultStreamPlayerOptions = (streamplayer.Options)(opt)
}

func (s *StreamPlayers) SetupStreamPlayers(
	ctx context.Context,
	opts ...SetupOption,
) (_err error) {
	defer func() { logger.Debugf(ctx, "setupStreamPlayers result: %v", _err) }()
	return xsync.DoR1(ctx, &s.Mutex, func() error {
		var streamCfg map[types.StreamID]*types.StreamConfig
		s.WithConfig(ctx, func(ctx context.Context, cfg *types.Config) {
			streamCfg = copyMapUnref(cfg.Streams)
		})
		return setupStreamPlayers(ctx, s, streamCfg, opts...)
	})
}

func setupStreamPlayers(
	ctx context.Context,
	s *StreamPlayers,
	streamCfg map[types.StreamID]*types.StreamConfig,
	opts ...SetupOption,
) error {
	setupCfg := SetupOptions(opts).Config()

	var streamIDsToDelete []types.StreamID

	curPlayers := map[types.StreamID]*streamplayer.StreamPlayerHandler{}
	s.StreamPlayers.StreamPlayersLocker.Do(ctx, func() {
		for _, player := range s.StreamPlayers.StreamPlayers {
			streamCfg, ok := streamCfg[types.StreamID(player.StreamID)]
			if !ok || streamCfg.Player == nil || streamCfg.Player.Disabled {
				streamIDsToDelete = append(streamIDsToDelete, player.StreamID)
				continue
			}
			curPlayers[player.StreamID] = player
		}
	})

	var result *multierror.Error

	logger.Debugf(ctx, "streamIDsToDelete == %#+v", streamIDsToDelete)

	for _, streamID := range streamIDsToDelete {
		err := s.StreamPlayers.Remove(ctx, streamID)
		if err != nil {
			err = fmt.Errorf("unable to remove stream '%s': %w", streamID, err)
			logger.Warnf(ctx, "%s", err)
			result = multierror.Append(result, err)
		} else {
			logger.Infof(ctx, "stopped the player for stream '%s'", streamID)
		}
	}

	for streamID, streamCfg := range streamCfg {
		playerCfg := streamCfg.Player
		if playerCfg == nil || playerCfg.Disabled {
			continue
		}

		if _, ok := curPlayers[streamID]; ok {
			continue
		}

		ssOpts := playerCfg.StreamPlayback.Options()
		if setupCfg.DefaultStreamPlayerOptions != nil {
			ssOpts = append(ssOpts, setupCfg.DefaultStreamPlayerOptions...)
		}
		ssOpts = append(ssOpts, sptypes.OptionGetRestartChanFunc(func() <-chan struct{} {
			pubCh, err := s.WaitPublisherChan(ctx, streamID)
			if err != nil {
				logger.Errorf(ctx, "unable to get a WaitPublisherChan: %v", err)
				return nil
			}

			ch := make(chan struct{})
			observability.Go(ctx, func() {
				select {
				case <-ctx.Done():
					return
				case <-pubCh:
					close(ch)
				}
			})
			return ch
		}))
		_, err := s.StreamPlayers.Create(
			xcontext.DetachDone(ctx),
			streamID,
			ssOpts...,
		)
		if err != nil {
			err = fmt.Errorf("unable to create a stream player for stream '%s': %w", streamID, err)
			logger.Warnf(ctx, "%s", err)
			result = multierror.Append(result, err)
			continue
		} else {
			logger.Infof(ctx, "started a player for stream '%s'", streamID)
		}
	}

	return result.ErrorOrNil()
}

func (s *StreamPlayers) GetStreamPlayer(
	ctx context.Context,
	streamID types.StreamID,
) (*types.StreamPlayer, error) {
	var (
		streamCfg *types.StreamConfig
		ok        bool
	)
	s.WithConfig(ctx, func(ctx context.Context, cfg *types.Config) {
		streamCfg, ok = cfg.Streams[streamID]
		streamCfg = copyAndPtr(streamCfg)
	})
	if !ok {
		return nil, fmt.Errorf("no stream '%s'", streamID)
	}
	playerCfg := streamCfg.Player
	if playerCfg == nil {
		return nil, fmt.Errorf("no stream player defined for '%s'", streamID)
	}
	return &api.StreamPlayer{
		StreamID:             streamID,
		PlayerType:           playerCfg.Player,
		Disabled:             playerCfg.Disabled,
		StreamPlaybackConfig: playerCfg.StreamPlayback,
	}, nil
}

func copyAndPtr[T any](in *T) *T {
	if in == nil {
		return nil
	}
	v := *in
	return &v
}

func (s *StreamPlayers) ListStreamPlayers(
	ctx context.Context,
) ([]types.StreamPlayer, error) {
	var result []api.StreamPlayer
	if s == nil {
		return nil, fmt.Errorf("StreamServer == nil")
	}

	var err error
	s.WithConfig(ctx, func(ctx context.Context, cfg *types.Config) {
		if cfg == nil {
			err = fmt.Errorf("s.Config == nil")
			return
		}
		if cfg.Streams == nil {
			err = fmt.Errorf("s.Config.Streams == nil")
			return
		}
		for streamID, streamCfg := range cfg.Streams {
			playerCfg := streamCfg.Player
			if playerCfg == nil {
				continue
			}

			result = append(result, api.StreamPlayer{
				StreamID:             streamID,
				PlayerType:           playerCfg.Player,
				Disabled:             playerCfg.Disabled,
				StreamPlaybackConfig: playerCfg.StreamPlayback,
			})
		}
		sort.Slice(result, func(i, j int) bool {
			return result[i].StreamID < result[j].StreamID
		})
	})
	return result, err
}

func (s *StreamPlayers) AddStreamPlayer(
	ctx context.Context,
	streamID types.StreamID,
	playerType player.Backend,
	disabled bool,
	streamPlaybackConfig sptypes.Config,
	opts ...types.StreamPlayerOption,
) error {
	return s.setStreamPlayer(
		ctx,
		streamID,
		playerType,
		disabled,
		streamPlaybackConfig,
		opts...,
	)
}

func (s *StreamPlayers) GetActiveStreamPlayer(
	ctx context.Context,
	streamID types.StreamID,
) (player.Player, error) {
	p := s.StreamPlayers.Get(streamID)
	if p == nil {
		return nil, fmt.Errorf("there is no player setup for '%s'", streamID)
	}
	return p.Player, nil
}

func (s *StreamPlayers) RemoveStreamPlayer(
	ctx context.Context,
	streamID types.StreamID,
) error {
	var err error
	s.WithConfig(ctx, func(ctx context.Context, cfg *types.Config) {
		if _, ok := cfg.Streams[streamID]; !ok {
			return
		}
		cfg.Streams[streamID].Player = nil

		err = setupStreamPlayers(ctx, s, copyMapUnref(cfg.Streams))
	})
	return err
}

func (s *StreamPlayers) UpdateStreamPlayer(
	ctx context.Context,
	streamID types.StreamID,
	playerType player.Backend,
	disabled bool,
	streamPlaybackConfig sptypes.Config,
	opts ...types.StreamPlayerOption,
) error {
	return s.setStreamPlayer(
		ctx,
		streamID,
		playerType,
		disabled,
		streamPlaybackConfig,
		opts...,
	)
}

func (s *StreamPlayers) setStreamPlayer(
	ctx context.Context,
	streamID streamtypes.StreamID,
	playerType player.Backend,
	disabled bool,
	streamPlaybackConfig sptypes.Config,
	opts ...types.StreamPlayerOption,
) (_err error) {
	defer func() { logger.Debugf(ctx, "setStreamPlayer result: %v", _err) }()

	playerCfg := types.StreamPlayerOptions(opts).Config()

	var err error
	s.WithConfig(ctx, func(ctx context.Context, cfg *types.Config) {
		cfg.Streams[streamID].Player = &types.PlayerConfig{
			Player:         playerType,
			Disabled:       disabled,
			StreamPlayback: streamPlaybackConfig,
		}

		var opts SetupOptions
		if playerCfg.DefaultStreamPlayerOptions != nil {
			opts = append(opts, SetupOptionDefaultStreamPlayerOptions(playerCfg.DefaultStreamPlayerOptions))
		}

		var streamCfg map[types.StreamID]*types.StreamConfig
		s.WithConfig(ctx, func(ctx context.Context, cfg *types.Config) {
			streamCfg = copyMapUnref(cfg.Streams)
		})
		err = setupStreamPlayers(ctx, s, streamCfg, opts...)
		if err != nil {
			err = fmt.Errorf("unable to setup the stream players: %w", err)
			return
		}
	})

	return nil
}
