package config

import "time"

// TranslationConfig configures the chat-message translator. When
// TargetLanguage is empty translation is disabled.
type TranslationConfig struct {
	TargetLanguage  string                      `yaml:"target_language"`
	ChatHistorySize int                         `yaml:"chat_history_size"`
	Providers       []TranslationProviderConfig `yaml:"providers"`
}

// TranslationProviderConfig matches cmd/chatinjector's ProviderConfig so the
// same chain of fallback providers can be configured directly inside streamd.
type TranslationProviderConfig struct {
	Type                    string        `yaml:"type"` // ollama, openai, openrouter, zen, anthropic, claude-code, streamdcfg, streampanelcfg
	APIURL                  string        `yaml:"api_url"`
	APIKey                  string        `yaml:"api_key"`
	Model                   string        `yaml:"model"`
	Parallelism             int           `yaml:"parallelism"`
	MaxQueueSize            int           `yaml:"max_queue_size"`
	Timeout                 time.Duration `yaml:"timeout"`
	ConfigPath              string        `yaml:"config_path"` // for streamdcfg/streampanelcfg
	Effort                  string        `yaml:"effort"`      // for claude-code
	CircuitBreakerThreshold int64         `yaml:"circuit_breaker_threshold"`
	CircuitBreakerCooldown  time.Duration `yaml:"circuit_breaker_cooldown"`
}
