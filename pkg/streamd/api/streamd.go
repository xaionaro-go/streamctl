package api

import (
	"context"
	"crypto"
	"net"
	"time"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/obs-grpc-proxy/protobuf/go/obs_grpc"
	"github.com/xaionaro-go/streamctl/pkg/player"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamd/cache"
	"github.com/xaionaro-go/streamctl/pkg/streamd/config"
	"github.com/xaionaro-go/streamctl/pkg/streamd/config/action"
	"github.com/xaionaro-go/streamctl/pkg/streamd/config/event"
	"github.com/xaionaro-go/streamctl/pkg/streamd/grpc/go/streamd_grpc"
	"github.com/xaionaro-go/streamctl/pkg/streampanel/consts"
	sptypes "github.com/xaionaro-go/streamctl/pkg/streamplayer/types"
	sstypes "github.com/xaionaro-go/streamctl/pkg/streamserver/types"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/types/streamportserver"
	"github.com/xaionaro-go/streamctl/pkg/streamtypes"
)

type StreamD interface {
	Run(ctx context.Context) error
	SetLoggingLevel(ctx context.Context, level logger.Level) error
	GetLoggingLevel(ctx context.Context) (logger.Level, error)
	ResetCache(ctx context.Context) error
	InitCache(ctx context.Context) error
	SaveConfig(ctx context.Context) error
	GetConfig(ctx context.Context) (*config.Config, error)
	SetConfig(ctx context.Context, cfg *config.Config) error
	IsBackendEnabled(
		ctx context.Context,
		id streamcontrol.PlatformName,
	) (bool, error)
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
	SetTitle(
		ctx context.Context,
		platID streamcontrol.PlatformName,
		title string,
	) error
	SetDescription(
		ctx context.Context,
		platID streamcontrol.PlatformName,
		description string,
	) error
	ApplyProfile(
		ctx context.Context,
		platID streamcontrol.PlatformName,
		profile streamcontrol.AbstractStreamProfile,
		customArgs ...any,
	) error
	GetBackendData(
		ctx context.Context,
		platID streamcontrol.PlatformName,
	) (any, error)
	EXPERIMENTAL_ReinitStreamControllers(ctx context.Context) error
	GetStreamStatus(
		ctx context.Context,
		platID streamcontrol.PlatformName,
	) (*streamcontrol.StreamStatus, error)
	GetVariable(ctx context.Context, key consts.VarKey) ([]byte, error)
	GetVariableHash(
		ctx context.Context,
		key consts.VarKey,
		hashType crypto.Hash,
	) ([]byte, error)
	SetVariable(ctx context.Context, key consts.VarKey, value []byte) error

	OBS(ctx context.Context) (obs_grpc.OBSServer, context.CancelFunc, error)

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
		opts ...streamportserver.Option,
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
		streamKey string,
	) error
	UpdateStreamDestination(
		ctx context.Context,
		destinationID DestinationID,
		url string,
		streamKey string,
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
		quirks StreamForwardingQuirks,
	) error
	UpdateStreamForward(
		ctx context.Context,
		streamID StreamID,
		destinationID DestinationID,
		enabled bool,
		quirks StreamForwardingQuirks,
	) error
	RemoveStreamForward(
		ctx context.Context,
		streamID StreamID,
		destinationID DestinationID,
	) error
	WaitForStreamPublisher(
		ctx context.Context,
		streamID StreamID,
	) (<-chan struct{}, error)

	AddStreamPlayer(
		ctx context.Context,
		streamID streamtypes.StreamID,
		playerType player.Backend,
		disabled bool,
		streamPlaybackConfig sptypes.Config,
	) error
	UpdateStreamPlayer(
		ctx context.Context,
		streamID streamtypes.StreamID,
		playerType player.Backend,
		disabled bool,
		streamPlaybackConfig sptypes.Config,
	) error
	RemoveStreamPlayer(
		ctx context.Context,
		streamID streamtypes.StreamID,
	) error
	ListStreamPlayers(
		ctx context.Context,
	) ([]StreamPlayer, error)
	GetStreamPlayer(
		ctx context.Context,
		streamID streamtypes.StreamID,
	) (*StreamPlayer, error)

	StreamPlayerProcessTitle(
		ctx context.Context,
		streamID StreamID,
	) (string, error)
	StreamPlayerOpenURL(
		ctx context.Context,
		streamID StreamID,
		link string,
	) error
	StreamPlayerGetLink(ctx context.Context, streamID StreamID) (string, error)
	StreamPlayerEndChan(
		ctx context.Context,
		streamID StreamID,
	) (<-chan struct{}, error)
	StreamPlayerIsEnded(ctx context.Context, streamID StreamID) (bool, error)
	StreamPlayerGetPosition(
		ctx context.Context,
		streamID StreamID,
	) (time.Duration, error)
	StreamPlayerGetLength(
		ctx context.Context,
		streamID StreamID,
	) (time.Duration, error)
	StreamPlayerSetSpeed(
		ctx context.Context,
		streamID StreamID,
		speed float64,
	) error
	StreamPlayerSetPause(
		ctx context.Context,
		streamID StreamID,
		pause bool,
	) error
	StreamPlayerStop(ctx context.Context, streamID StreamID) error
	StreamPlayerClose(ctx context.Context, streamID StreamID) error

	SubscribeToConfigChanges(ctx context.Context) (<-chan DiffConfig, error)
	SubscribeToStreamsChanges(ctx context.Context) (<-chan DiffStreams, error)
	SubscribeToStreamServersChanges(
		ctx context.Context,
	) (<-chan DiffStreamServers, error)
	SubscribeToStreamDestinationsChanges(
		ctx context.Context,
	) (<-chan DiffStreamDestinations, error)
	SubscribeToIncomingStreamsChanges(
		ctx context.Context,
	) (<-chan DiffIncomingStreams, error)
	SubscribeToStreamForwardsChanges(
		ctx context.Context,
	) (<-chan DiffStreamForwards, error)
	SubscribeToStreamPlayersChanges(
		ctx context.Context,
	) (<-chan DiffStreamPlayers, error)

	AddTimer(
		ctx context.Context,
		triggerAt time.Time,
		action Action,
	) (TimerID, error)
	RemoveTimer(ctx context.Context, timerID TimerID) error
	ListTimers(ctx context.Context) ([]Timer, error)

	AddTriggerRule(
		ctx context.Context,
		triggerRule *config.TriggerRule,
	) (TriggerRuleID, error)
	UpdateTriggerRule(
		ctx context.Context,
		ruleID TriggerRuleID,
		triggerRule *config.TriggerRule,
	) error
	RemoveTriggerRule(
		ctx context.Context,
		ruleID TriggerRuleID,
	) error
	ListTriggerRules(
		ctx context.Context,
	) (TriggerRules, error)

	SubmitEvent(
		ctx context.Context,
		event event.Event,
	) error

	SubscribeToChatMessages(
		ctx context.Context,
	) (<-chan ChatMessage, error)
	SendChatMessage(
		ctx context.Context,
		platID streamcontrol.PlatformName,
		message string,
	) error
	RemoveChatMessage(
		ctx context.Context,
		platID streamcontrol.PlatformName,
		msgID streamcontrol.ChatMessageID,
	) error
	BanUser(
		ctx context.Context,
		platID streamcontrol.PlatformName,
		userID streamcontrol.ChatUserID,
		reason string,
		deadline time.Time,
	) error

	DialContext(
		ctx context.Context,
		network string,
		addr string,
	) (net.Conn, error)
}

