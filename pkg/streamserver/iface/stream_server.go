package iface

import (
	"context"

	"github.com/xaionaro-go/player/pkg/player"
	"github.com/xaionaro-go/streamctl/pkg/streamplayer"
	sptypes "github.com/xaionaro-go/streamctl/pkg/streamplayer/types"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/streamforward"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/types"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/types/streamportserver"
	"github.com/xaionaro-go/streamctl/pkg/streamtypes"
)

type StreamServer interface {
	streamplayer.StreamServer
	types.ActiveStreamSourceIDsProvider

	Init(
		ctx context.Context,
		opts ...types.InitOption,
	) error

	StartServer(
		ctx context.Context,
		serverType streamtypes.ServerType,
		listenAddr string,
		opts ...streamportserver.Option,
	) (streamportserver.Server, error)
	StopServer(
		ctx context.Context,
		server streamportserver.Server,
	) error

	AddStreamSource(
		ctx context.Context,
		streamSourceID types.StreamSourceID,
	) error
	ListStreamSources(
		ctx context.Context,
	) []types.StreamSource
	RemoveStreamSource(
		ctx context.Context,
		streamSourceID types.StreamSourceID,
	) error

	ListStreamSinks(
		ctx context.Context,
	) ([]types.StreamSink, error)
	AddStreamSink(
		ctx context.Context,
		streamSinkID types.StreamSinkIDFullyQualified,
		sink types.StreamSinkConfig,
	) error
	UpdateStreamSink(
		ctx context.Context,
		streamSinkID types.StreamSinkIDFullyQualified,
		sink types.StreamSinkConfig,
	) error
	RemoveStreamSink(
		ctx context.Context,
		streamSinkID types.StreamSinkIDFullyQualified,
	) error

	AddStreamForward(
		ctx context.Context,
		streamSourceID types.StreamSourceID,
		streamSinkID types.StreamSinkIDFullyQualified,
		enabled bool,
		encode types.EncodeConfig,
		quirks types.ForwardingQuirks,
	) (*streamforward.StreamForward, error)
	ListStreamForwards(
		ctx context.Context,
	) ([]streamforward.StreamForward, error)
	UpdateStreamForward(
		ctx context.Context,
		streamSourceID types.StreamSourceID,
		streamSinkID types.StreamSinkIDFullyQualified,
		enabled bool,
		encode types.EncodeConfig,
		quirks types.ForwardingQuirks,
	) (*streamforward.StreamForward, error)
	RemoveStreamForward(
		ctx context.Context,
		streamSourceID types.StreamSourceID,
		streamSinkID types.StreamSinkIDFullyQualified,
	) error

	AddStreamPlayer(
		ctx context.Context,
		streamSourceID types.StreamSourceID,
		playerType player.Backend,
		disabled bool,
		streamPlaybackConfig sptypes.Config,
		opts ...types.StreamPlayerOption,
	) error
	UpdateStreamPlayer(
		ctx context.Context,
		streamSourceID types.StreamSourceID,
		playerType player.Backend,
		disabled bool,
		streamPlaybackConfig sptypes.Config,
		opts ...types.StreamPlayerOption,
	) error
	RemoveStreamPlayer(
		ctx context.Context,
		streamSourceID types.StreamSourceID,
	) error
	ListStreamPlayers(
		ctx context.Context,
	) ([]types.StreamPlayer, error)
	GetStreamPlayer(
		ctx context.Context,
		streamSourceID types.StreamSourceID,
	) (*types.StreamPlayer, error)
	GetActiveStreamPlayer(
		ctx context.Context,
		streamSourceID types.StreamSourceID,
	) (player.Player, error)

	ListServers(ctx context.Context) []streamportserver.Server
}
