package streamd

import (
	"context"
	"testing"

	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/streamctl/pkg/oauthhandler"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/obs"
	"github.com/xaionaro-go/streamctl/pkg/streamd/config"
)

type mockUI2 struct{}

func (ui *mockUI2) SetStatus(string)   {}
func (ui *mockUI2) DisplayError(error) {}
func (ui *mockUI2) OAuthHandler(ctx context.Context, platID streamcontrol.PlatformID, arg oauthhandler.OAuthHandlerArgument) error {
	return nil
}
func (ui *mockUI2) OpenBrowser(ctx context.Context, url string) error { return nil }
func (ui *mockUI2) OnSubmittedOAuthCode(ctx context.Context, platID streamcontrol.PlatformID, code string) error {
	return nil
}
func (ui *mockUI2) SetLoggingLevel(ctx context.Context, level logger.Level) {}

func TestStreamDApplyProfileWithSubProfiles(t *testing.T) {
	platID := obs.ID
	accID1 := streamcontrol.AccountID("acc1")
	accID2 := streamcontrol.AccountID("acc2")

	cfg := config.Config{
		Backends: streamcontrol.Config{
			platID: &streamcontrol.AbstractPlatformConfig{
				Accounts: map[streamcontrol.AccountID]streamcontrol.RawMessage{
					accID1: streamcontrol.ToRawMessage(obs.AccountConfig{
						AccountConfigBase: streamcontrol.AccountConfigBase[obs.StreamProfile]{
							Enable: ptr(true),
							StreamProfiles: map[streamcontrol.StreamID]streamcontrol.StreamProfiles[obs.StreamProfile]{
								streamcontrol.DefaultStreamID: {
									"HQ": {
										StreamProfileBase: streamcontrol.StreamProfileBase{
											Title: "High Quality",
										},
										EnableRecording: true,
									},
								},
							},
						},
						Host: "localhost",
						Port: 4455,
					}),
					accID2: streamcontrol.ToRawMessage(obs.AccountConfig{
						AccountConfigBase: streamcontrol.AccountConfigBase[obs.StreamProfile]{
							Enable: ptr(true),
						},
						Host: "localhost",
						Port: 4456,
					}),
				},
			},
		},
	}

	d, err := New(cfg, &mockUI2{}, nil, belt.New())
	require.NoError(t, err)

	ctx := context.Background()
	err = d.EXPERIMENTAL_ReinitStreamControllers(ctx)
	require.NoError(t, err)

	// Define a profile that has a sub-profile for acc1
	streamID1 := streamcontrol.NewStreamIDFullyQualified(platID, accID1, streamcontrol.DefaultStreamID)
	profile := streamcontrol.ToRawMessage(map[string]any{
		"streams": map[string]any{
			streamID1.String(): map[string]any{
				"parent": "HQ",
			},
		},
	})

	// Apply the profile.
	err = d.ApplyProfile(ctx, streamID1, profile)
	require.NoError(t, err)
}
