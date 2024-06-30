package api

import (
	"context"

	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamd/cache"
	"github.com/xaionaro-go/streamctl/pkg/streamd/config"
)

type StreamD interface {
	Run(ctx context.Context) error
	FetchConfig(ctx context.Context) error
	ResetCache(ctx context.Context) error
	InitCache(ctx context.Context) error
	SaveConfig(ctx context.Context) error
	GetConfig(ctx context.Context) (*config.Config, error)
	SetConfig(ctx context.Context, cfg *config.Config) error
	IsBackendEnabled(ctx context.Context, id streamcontrol.PlatformName) (bool, error)
	OBSOLETE_IsGITInitialized(ctx context.Context) (bool, error)
	StartStream(
		ctx context.Context,
		platID streamcontrol.PlatformName,
		title string, description string,
		profile streamcontrol.AbstractStreamProfile,
		customArgs ...any,
	) error
	EndStream(ctx context.Context, platID streamcontrol.PlatformName) error
	OBSOLETE_GitRelogin(ctx context.Context) error
	GetBackendData(ctx context.Context, platID streamcontrol.PlatformName) (any, error)
	Restart(ctx context.Context) error
	EXPERIMENTAL_ReinitStreamControllers(ctx context.Context) error
	GetStreamStatus(
		ctx context.Context,
		platID streamcontrol.PlatformName,
	) (*streamcontrol.StreamStatus, error)
}

type BackendDataOBS struct{}

type BackendDataTwitch struct {
	Cache cache.Twitch
}

type BackendDataYouTube struct {
	Cache cache.YouTube
}
