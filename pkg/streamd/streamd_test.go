package streamd

import (
	"context"
	"crypto"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/eventbus"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/streamctl/pkg/oauthhandler"
	"github.com/xaionaro-go/streamctl/pkg/secret"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/kick"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/twitch"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/youtube"
	"github.com/xaionaro-go/streamctl/pkg/streamd/api"
	"github.com/xaionaro-go/streamctl/pkg/streamd/config"
	streamdconsts "github.com/xaionaro-go/streamctl/pkg/streamd/consts"
	"github.com/xaionaro-go/streamctl/pkg/streamd/ui"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/streamforward"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/types"
)

type mockUI struct{}

func (m *mockUI) SetStatus(string) {}
func (m *mockUI) DisplayError(err error) {
	if err != nil {
		logger.Default().Errorf("UI Error: %v", err)
	}
}
func (m *mockUI) OpenBrowser(context.Context, string) error {
	return nil
}
func (m *mockUI) OAuthHandler(context.Context, streamcontrol.PlatformID, oauthhandler.OAuthHandlerArgument) error {
	return nil
}
func (m *mockUI) OnSubmittedOAuthCode(context.Context, streamcontrol.PlatformID, string) error {
	return nil
}
func (m *mockUI) SetLoggingLevel(context.Context, logger.Level) {}

var _ ui.UI = (*mockUI)(nil)

func TestStreamDLoggingLevel(t *testing.T) {
	d, err := New(config.Config{}, &mockUI{}, nil, belt.New())
	require.NoError(t, err)

	ctx := context.Background()
	err = d.SetLoggingLevel(ctx, logger.LevelDebug)
	require.NoError(t, err)

	level, err := d.GetLoggingLevel(ctx)
	require.NoError(t, err)
	require.Equal(t, logger.LevelDebug, level)
}

func TestStreamDConfigRoundTrip(t *testing.T) {
	cfg := config.Config{
		Backends: streamcontrol.Config{
			twitch.ID: &streamcontrol.AbstractPlatformConfig{
				Accounts: map[streamcontrol.AccountID]streamcontrol.RawMessage{
					"test": streamcontrol.RawMessage(`{"enable":true}`),
				},
			},
		},
	}
	d, err := New(cfg, &mockUI{}, nil, belt.New())
	require.NoError(t, err)

	ctx := context.Background()
	gotCfg, err := d.GetConfig(ctx)
	require.NoError(t, err)
	require.Equal(t, cfg.Backends[twitch.ID].IsEnabled(), gotCfg.Backends[twitch.ID].IsEnabled())

	newCfg := *gotCfg
	acc := newCfg.Backends[twitch.ID].Accounts["test"]
	acc.SetEnabled(false)
	newCfg.Backends[twitch.ID].Accounts["test"] = acc
	err = d.SetConfig(ctx, &newCfg)
	require.NoError(t, err)

	gotCfg2, err := d.GetConfig(ctx)
	require.NoError(t, err)
	require.Equal(t, false, gotCfg2.Backends[twitch.ID].IsEnabled())
}

