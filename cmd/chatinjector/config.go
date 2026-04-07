package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/goccy/go-yaml"
	"github.com/xaionaro-go/streamctl/pkg/streamd/grpc/go/streamd_grpc"
	"github.com/xaionaro-go/xpath"
)

const (
	defaultConfigPath      = "~/.config/chatinjector/chatinjector.yaml"
	defaultChatHistorySize = 20
)

type AppConfig struct {
	StreamdAddr string            `yaml:"streamd_addr"`
	Translation TranslationConfig `yaml:"translation"`
	Platforms   []PlatformConfig  `yaml:"platforms"`
}

// PlatformConfig describes a single chat source. The Type field selects
// which ChatSource implementation to create; remaining fields are
// platform-specific and ignored when irrelevant.
type PlatformConfig struct {
	Type string `yaml:"type"` // "youtube", "twitch", "kick"

	// Credentials specifies where to get platform auth tokens.
	// "streamd" fetches from the streamd config; empty uses static fields.
	Credentials string `yaml:"credentials,omitempty"`

	// Sources specifies which source types to try, in order. If empty,
	// the platform's default source is used. Example: ["eventsub", "irc"]
	Sources []string `yaml:"sources,omitempty"`

	// YouTube fields.
	ProxyAddr    string `yaml:"proxy_addr,omitempty"`
	Video        string `yaml:"video,omitempty"`
	DetectMethod string `yaml:"detect_method,omitempty"`
	Hl           string `yaml:"hl,omitempty"`
	RawMessage   bool   `yaml:"raw_message,omitempty"`

	// Shared: YouTube uses this as the channel URL; Twitch uses it as the
	// channel name.
	Channel string `yaml:"channel,omitempty"`

	// Kick fields.
	ChatWebhookAddr string `yaml:"chat_webhook_addr,omitempty"`

	// Twitch auth (required for eventsub source).
	ClientID    string `yaml:"client_id,omitempty"`
	AccessToken string `yaml:"access_token,omitempty"`
}

// newTwitchSourceByName creates a single Twitch ChatSource by source name.
func (pc PlatformConfig) newTwitchSourceByName(name string) (ChatSource, error) {
	switch name {
	case "eventsub":
		return &TwitchEventSubSource{
			Channel:     pc.Channel,
			ClientID:    pc.ClientID,
			AccessToken: pc.AccessToken,
		}, nil
	case "irc":
		return &TwitchSource{
			Channel: pc.Channel,
		}, nil
	default:
		return nil, fmt.Errorf("unknown twitch source type %q", name)
	}
}

// NewSource creates the ChatSource described by this PlatformConfig.
// When multiple Sources are specified, a FallbackSource wraps them so that
// failures cascade to the next source in the list.
func (pc PlatformConfig) NewSource() (ChatSource, error) {
	switch pc.Type {
	case "youtube":
		return &YouTubeSource{
			ProxyAddr:    pc.ProxyAddr,
			Video:        pc.Video,
			Channel:      pc.Channel,
			DetectMethod: pc.DetectMethod,
			Hl:           pc.Hl,
			RawMessage:   pc.RawMessage,
		}, nil
	case "twitch":
		sources := pc.Sources
		if len(sources) == 0 {
			sources = []string{"irc"}
		}
		if len(sources) == 1 {
			return pc.newTwitchSourceByName(sources[0])
		}
		var chatSources []ChatSource
		for _, name := range sources {
			src, err := pc.newTwitchSourceByName(name)
			if err != nil {
				return nil, err
			}
			chatSources = append(chatSources, src)
		}
		return &FallbackSource{Sources: chatSources}, nil
	case "kick":
		return &KickSource{
			ChatWebhookAddr: pc.ChatWebhookAddr,
		}, nil
	default:
		return nil, fmt.Errorf("unknown platform type %q", pc.Type)
	}
}

// streamdConfigForCredentials is a minimal struct for extracting platform
// credentials from the streamd config YAML without importing the full
// config types (avoiding circular dependencies).
type streamdConfigForCredentials struct {
	Backends map[string]struct {
		Config map[string]interface{} `yaml:"config"`
	} `yaml:"backends"`
}

