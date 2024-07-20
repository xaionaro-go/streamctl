package api

import (
	"context"
	"crypto"
	"fmt"

	"github.com/andreykaipov/goobs/api/requests/scenes"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamd/cache"
	"github.com/xaionaro-go/streamctl/pkg/streamd/config"
	"github.com/xaionaro-go/streamctl/pkg/streamd/grpc/go/streamd_grpc"
	"github.com/xaionaro-go/streamctl/pkg/streampanel/consts"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/types"
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
	AddIncomingStream(
		ctx context.Context,
		streamID StreamID,
	) error
	RemoveIncomingStream(
		ctx context.Context,
		streamID StreamID,
	) error
	ListIncomingStreams(
		ctx context.Context,
	) ([]IncomingStream, error)
	ListStreamDestinations(
		ctx context.Context,
	) ([]StreamDestination, error)
	AddStreamDestination(
		ctx context.Context,
		destinationID DestinationID,
		url string,
	) error
	RemoveStreamDestination(
		ctx context.Context,
		destinationID DestinationID,
	) error
	ListStreamForwards(
		ctx context.Context,
	) ([]StreamForward, error)
	AddStreamForward(
		ctx context.Context,
		streamID StreamID,
		destinationID DestinationID,
		enabled bool,
	) error
	UpdateStreamForward(
		ctx context.Context,
		streamID StreamID,
		destinationID DestinationID,
		enabled bool,
	) error
	RemoveStreamForward(
		ctx context.Context,
		streamID StreamID,
		destinationID DestinationID,
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

func (t StreamServerType) String() string {
	switch t {
	case StreamServerTypeUndefined:
		return "<undefined>"
	case StreamServerTypeRTMP:
		return "rtmp"
	case StreamServerTypeRTSP:
		return "rtsp"
	default:
		return fmt.Sprintf("unknown_type_%d", t)
	}
}

func ParseStreamServerType(in string) StreamServerType {
	switch in {
	case "rtmp":
		return StreamServerTypeRTMP
	case "rtsp":
		return StreamServerTypeRTSP
	}
	return StreamServerTypeUndefined
}

type StreamServer struct {
	Type       StreamServerType
	ListenAddr string

	NumBytesConsumerWrote uint64
	NumBytesProducerRead  uint64
}

type StreamDestination struct {
	ID  DestinationID
	URL string
}

type StreamForward struct {
	Enabled       bool
	StreamID      StreamID
	DestinationID DestinationID
	NumBytesWrote uint64
	NumBytesRead  uint64
}

type IncomingStream struct {
	StreamID StreamID
}

type StreamID string

type DestinationID string

func ServerTypeServer2API(t types.ServerType) StreamServerType {
	switch t {
	case types.ServerTypeUndefined:
		return StreamServerTypeUndefined
	case types.ServerTypeRTSP:
		return StreamServerTypeRTSP
	case types.ServerTypeRTMP:
		return StreamServerTypeRTMP
	default:
		panic(fmt.Errorf("unexpected server type: %v", t))
	}
}

func ServerTypeAPI2Server(t StreamServerType) types.ServerType {
	switch t {
	case StreamServerTypeUndefined:
		return types.ServerTypeUndefined
	case StreamServerTypeRTSP:
		return types.ServerTypeRTSP
	case StreamServerTypeRTMP:
		return types.ServerTypeRTMP
	default:
		panic(fmt.Errorf("unexpected server type: %v", t))
	}
}
