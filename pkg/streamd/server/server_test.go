package server

import (
	"context"
	"testing"

	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamd/api"
	"github.com/xaionaro-go/streamctl/pkg/streamd/config"
	"github.com/xaionaro-go/streamctl/pkg/streamd/grpc/go/streamd_grpc"
)

type mockStreamD struct {
	api.StreamD
	config *config.Config
}

func (m *mockStreamD) GetConfig(ctx context.Context) (*config.Config, error) {
	return m.config, nil
}

func TestServerListProfiles(t *testing.T) {
	mock := &mockStreamD{
		config: &config.Config{
			ProfileMetadata: map[streamcontrol.ProfileName]config.ProfileMetadata{
				"profileB": {},
				"profileA": {},
			},
		},
	}
	srv := &GRPCServer{
		StreamD: mock,
	}

	resp, err := srv.ListProfiles(context.Background(), &streamd_grpc.ListProfilesRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(resp.Profiles) != 2 {
		t.Fatalf("expected 2 profiles, got %d", len(resp.Profiles))
	}

	if resp.Profiles[0].Name != "profileA" {
		t.Errorf("expected first profile to be profileA, got %s", resp.Profiles[0].Name)
	}
	if resp.Profiles[1].Name != "profileB" {
		t.Errorf("expected second profile to be profileB, got %s", resp.Profiles[1].Name)
	}
}
