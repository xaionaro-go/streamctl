//go:build test_integration

package streamd

import (
	"context"
	"testing"
	"time"

	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/streamctl/pkg/oauthhandler"
	"github.com/xaionaro-go/streamctl/pkg/secret"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/kick"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/obs"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/twitch"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/youtube"
	"github.com/xaionaro-go/streamctl/pkg/streamd/api"
	"github.com/xaionaro-go/streamctl/pkg/streamd/config"
	"github.com/xaionaro-go/streamctl/pkg/streamd/ui"
	sstypes "github.com/xaionaro-go/streamctl/pkg/streamserver/types"
	"golang.org/x/oauth2"
)

type mockUIIntegration struct{}

func (m *mockUIIntegration) SetStatus(string) {}
func (m *mockUIIntegration) DisplayError(err error) {
	if err != nil {
		logger.Default().Errorf("UI Error: %v", err)
	}
}
func (m *mockUIIntegration) OpenBrowser(context.Context, string) error {
	return nil
}
func (m *mockUIIntegration) OAuthHandler(context.Context, streamcontrol.PlatformID, oauthhandler.OAuthHandlerArgument) error {
	return nil
}
func (m *mockUIIntegration) OnSubmittedOAuthCode(context.Context, streamcontrol.PlatformID, string) error {
	return nil
}
func (m *mockUIIntegration) SetLoggingLevel(context.Context, logger.Level) {}

var _ ui.UI = (*mockUIIntegration)(nil)

