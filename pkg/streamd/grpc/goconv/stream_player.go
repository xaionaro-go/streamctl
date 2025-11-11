package goconv

import (
	"github.com/xaionaro-go/player/pkg/player"
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
		JitterBufDurationSecs: cfg.JitterBufMaxDuration.Seconds(),
		CatchupMaxSpeedFactor: cfg.CatchupMaxSpeedFactor,
		MaxCatchupAtLagSecs:   cfg.CatchupAtMaxLag.Seconds(),
		StartTimeoutSecs:      cfg.StartTimeout.Seconds(),
		ReadTimeoutSecs:       cfg.ReadTimeout.Seconds(),
		OverriddenURL:         cfg.OverrideURL,
		ForceWaitForPublisher: cfg.ForceWaitForPublisher,
		EnableObserver:        cfg.EnableObserver,
	}
}

func StreamPlaybackConfigGRPC2Go(
	cfg *streamd_grpc.StreamPlaybackConfig,
) sptypes.Config {
	return sptypes.Config{
		JitterBufMaxDuration:  secsGRPC2Go(cfg.JitterBufDurationSecs),
		CatchupMaxSpeedFactor: cfg.CatchupMaxSpeedFactor,
		CatchupAtMaxLag:       secsGRPC2Go(cfg.MaxCatchupAtLagSecs),
		StartTimeout:          secsGRPC2Go(cfg.StartTimeoutSecs),
		ReadTimeout:           secsGRPC2Go(cfg.ReadTimeoutSecs),
		OverrideURL:           cfg.GetOverriddenURL(),
		ForceWaitForPublisher: cfg.ForceWaitForPublisher,
		EnableObserver:        cfg.EnableObserver,
	}
}
