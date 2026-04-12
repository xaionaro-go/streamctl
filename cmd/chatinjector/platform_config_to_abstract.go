package main

import (
	"fmt"

	"github.com/xaionaro-go/streamctl/pkg/secret"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	kicktypes "github.com/xaionaro-go/streamctl/pkg/streamcontrol/kick/types"
	twitchtypes "github.com/xaionaro-go/streamctl/pkg/streamcontrol/twitch/types"
)

// toAbstractPlatformConfig converts the chatinjector's flat PlatformConfig
// into an AbstractPlatformConfig suitable for the shared ChatListenerFactory.
// YouTube is not supported here because it uses channel monitoring logic
// that does not map to a single AbstractPlatformConfig.
func toAbstractPlatformConfig(
	pc PlatformConfig,
) (*streamcontrol.AbstractPlatformConfig, error) {
	switch pc.Type {
	case "twitch":
		return toAbstractTwitchConfig(pc), nil
	case "kick":
		return toAbstractKickConfig(pc), nil
	default:
		return nil, fmt.Errorf("toAbstractPlatformConfig: unsupported platform %q", pc.Type)
	}
}

func toAbstractTwitchConfig(pc PlatformConfig) *streamcontrol.AbstractPlatformConfig {
	return &streamcontrol.AbstractPlatformConfig{
		Config: twitchtypes.PlatformSpecificConfig{
			Channel:         pc.Channel,
			ClientID:        pc.ClientID,
			ClientSecret:    secret.New(pc.ClientSecret),
			UserAccessToken: secret.New(pc.AccessToken),
		},
	}
}

func toAbstractKickConfig(pc PlatformConfig) *streamcontrol.AbstractPlatformConfig {
	cfg := &streamcontrol.AbstractPlatformConfig{
		Config: kicktypes.PlatformSpecificConfig{},
	}
	if pc.ChatWebhookAddr != "" {
		cfg.Custom = map[string]any{
			"chatwebhook_addr": pc.ChatWebhookAddr,
		}
	}
	return cfg
}