type StreamPlayer = sstypes.StreamPlayer

type BackendDataOBS struct{}

type BackendDataTwitch struct {
	Cache cache.Twitch
}

type BackendDataKick struct {
}

type BackendDataYouTube struct {
	Cache cache.YouTube
}

type StreamServerType = streamtypes.ServerType

type StreamServer struct {
	streamportserver.Config

	NumBytesConsumerWrote uint64
	NumBytesProducerRead  uint64
}

type StreamDestination struct {
	ID        DestinationID
	URL       string
	StreamKey string
}

type StreamForward struct {
	Enabled       bool
	StreamID      StreamID
	DestinationID DestinationID
	Quirks        StreamForwardingQuirks
	NumBytesWrote uint64
	NumBytesRead  uint64
}

type IncomingStream struct {
	StreamID StreamID
}

type StreamID = streamtypes.StreamID
type DestinationID = streamtypes.DestinationID
type OBSInstanceID = streamtypes.OBSInstanceID

type StreamForwardingQuirks = sstypes.ForwardingQuirks

type RestartUntilYoutubeRecognizesStream = sstypes.RestartUntilYoutubeRecognizesStream

type StartAfterYoutubeRecognizedStream = sstypes.StartAfterYoutubeRecognizedStream

type DiffConfig struct{}
type DiffDashboard struct{}
type DiffStreams struct{}
type DiffStreamServers struct{}
type DiffStreamDestinations struct{}
type DiffIncomingStreams struct{}
type DiffStreamForwards struct{}
type DiffStreamPlayers struct{}

type TimerID uint64

type Timer struct {
	ID        TimerID
	TriggerAt time.Time
	Action    Action
}

type Action = action.Action

type TriggerRuleID uint64
type TriggerRule = config.TriggerRule
type TriggerRules = config.TriggerRules

type ChatMessage struct {
	streamcontrol.ChatMessage
	Platform streamcontrol.PlatformName
}
