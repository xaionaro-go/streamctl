package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/goccy/go-yaml"
	"github.com/xaionaro-go/streamctl/pkg/secret"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	kicktypes "github.com/xaionaro-go/streamctl/pkg/streamcontrol/kick/types"
	twitchtypes "github.com/xaionaro-go/streamctl/pkg/streamcontrol/twitch/types"
	yttypes "github.com/xaionaro-go/streamctl/pkg/streamcontrol/youtube/types"
	"github.com/xaionaro-go/xpath"
)

const (
	defaultConfigPath      = "~/.config/chatinjector/chatinjector.yaml"
	defaultChatHistorySize = 20
)

// AppConfig is the chatinjector configuration. Platform selection and
// listener type come from CLI flags (--platform, --listener-type), not
// from the config file. The config provides streamd address, translation
// settings, and optional per-platform credential/setting overrides.
type AppConfig struct {
	StreamdAddr       string                         `yaml:"streamd_addr"`
	Translation       TranslationConfig              `yaml:"translation"`
	PlatformOverrides map[string]PlatformOverrideConfig `yaml:"platform_overrides"`
}

// PlatformOverrideConfig provides optional credential and setting overrides
// for a platform. When present, these values supplement or replace what
// FetchPlatformConfig retrieves from streamd.
type PlatformOverrideConfig struct {
	// Twitch fields.
	Channel      string `yaml:"channel,omitempty"`
	ClientID     string `yaml:"client_id,omitempty"`
	ClientSecret string `yaml:"client_secret,omitempty"`
	AccessToken  string `yaml:"access_token,omitempty"`

	// Kick fields.
	ChatWebhookAddr string `yaml:"chat_webhook_addr,omitempty"`

	// YouTube fields.
	ProxyAddr string `yaml:"proxy_addr,omitempty"`
	Video     string `yaml:"video,omitempty"`
}

// applyTo merges override values into an existing AbstractPlatformConfig.
// Fields that are already set in the override take precedence.
func (o PlatformOverrideConfig) applyTo(
	platCfg *streamcontrol.AbstractPlatformConfig,
) *streamcontrol.AbstractPlatformConfig {
	if platCfg.Custom == nil {
		platCfg.Custom = map[string]any{}
	}

	// Apply Twitch-specific overrides.
	if twitchCfg, ok := platCfg.Config.(twitchtypes.PlatformSpecificConfig); ok {
		if o.Channel != "" {
			twitchCfg.Channel = o.Channel
		}
		if o.ClientID != "" {
			twitchCfg.ClientID = o.ClientID
		}
		if o.ClientSecret != "" {
			twitchCfg.ClientSecret = secret.New(o.ClientSecret)
		}
		if o.AccessToken != "" {
			twitchCfg.UserAccessToken = secret.New(o.AccessToken)
		}
		platCfg.Config = twitchCfg
	}

	// Apply Kick-specific overrides.
	if _, ok := platCfg.Config.(kicktypes.PlatformSpecificConfig); ok {
		if o.ChatWebhookAddr != "" {
			platCfg.Custom["chatwebhook_addr"] = o.ChatWebhookAddr
		}
	}

	// Apply YouTube-specific overrides.
	if ytCfg, ok := platCfg.Config.(yttypes.PlatformSpecificConfig); ok {
		if o.ProxyAddr != "" {
			ytCfg.YTProxyAddr = o.ProxyAddr
		}
		platCfg.Config = ytCfg
	}
	if o.Video != "" {
		platCfg.Custom["video_id"] = o.Video
	}

	return platCfg
}

type TranslationConfig struct {
	TargetLanguage  string           `yaml:"target_language"`
	ChatHistorySize int              `yaml:"chat_history_size"`
	Providers       []ProviderConfig `yaml:"providers"`
}