func TestStreamDStreamControlMocked(t *testing.T) {
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

	d, err := New(cfg, &mockUI{}, nil, belt.New())
	require.NoError(t, err)

	d.AddOAuthListenPort(8092)

	ctx, cancel := context.WithCancel(observability.WithSecretsProvider(context.Background(), &observability.SecretsStaticProvider{}))
	defer cancel()
	err = d.Run(ctx)
	require.NoError(t, err)

	// Test StartStream
	err = d.SetStreamActive(ctx, streamcontrol.NewStreamIDFullyQualified(twitch.ID, "test", streamcontrol.DefaultStreamID), true)
	require.NoError(t, err)

	status, err := d.GetStreamStatus(ctx, streamcontrol.NewStreamIDFullyQualified(twitch.ID, "test", streamcontrol.DefaultStreamID))
	require.NoError(t, err)
	require.NotNil(t, status)

	// Test EndStream
	err = d.SetStreamActive(ctx, streamcontrol.NewStreamIDFullyQualified(twitch.ID, "test", streamcontrol.DefaultStreamID), false)
	require.NoError(t, err)
}
func TestStreamDWaitStreamStarted(t *testing.T) {
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
	d, err := New(cfg, &mockUI{}, nil, belt.New())
	require.NoError(t, err)
	d.AddOAuthListenPort(8094)
	ctx, cancel := context.WithCancel(observability.WithSecretsProvider(context.Background(), &observability.SecretsStaticProvider{}))
	defer cancel()
	err = d.Run(ctx)
	require.NoError(t, err)
	streamID := streamcontrol.StreamIDFullyQualified{
		AccountIDFullyQualified: streamcontrol.AccountIDFullyQualified{
			PlatformID: twitch.ID,
			AccountID:  "test",
		},
	}
	// Initially not live in mock
	twitch.SetMockIsLive(false)
	waitDone := make(chan error, 1)
	go func() {
		waitDone <- d.WaitStreamStartedByStreamSourceID(ctx, streamID)
	}()
	// Simulate stream starting after a short delay
	time.Sleep(500 * time.Millisecond)
	twitch.SetMockIsLive(true)
	select {
	case err := <-waitDone:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for stream to start")
	}
}
func TestStreamDGetBackendInfo(t *testing.T) {
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
	d, err := New(cfg, &mockUI{}, nil, belt.New())
	require.NoError(t, err)
	d.AddOAuthListenPort(8095)
	ctx, cancel := context.WithCancel(observability.WithSecretsProvider(context.Background(), &observability.SecretsStaticProvider{}))
	defer cancel()
	err = d.Run(ctx)
	require.NoError(t, err)
	info, err := d.GetBackendInfo(ctx, twitch.ID, true)
	require.NoError(t, err)
	require.NotNil(t, info)
	data, ok := info.Data.(api.BackendDataTwitch)
	require.True(t, ok)
	require.NotNil(t, data.Cache)
}
func TestStreamDSaveConfig(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "streamd-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	configPath := filepath.Join(tempDir, "config.yaml")

	cfg := config.Config{
		Backends: streamcontrol.Config{
			twitch.ID: &streamcontrol.AbstractPlatformConfig{
				Accounts: map[streamcontrol.AccountID]streamcontrol.RawMessage{
					"test": streamcontrol.ToRawMessage(twitch.AccountConfig{
						AccountConfigBase: streamcontrol.AccountConfigBase[twitch.StreamProfile]{
							Enable: ptr(true),
						},
					}),
				},
			},
		},
	}

	d, err := New(cfg, &mockUI{}, func(ctx context.Context, c config.Config) error {
		// This is the save function
		return config.WriteConfigToPath(ctx, configPath, c)
	}, belt.New())
	require.NoError(t, err)

	ctx := context.Background()
	err = d.SaveConfig(ctx)
	require.NoError(t, err)

	// Verify file exists and has content
	_, err = os.Stat(configPath)
	require.NoError(t, err)

	content, err := os.ReadFile(configPath)
	require.NoError(t, err)
	require.Contains(t, string(content), "twitch")
}

func TestStreamDResetCache(t *testing.T) {
	d, err := New(config.Config{}, &mockUI{}, nil, belt.New())
	require.NoError(t, err)

	ctx := context.Background()
	err = d.InitCache(ctx)
	require.NoError(t, err)

	err = d.ResetCache(ctx)
	require.NoError(t, err)

	// Just verify it doesn't panic and returns success
}

func TestStreamDGetStreams(t *testing.T) {
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

	d, err := New(cfg, &mockUI{}, nil, belt.New())
	require.NoError(t, err)

	d.AddOAuthListenPort(8096)

	ctx, cancel := context.WithCancel(observability.WithSecretsProvider(context.Background(), &observability.SecretsStaticProvider{}))
	defer cancel()
	err = d.Run(ctx)
	require.NoError(t, err)

	streams, err := d.GetStreams(ctx, streamcontrol.AccountIDFullyQualified{
		PlatformID: twitch.ID,
		AccountID:  "test",
	})
	require.NoError(t, err)
	require.NotNil(t, streams)
}

