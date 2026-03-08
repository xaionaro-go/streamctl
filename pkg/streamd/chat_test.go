package streamd

import (
	"context"
	"testing"
	"time"

	benbjohnsonclock "github.com/benbjohnson/clock"
	"github.com/facebookincubator/go-belt"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/streamctl/pkg/clock"
	"github.com/xaionaro-go/streamctl/pkg/secret"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/twitch"
	"github.com/xaionaro-go/streamctl/pkg/streamd/config"
)

func TestStreamDChat(t *testing.T) {
	twitch.SetDebugUseMockClient(true)
	cfg := config.Config{
		Backends: streamcontrol.Config{
			twitch.ID: &streamcontrol.AbstractPlatformConfig{
				Accounts: map[streamcontrol.AccountID]streamcontrol.RawMessage{
					"test": streamcontrol.ToRawMessage(twitch.AccountConfig{
						AccountConfigBase: streamcontrol.AccountConfigBase[twitch.StreamProfile]{
							Enable: ptr(true),
						},
						Channel:      "test",
						AuthType:     "user",
						ClientID:     "dummy",
						ClientSecret: secret.New("dummy"),
					}),
				},
			},
		},
	}

	mockClock := benbjohnsonclock.NewMock()
	clock.Set(mockClock)
	d, err := New(cfg, &mockUI{}, nil, belt.New())
	require.NoError(t, err)

	d.AddOAuthListenPort(8093)

	ctx, cancel := context.WithCancel(observability.WithSecretsProvider(context.Background(), &observability.SecretsStaticProvider{}))
	defer cancel()
	err = d.Run(ctx)
	require.NoError(t, err)

	// Wait for message to be processed (mocked)
	ch, err := d.SubscribeToChatMessages(ctx, clock.Get().Now().Add(-time.Hour), 10)
	require.NoError(t, err)

	// Send a chat message
	err = d.SendChatMessage(ctx, twitch.ID, "Hello world!")
	require.NoError(t, err)

	// Mock Twitch client automatically adds messages when SendChatMessage is called in mock mode
	mockClock.Add(time.Second)
	select {
	case msg := <-ch:
		require.NotNil(t, msg.Message)
		require.Equal(t, "Hello world!", msg.Message.Content)
	case <-clock.Get().After(5 * time.Second):
		t.Fatal("timeout waiting for chat message")
	}
}
