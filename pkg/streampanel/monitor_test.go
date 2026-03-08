package streampanel

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/streamctl/pkg/streamd/api"
	"github.com/xaionaro-go/streamctl/pkg/streampanel/config"
)

type mockStreamD struct {
	api.StreamD
	ListStreamSourcesFunc func(ctx context.Context) ([]api.StreamSource, error)
}

func (m *mockStreamD) ListStreamSources(ctx context.Context) ([]api.StreamSource, error) {
	if m.ListStreamSourcesFunc == nil {
		return nil, nil
	}
	return m.ListStreamSourcesFunc(ctx)
}

func TestMonitorPage_Init_EmptyAddress(t *testing.T) {
	ctx := context.Background()
	p := &Panel{
		StreamD: &mockStreamD{},
		Config: config.Config{
			RemoteStreamDAddr: "",
		},
	}
	mp := &monitorPage{
		Panel:          p,
		activeMonitors: map[monitorKey]activeMonitor{},
	}

	// This should not panic despite RemoteStreamDAddr being empty
	require.NotPanics(t, func() {
		_ = mp.init(ctx)
	})
}
