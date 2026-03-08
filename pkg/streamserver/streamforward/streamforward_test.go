package streamforward

import (
	"context"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/recoder"
	"github.com/xaionaro-go/streamctl/pkg/secret"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/types"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/types/streamportserver"
	"github.com/xaionaro-go/streamctl/pkg/streamtypes"
)

type mockPlatformsController struct {
	types.PlatformsController
}

func (m *mockPlatformsController) GetStreamSinkConfig(ctx context.Context, id streamcontrol.StreamIDFullyQualified) (types.StreamSinkConfig, error) {
	return types.StreamSinkConfig{URL: "rtmp://target/live", StreamKey: secret.New("key")}, nil
}
func (m *mockPlatformsController) CheckStreamStartedByURL(ctx context.Context, sinkURL *url.URL) (bool, error) {
	return true, nil
}
func (m *mockPlatformsController) CheckStreamStartedByPlatformID(ctx context.Context, platID streamcontrol.PlatformID) (bool, error) {
	return true, nil
}
func (m *mockPlatformsController) CheckStreamStartedByStreamSourceID(ctx context.Context, streamID streamcontrol.StreamIDFullyQualified) (bool, error) {
	return true, nil
}
func (m *mockPlatformsController) WaitStreamStartedByStreamSourceID(ctx context.Context, streamID streamcontrol.StreamIDFullyQualified) error {
	return nil
}
func (m *mockPlatformsController) GetActiveStreamIDs(ctx context.Context) ([]streamcontrol.StreamIDFullyQualified, error) {
	return nil, nil
}

type mockStreamServer struct {
	StreamServer
}

func (m *mockStreamServer) GetPortServers(ctx context.Context) ([]streamportserver.Config, error) {
	return []streamportserver.Config{{Type: streamtypes.ServerTypeRTMP, ListenAddr: ":1935"}}, nil
}
func (m *mockStreamServer) WithConfig(ctx context.Context, callback func(context.Context, *types.Config)) {
	callback(ctx, &types.Config{})
}
func (m *mockStreamServer) ActiveStreamSourceIDs() ([]types.StreamSourceID, error) {
	return nil, nil
}
func (m *mockStreamServer) ListStreamSources(ctx context.Context) []types.StreamSource {
	return nil
}
func (m *mockStreamServer) WaitPublisherChan(ctx context.Context, streamSourceID streamtypes.StreamSourceID, waitForNext bool) (<-chan types.Publisher, error) {
	return nil, nil
}

func TestStreamForwardRecursiveDetection(t *testing.T) {
	fwds := &StreamForwards{
		PlatformsController: &mockPlatformsController{},
		StreamServer:        &mockStreamServer{},
		StreamSinks: []types.StreamSink{
			{ID: types.StreamSinkIDFullyQualified{Type: types.StreamSinkTypeLocal, ID: "src2"}, URL: ""},
		},
	}

	ctx := context.Background()

	// Test: local: prefix detection
	fwd, err := fwds.NewActiveStreamForward(ctx, "src1", types.StreamSinkIDFullyQualified{Type: types.StreamSinkTypeLocal, ID: "src2"}, types.EncodeConfig{}, types.ForwardingQuirks{}, nil)
	require.NoError(t, err)
	assert.Equal(t, "rtmp://localhost:1935/local/src2", fwd.ActiveForwarding.StreamSinkURL.String())
}

func TestStreamForwardTranscodingTrigger(t *testing.T) {
	fwds := &StreamForwards{
		PlatformsController: &mockPlatformsController{},
		StreamServer:        &mockStreamServer{},
		StreamSinks: []types.StreamSink{
			{ID: types.StreamSinkIDFullyQualified{Type: types.StreamSinkTypeCustom, ID: "dest1"}, URL: "rtmp://remote/live"},
		},
	}

	ctx := context.Background()
	encode := types.EncodeConfig{
		Enabled: true,
		EncodersConfig: recoder.EncodersConfig{
			OutputVideoTracks: []recoder.VideoTrackEncodingConfig{
				{
					Config: recoder.EncodeVideoConfig{
						Codec: recoder.VideoCodecH264,
					},
				},
			},
		},
	}

	fwd, err := fwds.NewActiveStreamForward(ctx, "src1", types.StreamSinkIDFullyQualified{Type: types.StreamSinkTypeCustom, ID: "dest1"}, encode, types.ForwardingQuirks{}, nil)
	require.NoError(t, err)
	assert.Equal(t, encode, fwd.Encode)
}
