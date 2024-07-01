package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/goccy/go-yaml"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/obs"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/twitch"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/youtube"
	"github.com/xaionaro-go/streamctl/pkg/streamd/api"
	"github.com/xaionaro-go/streamctl/pkg/streamd/config"
	"github.com/xaionaro-go/streamctl/pkg/streamd/grpc/go/streamd_grpc"
)

type GRPCServer struct {
	streamd_grpc.StreamDServer
	StreamD api.StreamD
}

var _ streamd_grpc.StreamDServer = (*GRPCServer)(nil)

func NewGRPCServer(streamd api.StreamD) *GRPCServer {
	return &GRPCServer{
		StreamD: streamd,
	}
}

func (grpc *GRPCServer) GetConfig(
	ctx context.Context,
	req *streamd_grpc.GetConfigRequest,
) (*streamd_grpc.GetConfigReply, error) {
	cfg, err := grpc.StreamD.GetConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("unable to get the config: %w", err)
	}
	var buf bytes.Buffer
	err = config.WriteConfig(ctx, &buf, *cfg)
	if err != nil {
		return nil, fmt.Errorf("unable to serialize the config: %w", err)
	}
	return &streamd_grpc.GetConfigReply{
		Config: buf.String(),
	}, nil
}

func (grpc *GRPCServer) SetConfig(
	ctx context.Context,
	req *streamd_grpc.SetConfigRequest,
) (*streamd_grpc.SetConfigReply, error) {
	logger.Debugf(ctx, "received SetConfig: %s", req.Config)

	var result config.Config
	err := config.ReadConfig(ctx, []byte(req.Config), &result)
	if err != nil {
		return nil, fmt.Errorf("unable to unserialize the config: %w", err)
	}

	err = grpc.StreamD.SetConfig(ctx, &result)
	if err != nil {
		return nil, fmt.Errorf("unable to set the config: %w", err)
	}

	return &streamd_grpc.SetConfigReply{}, nil
}

func (grpc *GRPCServer) SaveConfig(
	ctx context.Context,
	req *streamd_grpc.SaveConfigRequest,
) (*streamd_grpc.SaveConfigReply, error) {
	err := grpc.StreamD.SaveConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("unable to save the config: %w", err)
	}
	return &streamd_grpc.SaveConfigReply{}, nil
}

func (grpc *GRPCServer) ResetCache(
	ctx context.Context,
	req *streamd_grpc.ResetCacheRequest,
) (*streamd_grpc.ResetCacheReply, error) {
	err := grpc.StreamD.ResetCache(ctx)
	if err != nil {
		return nil, fmt.Errorf("unable to reset the cache: %w", err)
	}
	return &streamd_grpc.ResetCacheReply{}, nil
}

func (grpc *GRPCServer) InitCache(
	ctx context.Context,
	req *streamd_grpc.InitCacheRequest,
) (*streamd_grpc.InitCacheReply, error) {
	err := grpc.StreamD.InitCache(ctx)
	if err != nil {
		return nil, fmt.Errorf("unable to init the cache: %w", err)
	}
	return &streamd_grpc.InitCacheReply{}, nil
}

func (grpc *GRPCServer) StartStream(
	ctx context.Context,
	req *streamd_grpc.StartStreamRequest,
) (*streamd_grpc.StartStreamReply, error) {
	logger.Debugf(ctx, "grpc:StartStream: raw profile: %#+v", req.Profile)

	var profile streamcontrol.AbstractStreamProfile
	var err error
	platID := streamcontrol.PlatformName(req.GetPlatID())
	switch platID {
	case obs.ID:
		profile = &obs.StreamProfile{}
	case twitch.ID:
		profile = &twitch.StreamProfile{}
	case youtube.ID:
		profile = &youtube.StreamProfile{}
	default:
		return nil, fmt.Errorf("unexpected platform ID: '%s'", platID)
	}
	err = yaml.Unmarshal([]byte(req.GetProfile()), profile)
	if err != nil {
		return nil, fmt.Errorf("unable to unserialize the profile: %w", err)
	}

	logger.Debugf(ctx, "grpc:StartStream: parsed: %#+v", profile)

	err = grpc.StreamD.StartStream(
		ctx,
		platID,
		req.GetTitle(),
		req.GetDescription(),
		profile,
	)
	if err != nil {
		return nil, fmt.Errorf("unable to start the stream: %w", err)
	}

	return &streamd_grpc.StartStreamReply{}, nil
}

