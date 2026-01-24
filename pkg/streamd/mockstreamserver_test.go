package streamd

import (
	"context"

	"github.com/xaionaro-go/player/pkg/player"
	sptypes "github.com/xaionaro-go/streamctl/pkg/streamplayer/types"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/streamforward"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/types"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/types/streamportserver"
	"github.com/xaionaro-go/streamctl/pkg/streamtypes"
)

type mockStreamServer struct {
	Sinks    []types.StreamSink
	Forwards []streamforward.StreamForward
}

func (m *mockStreamServer) Init(ctx context.Context, opts ...types.InitOption) error { return nil }
func (m *mockStreamServer) StartServer(ctx context.Context, serverType streamtypes.ServerType, listenAddr string, opts ...streamportserver.Option) (streamportserver.Server, error) {
	return nil, nil
}
func (m *mockStreamServer) StopServer(ctx context.Context, server streamportserver.Server) error {
	return nil
}
func (m *mockStreamServer) ListServers(ctx context.Context) []streamportserver.Server { return nil }
func (m *mockStreamServer) AddStreamSource(ctx context.Context, streamSourceID types.StreamSourceID) error {
	return nil
}
func (m *mockStreamServer) ListStreamSources(ctx context.Context) []types.StreamSource {
	return nil
}
func (m *mockStreamServer) RemoveStreamSource(ctx context.Context, streamSourceID types.StreamSourceID) error {
	return nil
}
func (m *mockStreamServer) ListStreamSinks(ctx context.Context) ([]types.StreamSink, error) {
	return m.Sinks, nil
}
func (m *mockStreamServer) AddStreamSink(ctx context.Context, streamSinkID types.StreamSinkIDFullyQualified, sink types.StreamSinkConfig) error {
	return nil
}
func (m *mockStreamServer) UpdateStreamSink(ctx context.Context, streamSinkID types.StreamSinkIDFullyQualified, sink types.StreamSinkConfig) error {
	return nil
}
func (m *mockStreamServer) RemoveStreamSink(ctx context.Context, streamSinkID types.StreamSinkIDFullyQualified) error {
	return nil
}
func (m *mockStreamServer) AddStreamForward(ctx context.Context, streamSourceID types.StreamSourceID, streamSinkID types.StreamSinkIDFullyQualified, enabled bool, encode types.EncodeConfig, quirks types.ForwardingQuirks) (*streamforward.StreamForward, error) {
	f := streamforward.StreamForward{
		StreamSourceID: streamSourceID,
		StreamSinkID:   streamSinkID,
		Enabled:        enabled,
		Encode:         encode,
		Quirks:         quirks,
	}
	m.Forwards = append(m.Forwards, f)
	return &f, nil
}
func (m *mockStreamServer) ListStreamForwards(ctx context.Context) ([]streamforward.StreamForward, error) {
	return m.Forwards, nil
}
func (m *mockStreamServer) UpdateStreamForward(ctx context.Context, streamSourceID types.StreamSourceID, streamSinkID types.StreamSinkIDFullyQualified, enabled bool, encode types.EncodeConfig, quirks types.ForwardingQuirks) (*streamforward.StreamForward, error) {
	for i, f := range m.Forwards {
		if f.StreamSourceID == streamSourceID && f.StreamSinkID == streamSinkID {
			m.Forwards[i].Enabled = enabled
			return &m.Forwards[i], nil
		}
	}
	return nil, nil
}
func (m *mockStreamServer) RemoveStreamForward(ctx context.Context, streamSourceID types.StreamSourceID, dstID types.StreamSinkIDFullyQualified) error {
	return nil
}
func (m *mockStreamServer) AddStreamPlayer(ctx context.Context, streamSourceID types.StreamSourceID, playerType player.Backend, disabled bool, streamPlaybackConfig sptypes.Config, opts ...types.StreamPlayerOption) error {
	return nil
}
func (m *mockStreamServer) UpdateStreamPlayer(ctx context.Context, streamSourceID types.StreamSourceID, playerType player.Backend, disabled bool, streamPlaybackConfig sptypes.Config, opts ...types.StreamPlayerOption) error {
	return nil
}
func (m *mockStreamServer) RemoveStreamPlayer(ctx context.Context, streamSourceID types.StreamSourceID) error {
	return nil
}
func (m *mockStreamServer) ListStreamPlayers(ctx context.Context) ([]types.StreamPlayer, error) {
	return nil, nil
}
func (m *mockStreamServer) GetStreamPlayer(ctx context.Context, streamSourceID types.StreamSourceID) (*types.StreamPlayer, error) {
	return nil, nil
}
func (m *mockStreamServer) GetActiveStreamPlayer(ctx context.Context, streamSourceID types.StreamSourceID) (player.Player, error) {
	return nil, nil
}
func (m *mockStreamServer) SetStreamActive(ctx context.Context, streamSourceID types.StreamSourceID, active bool) error {
	return nil
}
func (m *mockStreamServer) IsStreamActive(ctx context.Context, streamSourceID types.StreamSourceID) bool {
	return false
}
func (m *mockStreamServer) ActiveStreamSourceIDs() ([]types.StreamSourceID, error) { return nil, nil }
func (m *mockStreamServer) GetPortServers(ctx context.Context) ([]streamportserver.Config, error) {
	return nil, nil
}
func (m *mockStreamServer) WaitPublisherChan(ctx context.Context, streamSourceID streamtypes.StreamSourceID, waitActive bool) (<-chan types.Publisher, error) {
	return nil, nil
}