type mockController struct {
	streamcontrol.AccountCommons
}

func (m *mockController) ApplyProfile(ctx context.Context, streamSourceID streamcontrol.StreamID, profile youtube.StreamProfile, customArgs ...any) error {
	return nil
}
func (m *mockController) GetImplementation() streamcontrol.AccountCommons { return m }
func (m *mockController) String() string                                  { return "mock" }
func (m *mockController) Close() error                                    { return nil }
func (m *mockController) GetPlatformID() streamcontrol.PlatformID         { return youtube.ID }
func (m *mockController) SetStreamActive(ctx context.Context, streamSourceID streamcontrol.StreamID, isActive bool) error {
	return nil
}
func (m *mockController) SetTitle(ctx context.Context, streamSourceID streamcontrol.StreamID, title string) error {
	return nil
}
func (m *mockController) SetDescription(ctx context.Context, streamSourceID streamcontrol.StreamID, description string) error {
	return nil
}
func (m *mockController) GetStreamStatus(ctx context.Context, streamSourceID streamcontrol.StreamID) (*streamcontrol.StreamStatus, error) {
	return &streamcontrol.StreamStatus{}, nil
}
func (m *mockController) IsCapable(context.Context, streamcontrol.Capability) bool { return false }
func (m *mockController) StreamProfileType() reflect.Type {
	return reflect.TypeOf(youtube.StreamProfile{})
}

func (m *mockController) GetStreams(ctx context.Context) ([]streamcontrol.StreamInfo, error) {
	return nil, nil
}
func (m *mockController) InsertAdsCuePoint(ctx context.Context, streamSourceID streamcontrol.StreamID, ts time.Time, duration time.Duration) error {
	return nil
}
func (m *mockController) Flush(ctx context.Context, streamSourceID streamcontrol.StreamID) error {
	return nil
}
func (m *mockController) GetChatMessagesChan(ctx context.Context, streamID streamcontrol.StreamID) (<-chan streamcontrol.Event, error) {
	return nil, nil
}
func (m *mockController) SendChatMessage(ctx context.Context, streamID streamcontrol.StreamID, message string) error {
	return nil
}
func (m *mockController) RemoveChatMessage(ctx context.Context, streamID streamcontrol.StreamID, messageID streamcontrol.EventID) error {
	return nil
}
func (m *mockController) BanUser(ctx context.Context, streamID streamcontrol.StreamID, userID streamcontrol.UserID, reason string, deadline time.Time) error {
	return nil
}
func (m *mockController) IsChannelStreaming(ctx context.Context, chanID streamcontrol.UserID) (bool, error) {
	return false, nil
}
func (m *mockController) Shoutout(ctx context.Context, streamID streamcontrol.StreamID, chanID streamcontrol.UserID) error {
	return nil
}
func (m *mockController) RaidTo(ctx context.Context, streamID streamcontrol.StreamID, chanID streamcontrol.UserID) error {
	return nil
}

func TestStreamDGetActiveAccountIDsInheritance(t *testing.T) {
	acc1ID := streamcontrol.AccountID("acc1")
	streamID1 := streamcontrol.NewStreamIDFullyQualified(youtube.ID, acc1ID, streamcontrol.DefaultStreamID)
	d := &StreamD{
		ActiveProfiles: make(map[streamcontrol.StreamIDFullyQualified]streamcontrol.ProfileName),
		Config: config.Config{
			Backends: streamcontrol.Config{
				youtube.ID: &streamcontrol.AbstractPlatformConfig{
					Accounts: map[streamcontrol.AccountID]streamcontrol.RawMessage{
						acc1ID: streamcontrol.ToRawMessage(youtube.AccountConfig{
							AccountConfigBase: streamcontrol.AccountConfigBase[youtube.StreamProfile]{
								StreamProfiles: map[streamcontrol.StreamID]streamcontrol.StreamProfiles[youtube.StreamProfile]{
									streamcontrol.DefaultStreamID: {
										"parent": {},
										"child": {
											StreamProfileBase: streamcontrol.StreamProfileBase{
												Parent: "parent",
											},
										},
									},
								},
							},
						}),
					},
				},
			},
		},
		AccountMap: Accounts{
			streamcontrol.NewAccountIDFullyQualified(youtube.ID, "acc1"): streamcontrol.ToAbstractAccount(&mockController{}),
			streamcontrol.NewAccountIDFullyQualified(youtube.ID, "acc2"): streamcontrol.ToAbstractAccount(&mockController{}),
		},
		EventBus: eventbus.New(),
	}

	ctx := context.Background()

	// Child should inherit AccountIDs from parent
	d.ActiveProfiles[streamID1] = "child"
	ids, err := d.GetActiveStreamIDs(ctx)
	require.NoError(t, err)
	require.Len(t, ids, 1)
	require.Equal(t, streamcontrol.AccountID("acc1"), ids[0].AccountID)
}

