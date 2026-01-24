package streamd

import (
	"context"
	"testing"

	"github.com/facebookincubator/go-belt"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/kick"
	"github.com/xaionaro-go/streamctl/pkg/streamd/config"
)

func TestKickAccountLifecycle(t *testing.T) {
	kick.SetDebugUseMockClient(true)
	ctx := context.Background()
	enable := true
	cfg := config.Config{
		Backends: streamcontrol.Config{
			kick.ID: &streamcontrol.AbstractPlatformConfig{
				Accounts: map[streamcontrol.AccountID]streamcontrol.RawMessage{
					"acc1": streamcontrol.ToRawMessage(kick.AccountConfig{
						AccountConfigBase: streamcontrol.AccountConfigBase[kick.StreamProfile]{
							Enable: &enable,
						},
						Channel: "channel1",
					}),
				},
			},
		},
	}
	d, err := New(cfg, &mockUI{}, nil, belt.New())
	require.NoError(t, err)

	err = d.EXPERIMENTAL_ReinitStreamControllers(ctx)
	require.NoError(t, err)

	controllers := d.getControllersByPlatform(kick.ID)
	require.Len(t, controllers, 1)
	require.Contains(t, controllers, streamcontrol.AccountID("acc1"))

	// Add account
	cfg.Backends[kick.ID].Accounts["acc2"] = streamcontrol.ToRawMessage(kick.AccountConfig{
		AccountConfigBase: streamcontrol.AccountConfigBase[kick.StreamProfile]{
			Enable: &enable,
		},
		Channel: "channel2",
	})
	err = d.SetConfig(ctx, &cfg)
	require.NoError(t, err)
	err = d.EXPERIMENTAL_ReinitStreamControllers(ctx)
	require.NoError(t, err)

	controllers = d.getControllersByPlatform(kick.ID)
	require.Len(t, controllers, 2)
	require.Contains(t, controllers, streamcontrol.AccountID("acc1"))
	require.Contains(t, controllers, streamcontrol.AccountID("acc2"))

	// Remove account
	delete(cfg.Backends[kick.ID].Accounts, "acc1")
	err = d.SetConfig(ctx, &cfg)
	require.NoError(t, err)
	err = d.EXPERIMENTAL_ReinitStreamControllers(ctx)
	require.NoError(t, err)

	controllers = d.getControllersByPlatform(kick.ID)
	require.Len(t, controllers, 1, "Should only have one account after deletion")
	require.NotContains(t, controllers, streamcontrol.AccountID("acc1"))
	require.Contains(t, controllers, streamcontrol.AccountID("acc2"))
}
