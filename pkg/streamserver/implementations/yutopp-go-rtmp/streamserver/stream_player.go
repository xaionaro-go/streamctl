package streamserver

import (
	"context"
	"fmt"
	"strings"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/hashicorp/go-multierror"
	"github.com/xaionaro-go/streamctl/pkg/observability"
	"github.com/xaionaro-go/streamctl/pkg/player"
	"github.com/xaionaro-go/streamctl/pkg/streamd/api"
	"github.com/xaionaro-go/streamctl/pkg/streamplayer"
	sptypes "github.com/xaionaro-go/streamctl/pkg/streamplayer/types"
	sstypes "github.com/xaionaro-go/streamctl/pkg/streamserver/types"
	"github.com/xaionaro-go/streamctl/pkg/streamtypes"
	"github.com/xaionaro-go/streamctl/pkg/xcontext"
)

type StreamPlayerStreamServer struct {
	*StreamServer
}

var _ streamplayer.StreamServer = (*StreamPlayerStreamServer)(nil)

func NewStreamPlayerStreamServer(ss *StreamServer) *StreamPlayerStreamServer {
	return &StreamPlayerStreamServer{
		StreamServer: ss,
	}
}

func streamID2LocalAppName(
	streamID sstypes.StreamID,
) string {
	streamIDParts := strings.Split(string(streamID), "/")
	localAppName := string(streamID)
	if len(streamIDParts) == 2 {
		localAppName = streamIDParts[1]
	}
	return localAppName
}

func (s *StreamPlayerStreamServer) WaitPublisher(
	ctx context.Context,
	streamID sstypes.StreamID,
) (<-chan struct{}, error) {

	ch := make(chan struct{})
	observability.Go(ctx, func() {
		s.RelayService.WaitPubsub(ctx, streamID2LocalAppName(streamID))
		close(ch)
	})
	return ch, nil
}

func (s *StreamPlayerStreamServer) GetPortServers(
	ctx context.Context,
) ([]streamplayer.StreamPortServer, error) {
	srvs := s.ListServers(ctx)

	result := make([]streamplayer.StreamPortServer, 0, len(srvs))
	for _, srv := range srvs {
		result = append(result, streamplayer.StreamPortServer{
			Addr: srv.ListenAddr(),
			Type: srv.Type(),
		})
	}

	return result, nil
}

type setupStreamPlayersConfig struct {
	DefaultStreamPlayerOptions streamplayer.Options
}

type setupStreamPlayersOption interface {
	apply(*setupStreamPlayersConfig)
}

type setupStreamPlayersOptions []setupStreamPlayersOption

func (s setupStreamPlayersOptions) Config() setupStreamPlayersConfig {
	cfg := setupStreamPlayersConfig{}
	for _, opt := range s {
		opt.apply(&cfg)
	}
	return cfg
}

type setupStreamPlayersOptionDefaultStreamPlayerOptions streamplayer.Options

func (opt setupStreamPlayersOptionDefaultStreamPlayerOptions) apply(cfg *setupStreamPlayersConfig) {
	cfg.DefaultStreamPlayerOptions = (streamplayer.Options)(opt)
}

func (s *StreamServer) setupStreamPlayers(
	ctx context.Context,
	opts ...setupStreamPlayersOption,
) (_err error) {
	defer func() { logger.Debugf(ctx, "setupStreamPlayers result: %v", _err) }()

	setupCfg := setupStreamPlayersOptions(opts).Config()

	var streamIDsToDelete []api.StreamID

	curPlayers := map[api.StreamID]*streamplayer.StreamPlayer{}
	s.StreamPlayers.StreamPlayersLocker.Do(ctx, func() {
		for _, player := range s.StreamPlayers.StreamPlayers {
			streamCfg, ok := s.Config.Streams[sstypes.StreamID(player.StreamID)]
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

	for streamID, streamCfg := range s.Config.Streams {
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
			pubSub := s.RelayService.GetPubsub(streamID2LocalAppName(streamID))
			if pubSub == nil {
				return nil
			}
			return pubSub.PublisherHandler().ClosedChan()
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

type StreamPlayerConfig struct {
	DefaultStreamPlayerOptions streamplayer.Options
}

type StreamPlayerOption interface {
	apply(*StreamPlayerConfig)
}

type StreamPlayerOptions []StreamPlayerOption

func (s StreamPlayerOptions) Config() StreamPlayerConfig {
	cfg := StreamPlayerConfig{}
	for _, opt := range s {
		opt.apply(&cfg)
	}
	return cfg
}

type StreamPlayerOptionDefaultStreamPlayerOptions streamplayer.Options

func (opt StreamPlayerOptionDefaultStreamPlayerOptions) apply(cfg *StreamPlayerConfig) {
	cfg.DefaultStreamPlayerOptions = (streamplayer.Options)(opt)
}

func (s *StreamServer) AddStreamPlayer(
	ctx context.Context,
	streamID streamtypes.StreamID,
	playerType player.Backend,
	disabled bool,
	streamPlaybackConfig sptypes.Config,
	opts ...StreamPlayerOption,
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

func (s *StreamServer) setStreamPlayer(
	ctx context.Context,
	streamID streamtypes.StreamID,
	playerType player.Backend,
	disabled bool,
	streamPlaybackConfig sptypes.Config,
	opts ...StreamPlayerOption,
) (_err error) {
	defer func() { logger.Debugf(ctx, "setStreamPlayer result: %v", _err) }()

	cfg := StreamPlayerOptions(opts).Config()

	s.Config.Streams[streamID].Player = &sstypes.PlayerConfig{
		Player:         playerType,
		Disabled:       disabled,
		StreamPlayback: streamPlaybackConfig,
	}

	{
		var opts setupStreamPlayersOptions
		if cfg.DefaultStreamPlayerOptions != nil {
			opts = append(opts, setupStreamPlayersOptionDefaultStreamPlayerOptions(cfg.DefaultStreamPlayerOptions))
		}
		if err := s.setupStreamPlayers(ctx, opts...); err != nil {
			return fmt.Errorf("unable to setup the stream players: %w", err)
		}
	}

	return nil
}

func (s *StreamServer) UpdateStreamPlayer(
	ctx context.Context,
	streamID api.StreamID,
	playerType player.Backend,
	disabled bool,
	streamPlaybackConfig sptypes.Config,
	opts ...StreamPlayerOption,
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

func (s *StreamServer) RemoveStreamPlayer(
	ctx context.Context,
	streamID api.StreamID,
) error {
	if _, ok := s.Config.Streams[streamID]; !ok {
		return nil
	}
	s.Config.Streams[streamID].Player = nil
	return s.setupStreamPlayers(ctx)
}
