package streamd

import (
	"context"
	"testing"
	"time"

	"github.com/facebookincubator/go-belt"

	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/streamctl/pkg/secret"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/kick"
	"github.com/xaionaro-go/streamctl/pkg/streamd/api"
	"github.com/xaionaro-go/streamctl/pkg/streamd/config"
)

func TestKickCategoryCache(t *testing.T) {
	ctx, cancel := context.WithTimeout(observability.WithSecretsProvider(context.Background(), &observability.SecretsStaticProvider{}), 4*time.Minute)
	defer cancel()

	kick.SetDebugUseMockClient(true)

	d, err := New(config.Config{
		Backends: streamcontrol.Config{
			kick.ID: &streamcontrol.AbstractPlatformConfig{
				Accounts: map[streamcontrol.AccountID]streamcontrol.RawMessage{
					"account1": streamcontrol.ToRawMessage(kick.AccountConfig{
						Channel:      "channel1",
						ClientID:     "id1",
						ClientSecret: secret.New("secret1"),
					}),
					"account2": streamcontrol.ToRawMessage(kick.AccountConfig{
						Channel:      "channel2",
						ClientID:     "id2",
						ClientSecret: secret.New("secret2"),
					}),
				},
			},
		},
	}, &mockUI{}, nil, belt.New())
	require.NoError(t, err)

	d.OAuthListenPorts = map[uint16]struct{}{8080: {}, 8081: {}}

	err = d.initKickBackend(ctx)
	require.NoError(t, err)

	changed := d.initKickData(ctx)
	require.True(t, changed, "Expected cache to be changed")

	require.NotEmpty(t, d.GetKickCache(ctx, "account1").Categories, "account1 should have categories")
	require.NotEmpty(t, d.GetKickCache(ctx, "account2").Categories, "account2 should have categories")

	info, err := d.GetBackendInfo(ctx, kick.ID, true)
	require.NoError(t, err)

	backendData, ok := info.Data.(api.BackendDataKick)
	require.True(t, ok, "Expected api.BackendDataKick, got %T", info.Data)

	categories := backendData.Cache.GetCategories()
	require.NotEmpty(t, categories, "Expected categories in global cache")
}
