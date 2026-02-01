package api

import (
	"context"
	"crypto"
	"net"
	"time"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/obs-grpc-proxy/protobuf/go/obs_grpc"
	"github.com/xaionaro-go/player/pkg/player"
	p2ptypes "github.com/xaionaro-go/streamctl/pkg/p2p/types"
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
		platformID streamcontrol.PlatformID,
	) (bool, error)
	SetStreamActive(
		ctx context.Context,
		streamID streamcontrol.StreamIDFullyQualified,
		active bool,
	) error
	SetTitle(
		ctx context.Context,
		streamID streamcontrol.StreamIDFullyQualified,
		title string,
	) error
	SetDescription(
		ctx context.Context,
		streamID streamcontrol.StreamIDFullyQualified,
		description string,
	) error
	ApplyProfile(
		ctx context.Context,
		streamID streamcontrol.StreamIDFullyQualified,
		profile streamcontrol.StreamProfile,
	) error
	GetBackendInfo(
		ctx context.Context,
		platID streamcontrol.PlatformID,
		includeData bool,
	) (*BackendInfo, error)
	EXPERIMENTAL_ReinitStreamControllers(ctx context.Context) error
	GetStreamStatus(
		ctx context.Context,
		streamID streamcontrol.StreamIDFullyQualified,
	) (*streamcontrol.StreamStatus, error)
	WaitStreamStartedByStreamSourceID(
		ctx context.Context,
		streamID streamcontrol.StreamIDFullyQualified,
	) error
	GetPlatforms(ctx context.Context) []streamcontrol.PlatformID
	GetAccounts(
		ctx context.Context,
		platformID ...streamcontrol.PlatformID,
	) ([]streamcontrol.AccountIDFullyQualified, error)
	GetStreams(
		ctx context.Context,
		accountID ...streamcontrol.AccountIDFullyQualified,
	) ([]streamcontrol.StreamInfo, error)
	CreateStream(
		ctx context.Context,
		accountID streamcontrol.AccountIDFullyQualified,
		title string,
	) (streamcontrol.StreamInfo, error)
	DeleteStream(
		ctx context.Context,
		streamID streamcontrol.StreamIDFullyQualified,
	) error
	GetActiveStreamIDs(
		ctx context.Context,
	) ([]streamcontrol.StreamIDFullyQualified, error)
	GetStreamSinkConfig(
		ctx context.Context,
		streamID streamcontrol.StreamIDFullyQualified,
	) (sstypes.StreamSinkConfig, error)
	GetVariable(ctx context.Context, key consts.VarKey) (VariableValue, error)
	GetVariableHash(
		ctx context.Context,
		key consts.VarKey,
		hashType crypto.Hash,
	) ([]byte, error)
	SetVariable(ctx context.Context, key consts.VarKey, value VariableValue) error
	SubscribeToVariable(ctx context.Context, key consts.VarKey) (<-chan VariableValue, error)

	OBS(ctx context.Context, accountID streamcontrol.AccountID) (obs_grpc.OBSServer, context.CancelFunc, error)

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
	AddStreamSource(
		ctx context.Context,
		streamSourceID StreamSourceID,
	) error
	RemoveStreamSource(
		ctx context.Context,
		streamSourceID StreamSourceID,
	) error
	ListStreamSources(
		ctx context.Context,
	) ([]StreamSource, error)
	ListStreamSinks(
		ctx context.Context,
	) ([]StreamSink, error)
	AddStreamSink(
		ctx context.Context,
		streamSinkID StreamSinkIDFullyQualified,
		dst sstypes.StreamSinkConfig,
	) error
	UpdateStreamSink(
		ctx context.Context,
		streamSinkID StreamSinkIDFullyQualified,
		dst sstypes.StreamSinkConfig,
	) error
	RemoveStreamSink(
		ctx context.Context,
		streamSinkID StreamSinkIDFullyQualified,
	) error
	ListStreamForwards(
		ctx context.Context,
	) ([]StreamForward, error)
	AddStreamForward(
		ctx context.Context,
		streamSourceID StreamSourceID,
		streamSinkID StreamSinkIDFullyQualified,
		enabled bool,
		encode sstypes.EncodeConfig,
		quirks StreamForwardingQuirks,
	) error
	UpdateStreamForward(
		ctx context.Context,
		streamSourceID StreamSourceID,
		streamSinkID StreamSinkIDFullyQualified,
		enabled bool,
		encode sstypes.EncodeConfig,
		quirks StreamForwardingQuirks,
	) error
	RemoveStreamForward(
		ctx context.Context,
		streamSourceID StreamSourceID,
		streamSinkID StreamSinkIDFullyQualified,
	) error
	WaitForStreamPublisher(
		ctx context.Context,
		streamSourceID StreamSourceID,
		waitForNext bool,
	) (<-chan struct{}, error)

	AddStreamPlayer(
		ctx context.Context,
		streamSourceID streamtypes.StreamSourceID,
		playerType player.Backend,
		disabled bool,
		streamPlaybackConfig sptypes.Config,
	) error
	UpdateStreamPlayer(
		ctx context.Context,
		streamSourceID streamtypes.StreamSourceID,
		playerType player.Backend,
		disabled bool,
		streamPlaybackConfig sptypes.Config,
	) error
	RemoveStreamPlayer(
		ctx context.Context,
		streamSourceID streamtypes.StreamSourceID,
	) error
	ListStreamPlayers(
		ctx context.Context,
	) ([]StreamPlayer, error)
	GetStreamPlayer(
		ctx context.Context,
		streamSourceID streamtypes.StreamSourceID,
	) (*StreamPlayer, error)

	StreamPlayerProcessTitle(
		ctx context.Context,
		streamSourceID StreamSourceID,
	) (string, error)
	StreamPlayerOpenURL(
		ctx context.Context,
		streamSourceID StreamSourceID,
		link string,
	) error
	StreamPlayerGetLink(ctx context.Context, streamSourceID StreamSourceID) (string, error)
	StreamPlayerEndChan(
		ctx context.Context,
		streamSourceID StreamSourceID,
	) (<-chan struct{}, error)
	StreamPlayerIsEnded(ctx context.Context, streamSourceID StreamSourceID) (bool, error)
	StreamPlayerGetPosition(
		ctx context.Context,
		streamSourceID StreamSourceID,
	) (time.Duration, error)
	StreamPlayerGetLength(
		ctx context.Context,
		streamSourceID StreamSourceID,
	) (time.Duration, error)
	StreamPlayerGetLag(
		ctx context.Context,
		streamSourceID StreamSourceID,
	) (time.Duration, time.Time, error)
	StreamPlayerSetSpeed(
		ctx context.Context,
		streamSourceID StreamSourceID,
		speed float64,
	) error
	StreamPlayerSetPause(
		ctx context.Context,
		streamSourceID StreamSourceID,
		pause bool,
	) error
	StreamPlayerStop(ctx context.Context, streamSourceID StreamSourceID) error
	StreamPlayerClose(ctx context.Context, streamSourceID StreamSourceID) error

	SubscribeToConfigChanges(ctx context.Context) (<-chan DiffConfig, error)
	SubscribeToStreamsChanges(ctx context.Context) (<-chan DiffStreams, error)
	SubscribeToStreamServersChanges(
		ctx context.Context,
	) (<-chan DiffStreamServers, error)
	SubscribeToStreamSinksChanges(
		ctx context.Context,
	) (<-chan DiffStreamSinks, error)
	SubscribeToStreamSourcesChanges(
		ctx context.Context,
	) (<-chan DiffStreamSources, error)
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
		since time.Time,
		limit uint64,
	) (<-chan ChatMessage, error)
	SendChatMessage(
		ctx context.Context,
		platID streamcontrol.PlatformID,
		message string,
	) error
	RemoveChatMessage(
		ctx context.Context,
		platID streamcontrol.PlatformID,
		msgID streamcontrol.EventID,
	) error
	BanUser(
		ctx context.Context,
		platID streamcontrol.PlatformID,
		userID streamcontrol.UserID,
		reason string,
		deadline time.Time,
	) error
	Shoutout(
		ctx context.Context,
		platID streamcontrol.PlatformID,
		userID streamcontrol.UserID,
	) error
	RaidTo(
		ctx context.Context,
		platID streamcontrol.PlatformID,
		userID streamcontrol.UserID,
	) error

	GetPeerIDs(ctx context.Context) ([]p2ptypes.PeerID, error)
	DialPeerByID(
		ctx context.Context,
		peerID p2ptypes.PeerID,
	) (StreamD, error)

	DialContext(
		ctx context.Context,
		network string,
		addr string,
	) (net.Conn, error)

	LLMGenerate(ctx context.Context, prompt string) (string, error)
}

