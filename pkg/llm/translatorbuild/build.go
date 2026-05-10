package translatorbuild

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/goccy/go-yaml"
	llms "github.com/xaionaro-go/streamctl/pkg/llm"
	llmcfg "github.com/xaionaro-go/streamctl/pkg/streamd/config/llm"
	"github.com/xaionaro-go/xpath"
)

// Default API URLs for provider types that allow omitting api_url.
const (
	defaultOpenAIURL     = "https://api.openai.com"
	defaultOpenRouterURL = "https://openrouter.ai/api"
	defaultZenURL        = "https://api.zen.ai"
)

// DefaultChatHistorySize is the chain.historyMax value applied when
// Config.ChatHistorySize is zero or negative. Surfaced as a const so tests
// and callers can assert the value without copying it.
const DefaultChatHistorySize = 20

// ParseConfigYAML decodes the wire YAML into a Config. Returns an error on
// malformed input; the caller (e.g. the subprocess Reconfigure handler)
// translates that into a retain-old-chain reply.
func ParseConfigYAML(b []byte) (Config, error) {
	var cfg Config
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return Config{}, fmt.Errorf("unmarshal: %w", err)
	}
	return cfg, nil
}

// BuildChain builds a TranslatorChain from a parsed Config. The single
// authoritative implementation called by the translator subprocess
// Reconfigure handler; streamd never builds chains itself (the subprocess
// owns the chain).
func BuildChain(
	ctx context.Context,
	cfg Config,
) (_ret *llms.TranslatorChain, _err error) {
	logger.Tracef(ctx, "translatorbuild.BuildChain")
	defer func() { logger.Tracef(ctx, "/translatorbuild.BuildChain: %v", _err) }()

	if cfg.TargetLanguage == "" {
		return nil, fmt.Errorf("target_language is empty")
	}

	// Disambiguate Names before expansion — once a streamdcfg/streampanelcfg
	// entry fans out, "Name" no longer uniquely identifies an entry, so the
	// de-dup must happen at the pre-expand layer.
	//
	// Rule:
	//   - Name == "" → auto-assigned to "<type>#<index>" so two unnamed
	//     entries of the same Type (e.g. two `ollama` blocks) keep working.
	//     Pre-existing configs that relied on the old "default Name=Type"
	//     behaviour are unaffected for the single-entry case (the resulting
	//     Name still uniquely identifies the entry); the only behaviour
	//     change is that what used to be a hard error (two unnamed ollamas)
	//     now succeeds.
	//   - Name != "" → kept as-is. Two entries with the same explicit Name
	//     are a configuration error and rejected so an operator running
	//     `providers remove <name>` cannot silently hit the wrong entry.
	seenNames := make(map[string]struct{}, len(cfg.Providers))
	for i := range cfg.Providers {
		pc := &cfg.Providers[i]
		if pc.Name == "" {
			pc.Name = fmt.Sprintf("%s#%d", pc.Type, i)
		}
		if _, dup := seenNames[pc.Name]; dup {
			return nil, fmt.Errorf("duplicate provider name %q (set distinct .name fields)", pc.Name)
		}
		seenNames[pc.Name] = struct{}{}
	}

	var entries []llms.ProviderEntry
	for _, pc := range cfg.Providers {
		expanded, err := expandProvider(ctx, pc)
		if err != nil {
			return nil, fmt.Errorf("provider %q: %w", pc.Type, err)
		}
		entries = append(entries, expanded...)
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("no providers resolved")
	}

	historySize := cfg.ChatHistorySize
	if historySize <= 0 {
		historySize = DefaultChatHistorySize
	}

	return llms.NewTranslatorChain(cfg.TargetLanguage, historySize, entries), nil
}

