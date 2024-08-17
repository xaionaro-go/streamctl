package streamserver

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/hashicorp/go-multierror"
	"github.com/xaionaro-go/streamctl/pkg/observability"
	"github.com/xaionaro-go/streamctl/pkg/player"
	"github.com/xaionaro-go/streamctl/pkg/streamd/api"
	"github.com/xaionaro-go/streamctl/pkg/streamplayer"
	sptypes "github.com/xaionaro-go/streamctl/pkg/streamplayer/types"
	sstypes "github.com/xaionaro-go/streamctl/pkg/streamserver/types"
	"github.com/xaionaro-go/streamctl/pkg/streamtypes"
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

func (s *StreamPlayerStreamServer) WaitPublisher(
	ctx context.Context,
	streamID sstypes.StreamID,
) (<-chan struct{}, error) {
	streamIDParts := strings.Split(string(streamID), "/")
	localAppName := string(streamID)
	if len(streamIDParts) == 2 {
		localAppName = streamIDParts[1]
	}

	ch := make(chan struct{})
	observability.Go(ctx, func() {
		s.RelayService.WaitPubsub(ctx, localAppName)
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
) error {
	setupCfg := setupStreamPlayersOptions(opts).Config()

	var streamIDsToDelete []api.StreamID

	logger.Tracef(ctx, "p.StreamPlayers.StreamPlayersLocker.Lock()-ing")
	s.StreamPlayers.StreamPlayersLocker.Lock()
	logger.Tracef(ctx, "p.StreamPlayers.StreamPlayersLocker.Lock()-ed")
	curPlayers := map[api.StreamID]*streamplayer.StreamPlayer{}
	for _, player := range s.StreamPlayers.StreamPlayers {
		streamCfg, ok := s.Config.Streams[sstypes.StreamID(player.StreamID)]
		if !ok || streamCfg.Player == nil || streamCfg.Player.Disabled {
			streamIDsToDelete = append(streamIDsToDelete, player.StreamID)
			continue
		}
		curPlayers[player.StreamID] = player
	}
	s.StreamPlayers.StreamPlayersLocker.Unlock()
	logger.Tracef(ctx, "p.StreamPlayers.StreamPlayersLocker.Unlock()-ed")

	var result *multierror.Error

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
		_, err := s.StreamPlayers.Create(
			detachDone(ctx),
			streamID,
			ssOpts...,
		)
		if err != nil {
			err = fmt.Errorf("unable to create a stream player for stream '%s': %w", streamID, err)
			logger.Warnf(ctx, "%s", err)
			result = multierror.Append(result, err)
		} else {
			logger.Infof(ctx, "started a player for stream '%s'", streamID)
		}
	}

	return result.ErrorOrNil()
}

func detachDone(ctx context.Context) context.Context {
	return ctxDetached{
		Context: ctx,
	}
}

type ctxDetached struct {
	context.Context
}

func (ctx ctxDetached) Deadline() (time.Time, bool) {
	return context.Background().Deadline()
}
func (ctx ctxDetached) Done() <-chan struct{} {
	return context.Background().Done()
}
func (ctx ctxDetached) Err() error {
	return context.Background().Err()
}

type AddStreamPlayerConfig struct {
	DefaultStreamPlayerOptions streamplayer.Options
}

type AddStreamPlayerOption interface {
	apply(*AddStreamPlayerConfig)
}

type AddStreamPlayerOptions []AddStreamPlayerOption

func (s AddStreamPlayerOptions) Config() AddStreamPlayerConfig {
	cfg := AddStreamPlayerConfig{}
	for _, opt := range s {
		opt.apply(&cfg)
	}
	return cfg
}

type AddStreamPlayerOptionDefaultStreamPlayerOptions streamplayer.Options

func (opt AddStreamPlayerOptionDefaultStreamPlayerOptions) apply(cfg *AddStreamPlayerConfig) {
	cfg.DefaultStreamPlayerOptions = (streamplayer.Options)(opt)
}

func (s *StreamServer) AddStreamPlayer(
	ctx context.Context,
	streamID streamtypes.StreamID,
	playerType player.Backend,
	disabled bool,
	streamPlaybackConfig sptypes.Config,
	opts ...AddStreamPlayerOption,
) error {
	cfg := AddStreamPlayerOptions(opts).Config()

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
) error {
	return s.AddStreamPlayer(ctx, streamID, playerType, disabled, streamPlaybackConfig)
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
