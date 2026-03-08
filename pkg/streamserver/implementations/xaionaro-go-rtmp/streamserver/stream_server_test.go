package streamserver

import (
	"context"
	"testing"

	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/facebookincubator/go-belt/tool/logger/implementation/logrus"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/types"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/types/streamportserver"
	"github.com/xaionaro-go/streamctl/pkg/streamtypes"
)

type mockPlatformsController struct {
	types.PlatformsController
}

func (m *mockPlatformsController) GetStreamSinkConfig(ctx context.Context, id streamcontrol.StreamIDFullyQualified) (types.StreamSinkConfig, error) {
	return types.StreamSinkConfig{}, nil
}
func (m *mockPlatformsController) CheckStreamStartedByStreamSourceID(ctx context.Context, streamID streamcontrol.StreamIDFullyQualified) (bool, error) {
	return true, nil
}
func (m *mockPlatformsController) GetActiveStreamIDs(ctx context.Context) ([]streamcontrol.StreamIDFullyQualified, error) {
	return nil, nil
}

func TestInitPortExhaustion(t *testing.T) {
	l := logrus.Default().WithLevel(logger.LevelTrace)
	ctx := logger.CtxWithLogger(context.Background(), l)
	ctx = belt.CtxWithBelt(ctx, belt.New())

	cfg := &types.Config{
		PortServers: []streamportserver.Config{
			{
				Type:       streamtypes.ServerTypeRTMP,
				ListenAddr: "127.0.0.1:0",
			},
		},
		StaticSinks: map[types.StreamSinkID]*types.StreamSinkConfig{
			"local_sink": {
				URL: "/test_stream",
			},
		},
		Streams: map[types.StreamSourceID]*types.StreamConfig{
			"source_stream": {
				Forwardings: map[types.StreamSinkIDFullyQualified]types.ForwardingConfig{
					types.NewStreamSinkIDFullyQualified(streamtypes.StreamSinkTypeCustom, "local_sink"): {},
				},
			},
		},
	}

	for i := 0; i < 100; i++ { // Try many times to increase chance of race condition
		s := New(cfg, &mockPlatformsController{})
		err := s.Init(ctx)
		if err != nil {
			require.NotContains(t, err.Error(), "there are no open server ports", "Failed at iteration %d", i)
			require.NoError(t, err, "Failed at iteration %d", i)
		}
	}
}