// expandProvider converts one ProviderConfig into one-or-more ProviderEntry
// values. The streamdcfg/streampanelcfg cases fan out: a single config entry
// imports every endpoint in the referenced YAML file.
func expandProvider(
	ctx context.Context,
	pc ProviderConfig,
) ([]llms.ProviderEntry, error) {
	entry := func(p llms.Provider) []llms.ProviderEntry {
		return []llms.ProviderEntry{{
			Provider:                p,
			Parallelism:             pc.Parallelism,
			MaxQueueSize:            pc.MaxQueueSize,
			Timeout:                 pc.Timeout,
			CircuitBreakerThreshold: pc.CircuitBreakerThreshold,
			CircuitBreakerCooldown:  pc.CircuitBreakerCooldown,
		}}
	}

	switch pc.Type {
	case "ollama":
		return entry(&llms.OllamaProvider{APIURL: pc.APIURL, Model: pc.Model}), nil
	case "openai":
		return entry(&llms.OpenAIProvider{APIURL: pc.APIURL, APIKey: pc.APIKey, Model: pc.Model}), nil
	case "openrouter":
		apiURL := pc.APIURL
		if apiURL == "" {
			apiURL = defaultOpenRouterURL
		}
		return entry(&llms.OpenAIProvider{APIURL: apiURL, APIKey: pc.APIKey, Model: pc.Model}), nil
	case "zen":
		apiURL := pc.APIURL
		if apiURL == "" {
			apiURL = defaultZenURL
		}
		return entry(&llms.OpenAIProvider{APIURL: apiURL, APIKey: pc.APIKey, Model: pc.Model}), nil
	case "anthropic":
		return entry(&llms.AnthropicProvider{APIURL: pc.APIURL, APIKey: pc.APIKey, Model: pc.Model}), nil
	case "claude-code":
		return entry(&llms.ClaudeCodeProvider{Model: pc.Model, Effort: pc.Effort}), nil
	case "streamdcfg", "streampanelcfg":
		return importLLMProviders(ctx, pc.ConfigPath, pc.Parallelism, pc.Timeout)
	default:
		return nil, fmt.Errorf("unknown provider type %q", pc.Type)
	}
}

// importLLMProviders reads LLM endpoints from a streamd or streampanel config
// file and converts them into provider instances. Mirrors the streamd-side
// init path so the subprocess can build chains without importing streamd
// itself.
func importLLMProviders(
	ctx context.Context,
	cfgPath string,
	parallelism int,
	timeout time.Duration,
) ([]llms.ProviderEntry, error) {
	expandedPath, err := xpath.Expand(cfgPath)
	if err != nil {
		return nil, fmt.Errorf("expand path %q: %w", cfgPath, err)
	}

	data, err := os.ReadFile(expandedPath)
	if err != nil {
		return nil, fmt.Errorf("read %q: %w", expandedPath, err)
	}

	type configLLMNested struct {
		LLM llmcfg.Config `yaml:"llm"`
	}
	type configLLMOnly struct {
		LLM            llmcfg.Config   `yaml:"llm"`
		BuiltinStreamD configLLMNested `yaml:"streamd_builtin"`
	}

	var cfg configLLMOnly
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse %q: %w", expandedPath, err)
	}

	endpoints := llmcfg.Endpoints{}
	for k, v := range cfg.LLM.Endpoints {
		endpoints[k] = v
	}
	for k, v := range cfg.BuiltinStreamD.LLM.Endpoints {
		endpoints[k] = v
	}

	if parallelism <= 0 {
		parallelism = 1
	}

	var result []llms.ProviderEntry
	for name, endpoint := range endpoints {
		if endpoint == nil {
			continue
		}
		p, err := endpointToProvider(endpoint)
		if err != nil {
			logger.Warnf(ctx, "skipping endpoint %q: %v", name, err)
			continue
		}
		result = append(result, llms.ProviderEntry{Provider: p, Parallelism: parallelism, Timeout: timeout})
	}
	return result, nil
}

// endpointToProvider turns one llm.Config endpoint into a Provider instance.
// Recognises ChatGPT-shaped endpoints (OpenAI-compatible) and falls back to
// Ollama for anything that supplies an api_url.
func endpointToProvider(endpoint *llmcfg.Endpoint) (llms.Provider, error) {
	switch endpoint.Provider {
	case llmcfg.ProviderChatGPT:
		apiURL := endpoint.APIURL
		if apiURL == "" {
			apiURL = defaultOpenAIURL
		}
		return &llms.OpenAIProvider{
			APIURL: apiURL,
			APIKey: endpoint.APIKey,
			Model:  endpoint.ModelName,
		}, nil
	default:
		if endpoint.APIURL == "" {
			return nil, fmt.Errorf("unsupported provider %q with no api_url", endpoint.Provider)
		}
		return &llms.OllamaProvider{
			APIURL: endpoint.APIURL,
			Model:  endpoint.ModelName,
		}, nil
	}
}