func TestStreamDApplyProfileDeactivation(t *testing.T) {
	mockServer := &mockStreamServer{
		Sinks: []types.StreamSink{
			{
				ID: types.StreamSinkIDFullyQualified{Type: types.StreamSinkTypeExternalPlatform, ID: "d1"},
				StreamID: &streamcontrol.StreamIDFullyQualified{
					AccountIDFullyQualified: streamcontrol.AccountIDFullyQualified{
						PlatformID: youtube.ID,
						AccountID:  "acc1",
					},
					StreamID: streamcontrol.DefaultStreamID,
				},
			},
			{
				ID: types.StreamSinkIDFullyQualified{Type: types.StreamSinkTypeExternalPlatform, ID: "d2"},
				StreamID: &streamcontrol.StreamIDFullyQualified{
					AccountIDFullyQualified: streamcontrol.AccountIDFullyQualified{
						PlatformID: youtube.ID,
						AccountID:  "acc2",
					},
					StreamID: streamcontrol.DefaultStreamID,
				},
			},
		},
		Forwards: []streamforward.StreamForward{
			{
				StreamSourceID: "s1",
				StreamSinkID:   types.StreamSinkIDFullyQualified{Type: types.StreamSinkTypeExternalPlatform, ID: "d1"},
				Enabled:        true,
			},
			{
				StreamSourceID: "s1",
				StreamSinkID:   types.StreamSinkIDFullyQualified{Type: types.StreamSinkTypeExternalPlatform, ID: "d2"},
				Enabled:        true,
			},
		},
	}
	onlyAcc1Profile := youtube.StreamProfile{}
	allProfile := youtube.StreamProfile{}
	d := &StreamD{
		ActiveProfiles: make(map[streamcontrol.StreamIDFullyQualified]streamcontrol.ProfileName),
		Config: config.Config{
			Backends: streamcontrol.Config{
				youtube.ID: &streamcontrol.AbstractPlatformConfig{
					Accounts: map[streamcontrol.AccountID]streamcontrol.RawMessage{
						"acc1": streamcontrol.ToRawMessage(youtube.AccountConfig{
							AccountConfigBase: streamcontrol.AccountConfigBase[youtube.StreamProfile]{
								Enable: ptr(true),
								StreamProfiles: map[streamcontrol.StreamID]streamcontrol.StreamProfiles[youtube.StreamProfile]{
									streamcontrol.DefaultStreamID: {
										"only-acc1": onlyAcc1Profile,
										"all":       allProfile,
									},
								},
							},
						}),
						"acc2": streamcontrol.ToRawMessage(youtube.AccountConfig{
							AccountConfigBase: streamcontrol.AccountConfigBase[youtube.StreamProfile]{
								Enable: ptr(true),
								StreamProfiles: map[streamcontrol.StreamID]streamcontrol.StreamProfiles[youtube.StreamProfile]{
									streamcontrol.DefaultStreamID: {
										"all": allProfile,
									},
								},
							},
						}),
					},
				},
			},
		},
		StreamServer: mockServer,
		AccountMap: Accounts{
			streamcontrol.NewAccountIDFullyQualified(youtube.ID, "acc1"): streamcontrol.ToAbstractAccount(&mockController{}),
			streamcontrol.NewAccountIDFullyQualified(youtube.ID, "acc2"): streamcontrol.ToAbstractAccount(&mockController{}),
		},
		EventBus: eventbus.New(),
	}

	ctx := context.Background()

	// Applying "only-acc1" should disable acc2
	d.ActiveProfiles[streamcontrol.NewStreamIDFullyQualified(youtube.ID, "acc1", streamcontrol.DefaultStreamID)] = "only-acc1"
	err := d.ApplyProfile(ctx, streamcontrol.NewStreamIDFullyQualified(youtube.ID, "acc1", streamcontrol.DefaultStreamID), streamcontrol.ToRawMessage(map[string]any{"enable": true}))
	require.NoError(t, err)
	err = d.ApplyProfile(ctx, streamcontrol.NewStreamIDFullyQualified(youtube.ID, "acc2", streamcontrol.DefaultStreamID), streamcontrol.ToRawMessage(map[string]any{"enable": false}))
	require.NoError(t, err)

	forwards, _ := mockServer.ListStreamForwards(ctx)
	require.True(t, forwards[0].Enabled, "acc1 should stay enabled")
	require.False(t, forwards[1].Enabled, "acc2 should be disabled")

	// Applying "all" should re-enable acc2
	d.ActiveProfiles[streamcontrol.NewStreamIDFullyQualified(youtube.ID, "acc2", streamcontrol.DefaultStreamID)] = "all"
	err = d.ApplyProfile(ctx, streamcontrol.NewStreamIDFullyQualified(youtube.ID, "acc2", streamcontrol.DefaultStreamID), streamcontrol.ToRawMessage(map[string]any{"enable": true}))
	require.NoError(t, err)

	forwards, _ = mockServer.ListStreamForwards(ctx)
	require.True(t, forwards[0].Enabled, "acc1 should stay enabled")
	require.True(t, forwards[1].Enabled, "acc2 should be re-enabled")
}
func TestStreamDListStreamSinksDynamic(t *testing.T) {
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

	d, err := New(cfg, &mockUI{}, func(context.Context, config.Config) error { return nil }, belt.New())
	require.NoError(t, err)

	d.AddOAuthListenPort(8091)

	ctx, cancel := context.WithCancel(observability.WithSecretsProvider(context.Background(), &observability.SecretsStaticProvider{}))
	defer cancel()
	err = d.Run(ctx)
	require.NoError(t, err)

	sinks, err := d.ListStreamSinks(ctx)
	require.NoError(t, err)

	foundTwitch := false
	for _, sink := range sinks {
		if sink.URL == "rtmp://live.twitch.tv/app/" && sink.StreamKey.Get() == "mock_stream_key" {
			foundTwitch = true
		}
	}
	require.True(t, foundTwitch, "Twitch dynamic sink not found")
}

