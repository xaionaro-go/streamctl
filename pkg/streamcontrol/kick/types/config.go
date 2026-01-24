package kick

import (
	"context"
	"time"

	"github.com/xaionaro-go/streamctl/pkg/buildvars"
	"github.com/xaionaro-go/streamctl/pkg/oauthhandler"
	"github.com/xaionaro-go/streamctl/pkg/secret"
	streamctl "github.com/xaionaro-go/streamctl/pkg/streamcontrol"
)

const (
	ID             = streamctl.PlatformID("kick")
	MaxTitleLength = 100
)

type OAuthHandler func(context.Context, oauthhandler.OAuthHandlerArgument) error

type AccountConfig struct {
	streamctl.AccountConfigBase[StreamProfile] `yaml:",inline"`

	Channel      string        `yaml:"channel"`
	ClientID     string        `yaml:"client_id"`
	ClientSecret secret.String `yaml:"client_secret"`

	UserAccessToken          secret.String `yaml:"user_access_token"`
	UserAccessTokenExpiresAt time.Time     `yaml:"user_access_token_expires_at,omitempty"`
	RefreshToken             secret.String `yaml:"refresh_token"`

	CustomOAuthHandler  OAuthHandler    `yaml:"-"`
	GetOAuthListenPorts func() []uint16 `yaml:"-"`
}

type Config = streamctl.PlatformConfig[AccountConfig, StreamProfile]

func InitConfig(cfg streamctl.Config) {
	streamctl.InitConfig(cfg, ID, Config{})
}

func (cfg AccountConfig) IsInitialized() bool {
	return cfg.Channel != "" &&
		valueOrDefault(cfg.ClientID, buildvars.KickClientID) != "" &&
		valueOrDefault(cfg.ClientSecret.Get(), buildvars.KickClientSecret) != ""
}

type StreamProfile struct {
	streamctl.StreamProfileBase `yaml:",inline"`

	CategoryID *uint64
}
