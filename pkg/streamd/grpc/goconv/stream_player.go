package goconv

import (
	"github.com/xaionaro-go/streamctl/pkg/player"
	"github.com/xaionaro-go/streamctl/pkg/streamd/grpc/go/streamd_grpc"
	sptypes "github.com/xaionaro-go/streamctl/pkg/streamplayer/types"
)

func StreamPlayerTypeGo2GRPC(
	playerType player.Backend,
) streamd_grpc.PlayerType {
	switch playerType {
	case player.BackendLibVLC:
		return streamd_grpc.PlayerType_PlayerTypeLibVLC
	case player.BackendMPV:
		return streamd_grpc.PlayerType_PlayerTypeMPV
	default:
		return streamd_grpc.PlayerType_PlayerTypeAuto
	}
}

func StreamPlayerTypeGRPC2Go(
	playerType streamd_grpc.PlayerType,
) player.Backend {
	switch playerType {
	case streamd_grpc.PlayerType_PlayerTypeLibVLC:
		return player.BackendLibVLC
	case streamd_grpc.PlayerType_PlayerTypeMPV:
		return player.BackendMPV
	default:
		return player.BackendUndefined
	}
}

func StreamPlaybackConfigGo2GRPC(
	cfg *sptypes.Config,
) *streamd_grpc.StreamPlaybackConfig {
	return &streamd_grpc.StreamPlaybackConfig{
		JitterBufDurationSecs: cfg.JitterBufDuration.Seconds(),
		CatchupMaxSpeedFactor: cfg.CatchupMaxSpeedFactor,
		MaxCatchupAtLagSecs:   cfg.MaxCatchupAtLag.Seconds(),
		StartTimeoutSecs:      cfg.StartTimeout.Seconds(),
		ReadTimeoutSecs:       cfg.ReadTimeout.Seconds(),
	}
}

func StreamPlaybackConfigGRPC2Go(
	cfg *streamd_grpc.StreamPlaybackConfig,
) sptypes.Config {
	return sptypes.Config{
		JitterBufDuration:     secsGRPC2Go(cfg.JitterBufDurationSecs),
		CatchupMaxSpeedFactor: cfg.CatchupMaxSpeedFactor,
		MaxCatchupAtLag:       secsGRPC2Go(cfg.MaxCatchupAtLagSecs),
		StartTimeout:          secsGRPC2Go(cfg.StartTimeoutSecs),
		ReadTimeout:           secsGRPC2Go(cfg.ReadTimeoutSecs),
	}
}
