package streampanel

import (
	"context"
	"fmt"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/hashicorp/go-multierror"
	"github.com/xaionaro-go/streamctl/pkg/player"
	"github.com/xaionaro-go/streamctl/pkg/streamd/api"
	"github.com/xaionaro-go/streamctl/pkg/streampanel/config"
	"github.com/xaionaro-go/streamctl/pkg/streamplayer"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/types"
)

type StreamPlayerStreamServer struct {
	StreamD api.StreamD
}

var _ streamplayer.StreamServer = (*StreamPlayerStreamServer)(nil)

func NewStreamPlayerStreamServer(streamD api.StreamD) *StreamPlayerStreamServer {
	return &StreamPlayerStreamServer{
		StreamD: streamD,
	}
}

func (s *StreamPlayerStreamServer) GetPortServers(
	ctx context.Context,
) ([]streamplayer.StreamPortServer, error) {
	srvs, err := s.StreamD.ListStreamServers(ctx)
	if err != nil {
		return nil, fmt.Errorf("unable to get the list of stream servers: %w", err)
	}

	result := make([]streamplayer.StreamPortServer, 0, len(srvs))
	for _, srv := range srvs {
		result = append(result, streamplayer.StreamPortServer{
			Addr: srv.ListenAddr,
			Type: types.ServerType(srv.Type),
		})
	}

	return result, nil
}

func (s *StreamPlayerStreamServer) WaitPublisher(
	ctx context.Context,
	streamID api.StreamID,
) (<-chan struct{}, error) {
	return s.StreamD.WaitForStreamPublisher(ctx, streamID)
}

func (p *Panel) updateStreamPlayers(
	ctx context.Context,
) error {
	var streamIDsToDelete []api.StreamID
	p.StreamPlayers.StreamPlayersLocker.Lock()
	curPlayers := map[api.StreamID]*streamplayer.StreamPlayer{}
	for _, player := range p.StreamPlayers.StreamPlayers {
		if cfg, ok := p.Config.StreamPlayers[player.StreamID]; !ok || cfg.Disabled {
			streamIDsToDelete = append(streamIDsToDelete, player.StreamID)
			continue
		}
		curPlayers[player.StreamID] = player
	}
	p.StreamPlayers.StreamPlayersLocker.Unlock()

	var result *multierror.Error

	for _, streamID := range streamIDsToDelete {
		err := p.StreamPlayers.Remove(ctx, streamID)
		if err != nil {
			logger.Warnf(ctx, "unable to stop the player for stream '%s': %w", streamID, err)
			result = multierror.Append(result, fmt.Errorf("unable to remove stream '%s': %w", streamID, err))
		} else {
			logger.Infof(ctx, "stopped the player for stream '%s'", streamID)
		}
	}

	for streamID, player := range p.Config.StreamPlayers {
		if player.Disabled {
			continue
		}

		if _, ok := curPlayers[streamID]; ok {
			continue
		}

		_, err := p.StreamPlayers.Create(ctx, streamID)
		if err != nil {
			logger.Warnf(ctx, "unable to start the player for stream '%s': %w", streamID, err)
			result = multierror.Append(result, fmt.Errorf("unable to create a stream player for stream '%s': %w", streamID, err))
		} else {
			logger.Infof(ctx, "started a player for stream '%s'", streamID)
		}
	}

	return result.ErrorOrNil()
}

func (p *Panel) addStreamPlayer(
	ctx context.Context,
	streamID api.StreamID,
	playerType player.Backend,
	disabled bool,
) error {
	p.Config.StreamPlayers[streamID] = config.PlayerConfig{
		Player:   playerType,
		Disabled: disabled,
	}

	if err := p.SaveConfig(ctx); err != nil {
		return fmt.Errorf("unable to update the config: %w", err)
	}

	if err := p.updateStreamPlayers(ctx); err != nil {
		return fmt.Errorf("unable to setup the stream players: %w", err)
	}

	return nil
}

func (p *Panel) updateStreamPlayer(
	ctx context.Context,
	streamID api.StreamID,
	playerType player.Backend,
	disabled bool,
) error {
	return p.addStreamPlayer(ctx, streamID, playerType, disabled)
}

func (p *Panel) removeStreamPlayer(
	ctx context.Context,
	streamID api.StreamID,
) error {
	delete(p.Config.StreamPlayers, streamID)

	var result *multierror.Error
	if err := p.SaveConfig(ctx); err != nil {
		result = multierror.Append(result, fmt.Errorf("unable to update the config: %w", err))
	}

	if err := p.updateStreamPlayers(ctx); err != nil {
		result = multierror.Append(result, fmt.Errorf("unable to setup the stream players: %w", err))
	}

	return result.ErrorOrNil()
}
