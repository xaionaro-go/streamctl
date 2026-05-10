// Package translatorbuild owns the canonical TranslatorChain build logic and
// the YAML-shaped config types both streamd (the supervisor) and the
// translator subprocess consume. Centralizing here breaks the cycle that
// would otherwise form (streamd → translator → streamd) and removes the
// duplicate expand/import logic that previously lived in pkg/streamd
// /translator.go and pkg/translator/build.go.
package translatorbuild

import "time"

// Defaults for the streamd-side translation worker. Override via
// Config.QueueSize.
//
//   - DefaultQueueSize bounds the in-memory channel feeding the worker.
//     When full, new messages SKIP translation entirely (the raw publish
//     path is unaffected) — back-pressure must never block ingestion.
const (
	DefaultQueueSize = 64
)

// Config is the YAML-shaped configuration for the TranslatorChain. Both the
// streamd-side init path and the translator subprocess decode YAML into this
// type so the supervisor → subprocess wire format is just yaml.Marshal of the
// same struct.
type Config struct {
	// TargetLanguage is the human-readable target language (e.g. "English").
	// An empty string disables translation in the supervisor and is rejected
	// by BuildChain in the subprocess.
	TargetLanguage string `yaml:"target_language"`

	// ChatHistorySize bounds the rolling message buffer fed to the LLM as
	// context. Zero or negative values mean "use the package default"
	// (DefaultChatHistorySize).
	ChatHistorySize int `yaml:"chat_history_size"`

	// Providers is the ordered fallback chain. BuildChain tries each provider
	// in turn; the first to succeed wins.
	Providers []ProviderConfig `yaml:"providers"`

	// QueueSize is the depth of the streamd-side translation channel feeding
	// the translation worker. When the channel is full, new messages skip
	// translation entirely (the raw publish path is unaffected). Zero or
	// negative means "use the package default" (DefaultQueueSize); read
	// the resolved value via EffectiveQueueSize.
	QueueSize int `yaml:"queue_size"`
}

// EffectiveQueueSize returns the configured QueueSize when positive, else
// DefaultQueueSize. Single source of truth for the resolved value so callers
// (streamd, tests) cannot drift.
func (c Config) EffectiveQueueSize() int {
	if c.QueueSize > 0 {
		return c.QueueSize
	}
	return DefaultQueueSize
}

// ProviderConfig is one entry in Config.Providers. Field names match
// cmd/chatinjector's ProviderConfig (the original source of the schema) so
// existing YAML round-trips lossless across both binaries.
type ProviderConfig struct {
	// Name is the stable handle for streamcli `providers remove <name>` /
	// `providers set <name> ...`. When empty, BuildChain auto-assigns
	// Name="<type>#<index>" so two unnamed entries of the same Type (e.g.
	// two `ollama` blocks) coexist without colliding. Two entries with the
	// same explicit non-empty Name are rejected by BuildChain — operators
	// must pick distinct names so `providers remove <name>` is unambiguous.
	Name string `yaml:"name"`

	// Type selects the provider implementation: ollama, openai, openrouter,
	// zen, anthropic, claude-code, streamdcfg, streampanelcfg.
	Type string `yaml:"type"`

	// APIURL is the provider HTTP base URL. Several types fall back to a
	// well-known default when empty (see defaultOpenRouterURL etc.).
	APIURL string `yaml:"api_url"`

	// APIKey is the provider authentication token. Required for cloud
	// providers; ignored for local providers like ollama.
	APIKey string `yaml:"api_key"`

	// Model is the model identifier the provider will load. Provider-specific.
	Model string `yaml:"model"`

	// Parallelism caps simultaneous in-flight Translate calls per provider.
	// Zero or negative means 1 (sequential).
	Parallelism int `yaml:"parallelism"`

	// MaxQueueSize is how many extra Translate calls may wait for a free
	// parallelism slot before the chain falls through to the next provider.
	MaxQueueSize int `yaml:"max_queue_size"`

	// Timeout caps a single Translate invocation. Zero means
	// pkg/llm.defaultProviderTimeout (30s) is used.
	Timeout time.Duration `yaml:"timeout"`

	// ConfigPath is consumed by the streamdcfg/streampanelcfg provider types
	// to point at a peer streamd/streampanel config file whose llm.endpoints
	// block should be imported.
	ConfigPath string `yaml:"config_path"`

	// Effort controls the claude-code provider's reasoning effort level.
	Effort string `yaml:"effort"`

	// CircuitBreakerThreshold is the consecutive-failure count after which the
	// provider is skipped. Zero means pkg/llm's package default.
	CircuitBreakerThreshold int64 `yaml:"circuit_breaker_threshold"`

	// CircuitBreakerCooldown is how long a tripped circuit must wait before
	// re-probing the provider. Zero means pkg/llm's package default.
	CircuitBreakerCooldown time.Duration `yaml:"circuit_breaker_cooldown"`
}
