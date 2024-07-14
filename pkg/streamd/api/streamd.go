package api

import (
	"context"
	"crypto"

	"github.com/andreykaipov/goobs/api/requests/scenes"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamd/cache"
	"github.com/xaionaro-go/streamctl/pkg/streamd/config"
	"github.com/xaionaro-go/streamctl/pkg/streamd/grpc/go/streamd_grpc"
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

	SubmitOAuthCode(
		context.Context,
		*streamd_grpc.SubmitOAuthCodeRequest,
	) (*streamd_grpc.SubmitOAuthCodeReply, error)

	ListStreamServers(
		ctx context.Context,
	) ([]StreamServer, error)
	StartStreamServer(
		ctx context.Context,
		serverType StreamServerType,
		listenAddr string,
	) error
	StopStreamServer(
		ctx context.Context,
		listenAddr string,
	) error
	ListIncomingStreams(
		ctx context.Context,
	) ([]IncomingStream, error)
	ListStreamDestinations(
		ctx context.Context,
	) ([]StreamDestination, error)
	AddStreamDestination(
		ctx context.Context,
		streamID StreamID,
		url string,
	) error
	RemoveStreamDestination(
		ctx context.Context,
		streamID StreamID,
	) error
	ListStreamForwards(
		ctx context.Context,
	) ([]StreamForward, error)
	AddStreamForward(
		ctx context.Context,
		streamIDSrc StreamID,
		streamIDDst StreamID,
	) error
	RemoveStreamForward(
		ctx context.Context,
		streamIDSrc StreamID,
		streamIDDst StreamID,
	) error
}

type BackendDataOBS struct{}

type BackendDataTwitch struct {
	Cache cache.Twitch
}

type BackendDataYouTube struct {
	Cache cache.YouTube
}

type StreamServerType int

const (
	StreamServerTypeUndefined = StreamServerType(iota)
	StreamServerTypeRTSP
	StreamServerTypeRTMP
)

type StreamServer struct {
	Type       StreamServerType
	ListenAddr string
}

type StreamDestination struct {
	StreamID StreamID
	URL      string
}

type StreamForward struct {
	StreamIDSrc StreamID
	StreamIDDst StreamID
}

type IncomingStream struct {
	StreamID StreamID
}

type StreamID string
