package streampanel

import (
	"context"
	"testing"

	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamd/api"
)

type mockStreamDForUI struct {
	api.StreamD
	accounts []streamcontrol.AccountIDFullyQualified
	streams  map[streamcontrol.AccountIDFullyQualified][]streamcontrol.StreamInfo
}

func (m *mockStreamDForUI) GetAccounts(ctx context.Context, platformID ...streamcontrol.PlatformID) ([]streamcontrol.AccountIDFullyQualified, error) {
	return m.accounts, nil
}

func (m *mockStreamDForUI) GetStreams(ctx context.Context, accountIDs ...streamcontrol.AccountIDFullyQualified) ([]streamcontrol.StreamInfo, error) {
	var result []streamcontrol.StreamInfo
	for _, accID := range accountIDs {
		result = append(result, m.streams[accID]...)
	}
	return result, nil
}

func TestProfileSelectStreams(t *testing.T) {
	t.Skip("This test is outdated and needs to be updated to the new UI structure")
}