type ProviderConfig struct {
	Type                    string        `yaml:"type"` // ollama, openai, anthropic, claude-code, streamdcfg, streampanelcfg
	APIURL                  string        `yaml:"api_url"`
	APIKey                  string        `yaml:"api_key"`
	Model                   string        `yaml:"model"`
	Parallelism             int           `yaml:"parallelism"`
	MaxQueueSize            int           `yaml:"max_queue_size"`            // max pending translations waiting for this provider; 0 = no queueing
	Timeout                 time.Duration `yaml:"timeout"`                   // per-request timeout; 0 means no timeout
	ConfigPath              string        `yaml:"config_path"`               // for streamdcfg/streampanelcfg: path to YAML config
	Effort                  string        `yaml:"effort"`                    // for claude-code: low, medium, high, max (default: low)
	CircuitBreakerThreshold int64         `yaml:"circuit_breaker_threshold"` // consecutive failures to open circuit (default: 3)
	CircuitBreakerCooldown  time.Duration `yaml:"circuit_breaker_cooldown"`  // cooldown before probing again (default: 30s)
}

func loadConfig(
	ctx context.Context,
	configPath string,
) (_ret AppConfig, _err error) {
	logger.Tracef(ctx, "loadConfig")
	defer func() { logger.Tracef(ctx, "/loadConfig: %v", _err) }()

	expandedPath, err := xpath.Expand(configPath)
	if err != nil {
		return AppConfig{}, fmt.Errorf("expand config path %q: %w", configPath, err)
	}

	data, err := os.ReadFile(expandedPath)
	if os.IsNotExist(err) {
		if writeErr := writeSampleConfig(ctx, expandedPath); writeErr != nil {
			return AppConfig{}, fmt.Errorf("write sample config to %q: %w", expandedPath, writeErr)
		}
		return AppConfig{}, fmt.Errorf("config not found; wrote sample config to %s — edit it and restart", expandedPath)
	}
	if err != nil {
		return AppConfig{}, fmt.Errorf("read config %q: %w", expandedPath, err)
	}

	var cfg AppConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return AppConfig{}, fmt.Errorf("parse config %q: %w", expandedPath, err)
	}

	if cfg.Translation.ChatHistorySize <= 0 {
		cfg.Translation.ChatHistorySize = defaultChatHistorySize
	}

	logger.Debugf(ctx, "loaded config: streamd=%s overrides=%d providers=%d",
		cfg.StreamdAddr, len(cfg.PlatformOverrides), len(cfg.Translation.Providers))

	return cfg, nil
}

const sampleConfig = `# chatinjector configuration
# See: https://github.com/xaionaro-go/streamctl/tree/main/cmd/chatinjector
#
# Usage: chatinjector --platform twitch --listener-type primary --streamd-addr localhost:3594
#
# One platform, one listener type, one process. To run multiple, launch
# multiple instances.

# streamd gRPC address (can also be set via --streamd-addr flag)
streamd_addr: "localhost:3594"

# Optional per-platform credential overrides. If omitted, credentials are
# fetched from streamd via FetchPlatformConfig.
# platform_overrides:
#   twitch:
#     channel: "xqc"
#     client_id: "your_client_id"
#     client_secret: "your_client_secret"
#     access_token: "your_access_token"
#
#   kick:
#     chat_webhook_addr: "localhost:9091"
#
#   youtube:
#     proxy_addr: "localhost:9090"
#     video: "VIDEO_ID_OR_URL"

# Translation configuration (remove this section to disable)
# translation:
#   target_language: "en"
#   chat_history_size: 20
#
#   # Providers are tried in order (fallback chain).
#   providers:
#     # Import LLM endpoints from streampanel/streamd config:
#     # - type: streampanelcfg
#     #   config_path: "~/.streampanel.yaml"
#     #   parallelism: 2
#
#     - type: ollama
#       api_url: "http://localhost:11434"
#       model: "qwen3:30b-instruct"
#       parallelism: 2
#
#     # - type: openai
#     #   api_url: "https://api.openai.com"
#     #   api_key: "sk-..."
#     #   model: "gpt-4o"
#     #   parallelism: 4
#
#     # - type: anthropic
#     #   api_key: "sk-ant-..."
#     #   model: "claude-sonnet-4-20250514"
#     #   parallelism: 2
#
#     # - type: claude-code
#     #   model: "sonnet"        # sonnet, opus, haiku, or full model name
#     #   effort: "low"          # low, medium, high, max
#     #   parallelism: 1
`

func writeSampleConfig(
	ctx context.Context,
	path string,
) (_err error) {
	logger.Tracef(ctx, "writeSampleConfig")
	defer func() { logger.Tracef(ctx, "/writeSampleConfig: %v", _err) }()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}

	return os.WriteFile(path, []byte(sampleConfig), 0o644)
}
