package iface

import (
	"context"

	"github.com/xaionaro-go/streamctl/pkg/player"
	"github.com/xaionaro-go/streamctl/pkg/streamplayer"
	sptypes "github.com/xaionaro-go/streamctl/pkg/streamplayer/types"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/streamforward"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/types"
	"github.com/xaionaro-go/streamctl/pkg/streamtypes"
)

type StreamServer interface {
	streamplayer.StreamServer
	types.PubsubNameser

	Init(
		ctx context.Context,
		opts ...types.InitOption,
	) error

	StartServer(
		ctx context.Context,
		serverType streamtypes.ServerType,
		listenAddr string,
		opts ...types.ServerOption,
	) (types.PortServer, error)
	StopServer(
		ctx context.Context,
		server types.PortServer,
	) error

	AddIncomingStream(
		ctx context.Context,
		streamID types.StreamID,
	) error
	ListIncomingStreams(
		ctx context.Context,
	) []types.IncomingStream
	RemoveIncomingStream(
		ctx context.Context,
		streamID types.StreamID,
	) error

	ListStreamDestinations(
		ctx context.Context,
	) ([]types.StreamDestination, error)
	AddStreamDestination(
		ctx context.Context,
		destinationID types.DestinationID,
		url string,
		streamKey string,
	) error
	UpdateStreamDestination(
		ctx context.Context,
		destinationID types.DestinationID,
		url string,
		streamKey string,
	) error
	RemoveStreamDestination(
		ctx context.Context,
		destinationID types.DestinationID,
	) error

	AddStreamForward(
		ctx context.Context,
		streamID types.StreamID,
		destinationID types.DestinationID,
		enabled bool,
		quirks types.ForwardingQuirks,
	) (*streamforward.StreamForward, error)
	ListStreamForwards(
		ctx context.Context,
	) ([]streamforward.StreamForward, error)
	UpdateStreamForward(
		ctx context.Context,
		streamID types.StreamID,
		destinationID types.DestinationID,
		enabled bool,
		quirks types.ForwardingQuirks,
	) (*streamforward.StreamForward, error)
	RemoveStreamForward(
		ctx context.Context,
		streamID types.StreamID,
		dstID types.DestinationID,
	) error

	AddStreamPlayer(
		ctx context.Context,
		streamID types.StreamID,
		playerType player.Backend,
		disabled bool,
		streamPlaybackConfig sptypes.Config,
		opts ...types.StreamPlayerOption,
	) error
	UpdateStreamPlayer(
		ctx context.Context,
		streamID types.StreamID,
		playerType player.Backend,
		disabled bool,
		streamPlaybackConfig sptypes.Config,
		opts ...types.StreamPlayerOption,
	) error
	RemoveStreamPlayer(
		ctx context.Context,
		streamID types.StreamID,
	) error
	ListStreamPlayers(
		ctx context.Context,
	) ([]types.StreamPlayer, error)
	GetStreamPlayer(
		ctx context.Context,
		streamID types.StreamID,
	) (*types.StreamPlayer, error)
	GetActiveStreamPlayer(
		ctx context.Context,
		streamID types.StreamID,
	) (player.Player, error)

	ListServers(ctx context.Context) []types.PortServer
}