func TestStreamDGetActiveStreamIDsBasic(t *testing.T) {
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

	d, err := New(cfg, &mockUI{}, func(context.Context, config.Config) error { return nil }, belt.New())
	require.NoError(t, err)

	d.AddOAuthListenPort(8091)

	ctx, cancel := context.WithCancel(observability.WithSecretsProvider(context.Background(), &observability.SecretsStaticProvider{}))
	defer cancel()
	err = d.Run(ctx)
	require.NoError(t, err)

	ids, err := d.GetActiveStreamIDs(ctx)
	require.NoError(t, err)

	require.Greater(t, len(ids), 0)
	found := false
	for _, id := range ids {
		if id.PlatformID == twitch.ID && id.AccountID == "test" {
			found = true
		}
	}
	require.True(t, found, "Twitch active stream source ID not found")
}
func TestStreamDInitArchitecture(t *testing.T) {
	cfg := config.NewConfig()
	d, err := New(cfg, &mockUI{}, nil, belt.New())
	require.NoError(t, err)
	require.NotNil(t, d)
	require.NotNil(t, d.EventBus)

	ctx := context.Background()
	err = d.SetLoggingLevel(ctx, logger.LevelDebug)
	require.NoError(t, err)
	level, err := d.GetLoggingLevel(ctx)
	require.NoError(t, err)
	require.Equal(t, logger.LevelDebug, level)

	err = d.InitCache(ctx)
	require.NoError(t, err)
}