// streamdConfigString extracts a string value from the parsed backend config map.
func streamdConfigString(m map[string]interface{}, key string) string {
	v, ok := m[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

// resolveCredentials fetches platform credentials from streamd when
// Credentials is set to "streamd". Updates the PlatformConfig fields
// (ClientID, AccessToken, Channel, etc.) in place, without overriding
// fields that are already set.
func (pc *PlatformConfig) resolveCredentials(
	ctx context.Context,
	streamdClient streamd_grpc.StreamDClient,
) (_err error) {
	logger.Tracef(ctx, "resolveCredentials[%s]", pc.Type)
	defer func() { logger.Tracef(ctx, "/resolveCredentials[%s]: %v", pc.Type, _err) }()

	switch pc.Credentials {
	case "":
		return nil
	case "streamd":
	default:
		return fmt.Errorf("unknown credentials source %q (supported: \"streamd\")", pc.Credentials)
	}

	reply, err := streamdClient.GetConfig(ctx, &streamd_grpc.GetConfigRequest{})
	if err != nil {
		return fmt.Errorf("GetConfig from streamd: %w", err)
	}

	var sdCfg streamdConfigForCredentials
	if err := yaml.Unmarshal([]byte(reply.GetConfig()), &sdCfg); err != nil {
		return fmt.Errorf("parse streamd config YAML: %w", err)
	}

	backendName := pc.Type
	backend, ok := sdCfg.Backends[backendName]
	if !ok {
		return fmt.Errorf("backend %q not found in streamd config", backendName)
	}

	m := backend.Config
	if m == nil {
		return fmt.Errorf("backend %q has no config in streamd", backendName)
	}

	switch pc.Type {
	case "twitch":
		// goccy/go-yaml lowercases field names without yaml tags:
		// ClientID -> "clientid", UserAccessToken -> "useraccesstoken", Channel -> "channel".
		if pc.Channel == "" {
			pc.Channel = streamdConfigString(m, "channel")
		}
		if pc.ClientID == "" {
			pc.ClientID = streamdConfigString(m, "clientid")
		}
		if pc.AccessToken == "" {
			pc.AccessToken = streamdConfigString(m, "useraccesstoken")
		}
	case "kick":
		// Kick types have explicit yaml tags: "channel", "client_id", "user_access_token".
		if pc.Channel == "" {
			pc.Channel = streamdConfigString(m, "channel")
		}
	case "youtube":
		// YouTube PlatformSpecificConfig has no yaml tags:
		// ChannelID -> "channelid", ClientID -> "clientid".
		if pc.Channel == "" {
			pc.Channel = streamdConfigString(m, "channelid")
		}
	default:
		return fmt.Errorf("credentials resolution not supported for platform %q", pc.Type)
	}

	logger.Debugf(ctx, "resolved credentials for %s from streamd (channel=%q)", pc.Type, pc.Channel)
	return nil
}

type TranslationConfig struct {
	TargetLanguage  string           `yaml:"target_language"`
	ChatHistorySize int              `yaml:"chat_history_size"`
	Providers       []ProviderConfig `yaml:"providers"`
}

type ProviderConfig struct {
	Type         string        `yaml:"type"` // ollama, openai, anthropic, claude-code, streamdcfg, streampanelcfg
	APIURL       string        `yaml:"api_url"`
	APIKey       string        `yaml:"api_key"`
	Model        string        `yaml:"model"`
	Parallelism  int           `yaml:"parallelism"`
	MaxQueueSize int           `yaml:"max_queue_size"` // max pending translations waiting for this provider; 0 = no queueing
	Timeout      time.Duration `yaml:"timeout"`        // per-request timeout; 0 means no timeout
	ConfigPath   string        `yaml:"config_path"`    // for streamdcfg/streampanelcfg: path to YAML config
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

	logger.Debugf(ctx, "loaded config: streamd=%s platforms=%d providers=%d",
		cfg.StreamdAddr, len(cfg.Platforms), len(cfg.Translation.Providers))

	return cfg, nil
}

const sampleConfig = `# chatinjector configuration
# See: https://github.com/xaionaro-go/streamctl/tree/main/cmd/chatinjector

# streamd gRPC address
streamd_addr: "localhost:3594"

# Chat source platforms (at least one required).
platforms:
  # YouTube — requires youtubeapiproxy running at proxy_addr.
  - type: youtube
    proxy_addr: "localhost:9090"
    # Monitor a channel for live streams (auto-detect):
    # channel: "https://www.youtube.com/@YourChannel"
    # Or connect to a specific video/liveChatId:
    # video: "https://www.youtube.com/watch?v=VIDEO_ID"
    detect_method: "search"
    # hl: "en"
    # raw_message: false

  # Twitch — anonymous IRC, read-only (default).
  # - type: twitch
  #   channel: "xqc"

  # Twitch — with EventSub fallback to IRC, credentials from streamd.
  # - type: twitch
  #   channel: "xqc"
  #   sources: ["eventsub", "irc"]  # try eventsub first, fall back to IRC
  #   credentials: streamd            # fetch tokens from streamd (auto-refreshed)

  # Twitch — with EventSub fallback to IRC, static credentials.
  # - type: twitch
  #   channel: "xqc"
  #   sources: ["eventsub", "irc"]  # try eventsub first, fall back to IRC
  #   client_id: "your_client_id"       # required for eventsub
  #   access_token: "your_access_token" # required for eventsub

  # Kick — requires chatwebhook gRPC service.
  # - type: kick
  #   chat_webhook_addr: "localhost:9091"

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