func TestStreamDIntegrationFull(t *testing.T) {
	// Enable mocks
	youtube.SetDebugUseMockClient(true)
	twitch.SetDebugUseMockClient(true)
	kick.SetDebugUseMockClient(true)
	obs.SetDebugUseMockClient(true)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	mServer := &mockStreamServer{}

	cfg := config.Config{
		Backends: streamcontrol.Config{
			youtube.ID: &streamcontrol.AbstractPlatformConfig{
				Accounts: map[streamcontrol.AccountID]streamcontrol.RawMessage{
					"acc1": streamcontrol.ToRawMessage(youtube.PlatformSpecificConfig{
						ClientID:     "dummy1",
						ClientSecret: secret.New("dummy1"),
						Token: ptr(secret.New(oauth2.Token{
							AccessToken: "mock-token-1",
							Expiry:      time.Now().Add(time.Hour),
						})),
					}),
					"acc2": streamcontrol.ToRawMessage(youtube.PlatformSpecificConfig{
						ClientID:     "dummy2",
						ClientSecret: secret.New("dummy2"),
						Token: ptr(secret.New(oauth2.Token{
							AccessToken: "mock-token-2",
							Expiry:      time.Now().Add(time.Hour),
						})),
					}),
				},
				StreamProfiles: streamcontrol.ToStreamProfiles(map[streamcontrol.ProfileName]youtube.StreamProfile{
					"standard": {
						StreamProfileBase: streamcontrol.StreamProfileBase{
							Title:       "Stream A",
							Description: "Description A",
							Streams: map[streamcontrol.StreamIDFullyQualified]any{
								streamcontrol.StreamIDFullyQualified{AccountIDFullyQualified: streamcontrol.AccountIDFullyQualified{PlatformID: youtube.ID, AccountID: "acc1"}}: streamcontrol.ToRawMessage(youtube.StreamProfile{
									StreamProfileBase: streamcontrol.StreamProfileBase{
										Title:       "Stream A",
										Description: "Description A",
									},
									TemplateBroadcastIDs: []string{"template-1"},
								}),
								streamcontrol.StreamIDFullyQualified{AccountIDFullyQualified: streamcontrol.AccountIDFullyQualified{PlatformID: youtube.ID, AccountID: "acc2"}, StreamID: "stream-key-B"}: streamcontrol.ToRawMessage(youtube.StreamProfile{
									StreamProfileBase: streamcontrol.StreamProfileBase{
										Title:       "Stream A",
										Description: "Description A",
									},
									TemplateBroadcastIDs: []string{"template-1"},
								}),
							},
						},
						TemplateBroadcastIDs: []string{"template-1"},
					},
				}),
			},
			twitch.ID: &streamcontrol.AbstractPlatformConfig{
				Accounts: map[streamcontrol.AccountID]streamcontrol.RawMessage{
					"default": streamcontrol.ToRawMessage(twitch.PlatformSpecificConfig{
						Channel:      "dummy",
						ClientID:     "dummy",
						ClientSecret: secret.New("dummy"),
						AuthType:     "user",
					}),
				},
				StreamProfiles: streamcontrol.ToStreamProfiles(map[streamcontrol.ProfileName]twitch.StreamProfile{
					"standard": {
						StreamProfileBase: streamcontrol.StreamProfileBase{
							Streams: map[streamcontrol.StreamIDFullyQualified]any{
								streamcontrol.StreamIDFullyQualified{AccountIDFullyQualified: streamcontrol.AccountIDFullyQualified{PlatformID: twitch.ID, AccountID: "default"}}: streamcontrol.ToRawMessage(streamcontrol.StreamProfileBase{}),
							},
						},
					},
				}),
			},
			kick.ID: &streamcontrol.AbstractPlatformConfig{
				Accounts: map[streamcontrol.AccountID]streamcontrol.RawMessage{
					"default": streamcontrol.ToRawMessage(kick.PlatformSpecificConfig{
						Channel: "dummy",
					}),
				},
				StreamProfiles: streamcontrol.ToStreamProfiles(map[streamcontrol.ProfileName]kick.StreamProfile{
					"standard": {
						StreamProfileBase: streamcontrol.StreamProfileBase{
							Streams: map[streamcontrol.StreamIDFullyQualified]any{
								streamcontrol.StreamIDFullyQualified{AccountIDFullyQualified: streamcontrol.AccountIDFullyQualified{PlatformID: kick.ID, AccountID: "default"}}: streamcontrol.ToRawMessage(streamcontrol.StreamProfileBase{}),
							},
						},
					},
				}),
			},
			obs.ID: &streamcontrol.AbstractPlatformConfig{
				Accounts: map[streamcontrol.AccountID]streamcontrol.RawMessage{
					"default": streamcontrol.ToRawMessage(obs.PlatformSpecificConfig{
						Host: "localhost",
						Port: 4455,
					}),
				},
				StreamProfiles: streamcontrol.ToStreamProfiles(map[streamcontrol.ProfileName]obs.StreamProfile{
					"standard": {
						StreamProfileBase: streamcontrol.StreamProfileBase{
							Streams: map[streamcontrol.StreamIDFullyQualified]any{
								streamcontrol.StreamIDFullyQualified{AccountIDFullyQualified: streamcontrol.AccountIDFullyQualified{PlatformID: obs.ID, AccountID: "default"}}: streamcontrol.ToRawMessage(streamcontrol.StreamProfileBase{}),
							},
						},
					},
				}),
			},
		},
	}

	d, err := New(cfg, &mockUIIntegration{}, func(context.Context, config.Config) error { return nil }, belt.New())
	require.Nil(t, err)

	d.AddOAuthListenPort(8091)
	d.AddOAuthListenPort(8092)
	d.AddOAuthListenPort(8093)
	d.AddOAuthListenPort(8094)
	d.StreamServer = mServer

	// 1. Initialize backends
	platforms := []streamcontrol.PlatformID{youtube.ID, twitch.ID, kick.ID, obs.ID}
	_ = platforms
	sID := streamcontrol.StreamID("")
	_ = sID
	err = d.EXPERIMENTAL_ReinitStreamControllers(ctx)
	require.NoError(t, err, "failed to init backends")

	// 2. Add forwards for two independent streams
	streamIDA := api.StreamSourceID("stream-A")
	streamIDB := api.StreamSourceID("stream-B")

	// Stream A: YouTube (acc1), Twitch, Kick, OBS
	for _, platID := range platforms {
		accID := streamcontrol.AccountID("default")
		if platID == youtube.ID {
			accID = "acc1"
		}
		sinkID := api.StreamSinkID("sink-A-" + string(platID))
		err := d.AddStreamSink(ctx, sinkID, sstypes.StreamSinkConfig{
			StreamSourceID: &streamcontrol.StreamIDFullyQualified{
				AccountIDFullyQualified: streamcontrol.AccountIDFullyQualified{
					PlatformID: platID,
					AccountID:  accID,
				},
			},
		})
		require.NoError(t, err)
		err = d.AddStreamForward(ctx, streamIDA, sinkID, true, sstypes.EncodeConfig{}, api.StreamForwardingQuirks{})
		require.NoError(t, err)
	}

	// Stream B: YouTube (acc2)
	sinkIDB := api.StreamSinkID("sink-B-youtube")
	err = d.AddStreamSink(ctx, sinkIDB, sstypes.StreamSinkConfig{
		StreamSourceID: &streamcontrol.StreamIDFullyQualified{
			AccountIDFullyQualified: streamcontrol.AccountIDFullyQualified{
				PlatformID: youtube.ID,
				AccountID:  "acc2",
			},
			StreamID: "stream-key-B",
		},
	})
	require.NoError(t, err)
	err = d.AddStreamForward(ctx, streamIDB, sinkIDB, true, sstypes.EncodeConfig{}, api.StreamForwardingQuirks{})
	require.NoError(t, err)

	// 3. Start Stream A on all platforms
	for _, platID := range platforms {
		accID := streamcontrol.AccountID("default")
		if platID == youtube.ID {
			accID = "acc1"
		}
		if platID == youtube.ID {
		}
		profile := d.Config.Backends[platID].StreamProfiles["standard"]
		err := d.StartStream(ctx, platID, "", "", profile)
		require.NoError(t, err, "failed to start stream A on %s:%s", platID, accID)
	}

	// 5. Verify status
	for _, platID := range platforms {
		accID := streamcontrol.AccountID("default")
		if platID == youtube.ID {
			accID = "acc1"
		}
		status, err := d.GetStreamStatus(ctx, platID)
		require.NoError(t, err)
		require.True(t, status.IsActive, "stream A should be active on %s:%s", platID, accID)
	}

	// 6. Update Title on Stream A
	newTitleA := "Updated Stream A Title"
	for _, platID := range platforms {
		err := d.SetTitle(ctx, platID, newTitleA)
		require.NoError(t, err)
	}

	// 7. End Stream A
	for _, platID := range platforms {
		err := d.EndStream(ctx, platID)
		require.NoError(t, err)
		if platID == twitch.ID {
			twitch.SetMockIsLive(false)
		}
		if platID == kick.ID {
			kick.SetMockIsLive(false)
		}
	}

	// 8. Verify Stream A is stopped
	for _, platID := range platforms {
		accID := streamcontrol.AccountID("default")
		if platID == youtube.ID {
			accID = "acc1"
		}
		status, err := d.GetStreamStatus(ctx, platID)
		require.NoError(t, err)
		require.False(t, status.IsActive, "stream A should be stopped on %s:%s", platID, accID)
	}
}