type StreamPlayer = sstypes.StreamPlayer

type BackendDataOBS struct{}

type BackendDataTwitch struct {
	Cache *cache.Twitch
}

type BackendDataKick struct {
	Cache *cache.Kick
}

type BackendDataYouTube struct {
	Cache *cache.YouTube
}

type StreamServerType = streamtypes.ServerType

type StreamServer struct {
	streamportserver.Config

	NumBytesConsumerWrote uint64
	NumBytesProducerRead  uint64
}

type StreamSink struct {
	ID StreamSinkIDFullyQualified
	sstypes.StreamSinkConfig
}

type StreamForward struct {
	Enabled        bool
	StreamSourceID StreamSourceID
	StreamSinkID   StreamSinkIDFullyQualified
	Encode         sstypes.EncodeConfig
	Quirks         StreamForwardingQuirks
	NumBytesWrote  uint64
	NumBytesRead   uint64
}

type StreamSource struct {
	StreamSourceID StreamSourceID
	IsActive       bool
}

type StreamSourceID = streamtypes.StreamSourceID
type StreamSinkID = streamtypes.StreamSinkID
type StreamSinkIDFullyQualified = streamtypes.StreamSinkIDFullyQualified
type OBSInstanceID = streamtypes.OBSInstanceID

type StreamForwardingQuirks = sstypes.ForwardingQuirks

type RestartUntilPlatformRecognizesStream = sstypes.RestartUntilPlatformRecognizesStream

type WaitUntilPlatformRecognizesStream = sstypes.WaitUntilPlatformRecognizesStream

type DiffConfig struct{}
type DiffDashboard struct{}
type DiffStreams struct{}
type DiffStreamServers struct{}
type DiffStreamSinks struct{}
type DiffStreamSources struct{}
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
	streamcontrol.Event
	Platform streamcontrol.PlatformID
	IsLive   bool
}

type VariableValue []byte
