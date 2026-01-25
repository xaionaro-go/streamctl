package streampanel

import (
	"context"
	"testing"
	"time"

	"fyne.io/fyne/v2/test"
	benbjohnsonclock "github.com/benbjohnson/clock"
	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/streamctl/pkg/clock"
	"github.com/xaionaro-go/streamctl/pkg/oauthhandler"
	"github.com/xaionaro-go/streamctl/pkg/secret"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/kick"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/obs"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/twitch"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/youtube"
	"github.com/xaionaro-go/streamctl/pkg/streamd"
	"github.com/xaionaro-go/streamctl/pkg/streamd/config"
	sstypes "github.com/xaionaro-go/streamctl/pkg/streamserver/types"
	"golang.org/x/oauth2"
)

type mockUI struct{}

func (m *mockUI) SetStatus(string)                              {}
func (m *mockUI) SetLoggingLevel(context.Context, logger.Level) {}
func (m *mockUI) DisplayError(err error) {
	if err != nil {
		logger.Default().Errorf("UI Error: %v", err)
	}
}
func (m *mockUI) OAuthHandler(ctx context.Context, platID streamcontrol.PlatformID, arg oauthhandler.OAuthHandlerArgument) error {
	return nil
}
func (m *mockUI) OpenBrowser(ctx context.Context, url string) error {
	return nil
}
func (m *mockUI) OnSubmittedOAuthCode(ctx context.Context, platID streamcontrol.PlatformID, code string) error {
	return nil
}

func TestIntegration(t *testing.T) {
	// 0. Enable Mocks
	youtube.SetDebugUseMockClient(true)
	twitch.SetDebugUseMockClient(true)
	kick.SetDebugUseMockClient(true)
	obs.SetDebugUseMockClient(true)

	// 1. Prepare Complex Config
	cfg := config.Config{
		Backends: make(map[streamcontrol.PlatformID]*streamcontrol.AbstractPlatformConfig),
	}

	mockClock := benbjohnsonclock.NewMock()

	// YouTube: 2 accounts
	expiry := mockClock.Now().Add(24 * time.Hour)
	ytToken1 := secret.New(oauth2.Token{AccessToken: "dummy", Expiry: expiry})
	ytToken2 := secret.New(oauth2.Token{AccessToken: "dummy", Expiry: expiry})
	cfg.Backends[youtube.ID] = &streamcontrol.AbstractPlatformConfig{
		Accounts: map[streamcontrol.AccountID]streamcontrol.RawMessage{
			"yt1": streamcontrol.ToRawMessage(youtube.AccountConfig{
				ClientID:     "id1",
				ClientSecret: secret.New("secret1"),
				Token:        &ytToken1,
			}),
			"yt2": streamcontrol.ToRawMessage(youtube.AccountConfig{
				ClientID:     "id2",
				ClientSecret: secret.New("secret2"),
				Token:        &ytToken2,
			}),
		},
	}

	// Twitch: 2 accounts
	cfg.Backends[twitch.ID] = &streamcontrol.AbstractPlatformConfig{
		Accounts: map[streamcontrol.AccountID]streamcontrol.RawMessage{
			"tw1": streamcontrol.ToRawMessage(twitch.AccountConfig{
				ClientID:     "id1",
				ClientSecret: secret.New("secret1"),
				Channel:      "chan1",
				AuthType:     "user",
			}),
			"tw2": streamcontrol.ToRawMessage(twitch.AccountConfig{
				ClientID:     "id2",
				ClientSecret: secret.New("secret2"),
				Channel:      "chan2",
				AuthType:     "user",
			}),
		},
	}

	// Kick: 2 accounts
	cfg.Backends[kick.ID] = &streamcontrol.AbstractPlatformConfig{
		Accounts: map[streamcontrol.AccountID]streamcontrol.RawMessage{
			"ki1": streamcontrol.ToRawMessage(kick.AccountConfig{
				Channel:      "chan1",
				ClientID:     "id1",
				ClientSecret: secret.New("secret1"),
			}),
			"ki2": streamcontrol.ToRawMessage(kick.AccountConfig{
				Channel:      "chan2",
				ClientID:     "id2",
				ClientSecret: secret.New("secret2"),
			}),
		},
	}

	// OBS: 2 accounts
	cfg.Backends[obs.ID] = &streamcontrol.AbstractPlatformConfig{
		Accounts: map[streamcontrol.AccountID]streamcontrol.RawMessage{
			"obs1": streamcontrol.ToRawMessage(obs.AccountConfig{Host: "localhost", Port: 4455}),
			"obs2": streamcontrol.ToRawMessage(obs.AccountConfig{Host: "localhost", Port: 4456}),
		},
	}

	// Forwarding
	cfg.StreamServer = sstypes.Config{
		StaticSinks: map[sstypes.StreamSinkID]*sstypes.StreamSinkConfig{
			"youtube-sink": {
				URL:       "rtmp://a.rtmp.youtube.com/live2",
				StreamKey: secret.New("dummy-key"),
			},
		},
		Streams: map[sstypes.StreamSourceID]*sstypes.StreamConfig{
			"local-srt": {
				Forwardings: map[sstypes.StreamSinkIDFullyQualified]sstypes.ForwardingConfig{
					{Type: sstypes.StreamSinkTypeCustom, ID: "youtube-sink"}: {},
				},
			},
		},
	}

	// 2. Initialize StreamD
	ctx := t.Context()
	ctx = observability.WithSecretsProvider(ctx, &observability.SecretsStaticProvider{})
	b := belt.New()
	clock.Set(mockClock)
	d, err := streamd.New(cfg, &mockUI{}, func(ctx context.Context, cfg config.Config) error {
		return nil
	}, b)
	require.NoError(t, err)

	d.AddOAuthListenPort(8091)
	d.AddOAuthListenPort(8092)
	d.AddOAuthListenPort(8093)
	d.AddOAuthListenPort(8094)

	go func() {
		err := d.Run(ctx)
		if err != nil && err != context.Canceled {
			t.Errorf("StreamD.Run returned error: %v", err)
		}
	}()

	// 3. Initialize Panel
	testApp := test.NewApp()
	p, err := New("", OptionApp{App: testApp})
	require.NoError(t, err)
	p.StreamD = d

	// 4. Verify discovery
	ctxVerify, cancelVerify := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancelVerify()

	// Wait a bit for initial init
	require.Eventually(t, func() bool {
		mockClock.Add(time.Second)
		streams, _ := d.GetStreams(ctxVerify)
		return len(streams) >= 1
	}, 10*time.Second, 100*time.Millisecond)

	platforms := d.GetPlatforms(ctxVerify)
	t.Logf("Platforms found: %v", platforms)

	accounts, err := d.GetAccounts(ctxVerify)
	if err != nil {
		t.Fatalf("GetAccounts failed: %v", err)
	}
	t.Logf("Accounts found: %v", accounts)
	require.GreaterOrEqual(t, len(accounts), 8)

	streams, err := d.GetStreams(ctxVerify)
	if err != nil {
		t.Fatalf("GetStreams failed: %v", err)
	}
	t.Logf("Streams found: %v", streams)
	require.GreaterOrEqual(t, len(streams), 1)

	// 5. Verify Forwarding
	require.NotNil(t, d.Config.StreamServer.Streams)
	require.Contains(t, d.Config.StreamServer.Streams, sstypes.StreamSourceID("local-srt"))

	// 6. Verify Panel initialization
	require.Equal(t, d, p.StreamD)
}
