package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/goccy/go-yaml"
	"github.com/xaionaro-go/xpath"
)

const (
	defaultConfigPath      = "~/.config/chatinjector/chatinjector.yaml"
	defaultChatHistorySize = 20
)

type AppConfig struct {
	YTProxyAddr  string            `yaml:"yt_proxy_addr"`
	StreamdAddr  string            `yaml:"streamd_addr"`
	Video        string            `yaml:"video"`
	Channel      string            `yaml:"channel"`
	DetectMethod string            `yaml:"detect_method"`
	Hl           string            `yaml:"hl"`
	RawMessage   bool              `yaml:"raw_message"`
	Translation  TranslationConfig `yaml:"translation"`
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

	logger.Debugf(ctx, "loaded config: yt_proxy=%s streamd=%s channel=%s video=%s providers=%d",
		cfg.YTProxyAddr, cfg.StreamdAddr, cfg.Channel, cfg.Video, len(cfg.Translation.Providers))

	return cfg, nil
}

const sampleConfig = `# chatinjector configuration
# See: https://github.com/xaionaro-go/streamctl/tree/main/cmd/chatinjector

# youtubeapiproxy gRPC address
yt_proxy_addr: "localhost:9090"

# streamd gRPC address
streamd_addr: "localhost:3594"

# Monitor a channel for live streams (auto-detect)
# channel: "https://www.youtube.com/@YourChannel"

# Or connect to a specific video/liveChatId
# video: "https://www.youtube.com/watch?v=VIDEO_ID"

# Detection method for channel monitoring: broadcasts, search, or html
detect_method: "search"

# YouTube display language for system messages
# hl: "en"

# Use raw TextMessageDetails instead of DisplayMessage
# raw_message: false

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
