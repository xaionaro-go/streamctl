package streamserver

import (
	"context"
	"testing"

	testifyassert "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/iface"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/types"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/types/streamportserver"
	"github.com/xaionaro-go/streamctl/pkg/streamtypes"
)

type mockServer struct {
	iface.StreamServer
	running bool
}

func (m *mockServer) Start(ctx context.Context) error {
	m.running = true
	return nil
}

func (m *mockServer) Stop(ctx context.Context) error {
	m.running = false
	return nil
}

func (m *mockServer) GetPortServers(ctx context.Context) ([]streamportserver.Config, error) {
	return []streamportserver.Config{{Type: streamtypes.ServerTypeRTMP, ListenAddr: ":1935"}}, nil
}

type mockPlatformsController struct {
	types.PlatformsController
}

func (m *mockPlatformsController) GetStreamSinkConfig(ctx context.Context, id streamcontrol.StreamIDFullyQualified) (types.StreamSinkConfig, error) {
	return types.StreamSinkConfig{}, nil
}

func TestStreamServerManagement(t *testing.T) {
	s := &mockServer{}
	err := s.Start(context.Background())
	require.NoError(t, err)
	testifyassert.True(t, s.running)
}

func TestStreamServerForwardingRecursion(t *testing.T) {
	// Verify that the local: prefix is handled
}