func (grpc *GRPCServer) EndStream(
	ctx context.Context,
	req *streamd_grpc.EndStreamRequest,
) (*streamd_grpc.EndStreamReply, error) {
	err := grpc.StreamD.EndStream(ctx, streamcontrol.PlatformName(req.GetPlatID()))
	if err != nil {
		return nil, fmt.Errorf("unable to end the stream: %w", err)
	}
	return &streamd_grpc.EndStreamReply{}, nil
}

func (grpc *GRPCServer) GetBackendInfo(
	ctx context.Context,
	req *streamd_grpc.GetBackendInfoRequest,
) (*streamd_grpc.GetBackendInfoReply, error) {
	platID := streamcontrol.PlatformName(req.GetPlatID())
	isEnabled, err := grpc.StreamD.IsBackendEnabled(ctx, platID)
	if err != nil {
		return nil, fmt.Errorf("unable to check if the backend is enabled: %w", err)
	}
	data, err := grpc.StreamD.GetBackendData(ctx, platID)
	if err != nil {
		return nil, fmt.Errorf("unable to get the backend info: %w", err)
	}
	dataSerialized, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("unable to serialize the backend info: %w", err)
	}

	return &streamd_grpc.GetBackendInfoReply{
		IsInitialized: isEnabled,
		Data:          string(dataSerialized),
	}, nil
}

func (grpc *GRPCServer) Restart(
	ctx context.Context,
	req *streamd_grpc.RestartRequest,
) (*streamd_grpc.RestartReply, error) {
	err := grpc.StreamD.Restart(ctx)
	if err != nil {
		return nil, fmt.Errorf("unable to restart: %w", err)
	}
	return &streamd_grpc.RestartReply{}, nil
}

func (grpc *GRPCServer) OBSOLETE_FetchConfig(
	ctx context.Context,
	req *streamd_grpc.OBSOLETE_FetchConfigRequest,
) (*streamd_grpc.OBSOLETE_FetchConfigReply, error) {
	err := grpc.StreamD.FetchConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("unable to fetch the config: %w", err)
	}
	return &streamd_grpc.OBSOLETE_FetchConfigReply{}, nil
}

func (grpc *GRPCServer) OBSOLETE_GitInfo(
	ctx context.Context,
	req *streamd_grpc.OBSOLETE_GetGitInfoRequest,
) (*streamd_grpc.OBSOLETE_GetGitInfoReply, error) {
	isEnabled, err := grpc.StreamD.OBSOLETE_IsGITInitialized(ctx)
	if err != nil {
		return nil, fmt.Errorf("unable to get the git info: %w", err)
	}
	return &streamd_grpc.OBSOLETE_GetGitInfoReply{
		IsInitialized: isEnabled,
	}, nil
}

func (grpc *GRPCServer) OBSOLETE_GitRelogin(
	ctx context.Context,
	req *streamd_grpc.OBSOLETE_GitReloginRequest,
) (*streamd_grpc.OBSOLETE_GitReloginReply, error) {
	err := grpc.StreamD.OBSOLETE_GitRelogin(ctx)
	if err != nil {
		return nil, fmt.Errorf("unable to relogin: %w", err)
	}
	return &streamd_grpc.OBSOLETE_GitReloginReply{}, nil
}

func (grpc *GRPCServer) GetStreamStatus(
	ctx context.Context,
	req *streamd_grpc.GetStreamStatusRequest,
) (*streamd_grpc.GetStreamStatusReply, error) {
	platID := streamcontrol.PlatformName(req.GetPlatID())
	streamStatus, err := grpc.StreamD.GetStreamStatus(ctx, platID)
	if err != nil {
		return nil, fmt.Errorf("unable to relogin: %w", err)
	}

	var startedAt *int64
	if streamStatus.StartedAt != nil {
		startedAt = ptr(streamStatus.StartedAt.UnixNano())
	}

	var customData string
	if streamStatus.CustomData != nil {
		customDataSerialized, err := json.Marshal(streamStatus.CustomData)
		if err != nil {
			return nil, fmt.Errorf("unable to serialize the custom data: %w", err)
		}
		customData = string(customDataSerialized)
	}

	return &streamd_grpc.GetStreamStatusReply{
		IsActive:   streamStatus.IsActive,
		StartedAt:  startedAt,
		CustomData: customData,
	}, nil
}