func TestStreamDRunArchitecture(t *testing.T) {
	cfg := config.NewConfig()
	d, err := New(cfg, &mockUI{}, func(ctx context.Context, config config.Config) error { return nil }, belt.New())
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx = observability.WithSecretsProvider(ctx, &observability.SecretsStaticProvider{})

	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	err = d.Run(ctx)
	if err != nil {
		require.ErrorIs(t, err, context.Canceled)
	}
}

func TestStreamDVariables(t *testing.T) {
	cfg := config.NewConfig()
	d, _ := New(cfg, &mockUI{}, nil, belt.New())
	ctx := context.Background()

	key := streamdconsts.VarKey("test-key")
	err := d.SetVariable(ctx, key, api.VariableValue("test-value"))
	require.NoError(t, err)

	val, err := d.GetVariable(ctx, key)
	require.NoError(t, err)
	require.Equal(t, api.VariableValue("test-value"), val)

	hash, err := d.GetVariableHash(ctx, key, crypto.SHA1)
	require.NoError(t, err)
	require.NotEmpty(t, hash)
}

func TestStreamDStreamServer(t *testing.T) {
	cfg := config.NewConfig()
	d, _ := New(cfg, &mockUI{}, nil, belt.New())
	ctx := context.Background()

	err := d.initStreamServer(ctx)
	require.NoError(t, err)
	require.NotNil(t, d.StreamServer)
}

func TestStreamDGitSync(t *testing.T) {
	cfg := config.NewConfig()
	cfg.GitRepo.URL = "git@github.com:user/repo.git"

	d, err := New(cfg, &mockUI{}, nil, belt.New())
	require.NoError(t, err)
	require.Equal(t, "git@github.com:user/repo.git", d.Config.GitRepo.URL)
}

func TestStreamDRunNoDeadlock(t *testing.T) {
	// Re-register kick to make sure it's available
	kick.SetDebugUseMockClient(true)

	cfg := config.Config{
		Backends: streamcontrol.Config{
			kick.ID: &streamcontrol.AbstractPlatformConfig{
				Accounts: map[streamcontrol.AccountID]streamcontrol.RawMessage{
					"test": streamcontrol.ToRawMessage(kick.AccountConfig{
						AccountConfigBase: streamcontrol.AccountConfigBase[kick.StreamProfile]{
							Enable: ptr(true),
						},
						Channel: "test",
					}),
				},
			},
		},
	}

	d, err := New(cfg, &mockUI{}, func(context.Context, config.Config) error { return nil }, belt.New())
	require.NoError(t, err)

	ctx := observability.WithSecretsProvider(context.Background(), &observability.SecretsStaticProvider{})
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	// This should not deadlock
	err = d.Run(ctx)
	require.NoError(t, err)
}
func TestStreamDChat_StreamD(t *testing.T) {
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

	d, err := New(cfg, &mockUI{}, nil, belt.New())
	require.NoError(t, err)

	d.AddOAuthListenPort(8093)

	ctx, cancel := context.WithCancel(observability.WithSecretsProvider(context.Background(), &observability.SecretsStaticProvider{}))
	defer cancel()
	err = d.Run(ctx)
	require.NoError(t, err)

	// Wait for message to be processed (mocked)
	ch, err := d.SubscribeToChatMessages(ctx, time.Now().Add(-time.Hour), 10)
	require.NoError(t, err)

	// Send a chat message
	err = d.SendChatMessage(ctx, twitch.ID, "Hello world!")
	require.NoError(t, err)

	// Mock Twitch client automatically adds messages when SendChatMessage is called in mock mode
	select {
	case msg := <-ch:
		require.NotNil(t, msg.Message)
		require.Equal(t, "Hello world!", msg.Message.Content)
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for chat message")
	}
}
