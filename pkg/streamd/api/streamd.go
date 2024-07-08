package api

import (
	"context"
	"crypto"

	"github.com/andreykaipov/goobs/api/requests/scenes"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamd/cache"
	"github.com/xaionaro-go/streamctl/pkg/streamd/config"
	"github.com/xaionaro-go/streamctl/pkg/streampanel/consts"
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
	UpdateStream(
		ctx context.Context,
		platID streamcontrol.PlatformName,
		title string, description string,
		profile streamcontrol.AbstractStreamProfile,
		customArgs ...any,
	) error
	SetTitle(ctx context.Context, platID streamcontrol.PlatformName, title string) error
	SetDescription(ctx context.Context, platID streamcontrol.PlatformName, description string) error
	ApplyProfile(ctx context.Context, platID streamcontrol.PlatformName, profile streamcontrol.AbstractStreamProfile, customArgs ...any) error
	OBSOLETE_GitRelogin(ctx context.Context) error
	GetBackendData(ctx context.Context, platID streamcontrol.PlatformName) (any, error)
	Restart(ctx context.Context) error
	EXPERIMENTAL_ReinitStreamControllers(ctx context.Context) error
	GetStreamStatus(
		ctx context.Context,
		platID streamcontrol.PlatformName,
	) (*streamcontrol.StreamStatus, error)
	GetVariable(ctx context.Context, key consts.VarKey) ([]byte, error)
	GetVariableHash(ctx context.Context, key consts.VarKey, hashType crypto.Hash) ([]byte, error)
	SetVariable(ctx context.Context, key consts.VarKey, value []byte) error

	OBSGetSceneList(
		ctx context.Context,
	) (*scenes.GetSceneListResponse, error)
	OBSSetCurrentProgramScene(
		ctx context.Context,
		req *scenes.SetCurrentProgramSceneParams,
	) error
}

type BackendDataOBS struct{}

type BackendDataTwitch struct {
	Cache cache.Twitch
}

type BackendDataYouTube struct {
	Cache cache.YouTube
}
