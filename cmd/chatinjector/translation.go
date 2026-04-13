package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/goccy/go-yaml"
	"github.com/xaionaro-go/streamctl/pkg/llm"
	llmcfg "github.com/xaionaro-go/streamctl/pkg/streamd/config/llm"
	"github.com/xaionaro-go/xpath"
)

func buildTranslatorChain(
	ctx context.Context,
	tc TranslationConfig,
) (_ret *llm.TranslatorChain, _err error) {
	logger.Tracef(ctx, "buildTranslatorChain")
	defer func() { logger.Tracef(ctx, "/buildTranslatorChain: %v", _err) }()

	var entries []llm.ProviderEntry

	for _, pc := range tc.Providers {
		expanded, err := expandProvider(ctx, pc)
		if err != nil {
			return nil, fmt.Errorf("create provider %q: %w", pc.Type, err)
		}
		entries = append(entries, expanded...)
	}

	if len(entries) == 0 {
		return nil, fmt.Errorf("translation target_language is set but no providers configured")
	}

	logger.Debugf(ctx, "built translator chain with %d providers for language %q",
		len(entries), tc.TargetLanguage)

	return llm.NewTranslatorChain(tc.TargetLanguage, tc.ChatHistorySize, entries), nil
}

func expandProvider(
	ctx context.Context,
	pc ProviderConfig,
) (_ret []llm.ProviderEntry, _err error) {
	logger.Tracef(ctx, "expandProvider")
	defer func() { logger.Tracef(ctx, "/expandProvider: %v", _err) }()

	entry := func(p llm.Provider) []llm.ProviderEntry {
		return []llm.ProviderEntry{{
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
		return entry(&llm.OllamaProvider{APIURL: pc.APIURL, Model: pc.Model}), nil
	case "openai":
		return entry(&llm.OpenAIProvider{APIURL: pc.APIURL, APIKey: pc.APIKey, Model: pc.Model}), nil
	case "openrouter":
		apiURL := pc.APIURL
		if apiURL == "" {
			apiURL = defaultOpenRouterURL
		}
		return entry(&llm.OpenAIProvider{APIURL: apiURL, APIKey: pc.APIKey, Model: pc.Model}), nil
	case "zen":
		apiURL := pc.APIURL
		if apiURL == "" {
			apiURL = defaultZenURL
		}
		return entry(&llm.OpenAIProvider{APIURL: apiURL, APIKey: pc.APIKey, Model: pc.Model}), nil
	case "anthropic":
		return entry(&llm.AnthropicProvider{APIURL: pc.APIURL, APIKey: pc.APIKey, Model: pc.Model}), nil
	case "claude-code":
		return entry(&llm.ClaudeCodeProvider{Model: pc.Model, Effort: pc.Effort}), nil
	case "streamdcfg", "streampanelcfg":
		return importLLMProviders(ctx, pc.ConfigPath, pc.Parallelism, pc.Timeout)
	default:
		return nil, fmt.Errorf("unknown provider type %q", pc.Type)
	}
}

// importLLMProviders reads LLM endpoints from a streamd or streampanel
// config file and converts them into provider instances.
func importLLMProviders(
	ctx context.Context,
	cfgPath string,
	defaultParallelism int,
	defaultTimeout time.Duration,
) (_ret []llm.ProviderEntry, _err error) {
	logger.Tracef(ctx, "importLLMProviders")
	defer func() { logger.Tracef(ctx, "/importLLMProviders: %v", _err) }()

	expandedPath, err := xpath.Expand(cfgPath)
	if err != nil {
		return nil, fmt.Errorf("expand path %q: %w", cfgPath, err)
	}

	data, err := os.ReadFile(expandedPath)
	if err != nil {
		return nil, fmt.Errorf("read %q: %w", expandedPath, err)
	}

	// Minimal struct to extract LLM config from either streamd or streampanel format.
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

	// Merge endpoints from both top-level (streamd) and nested (streampanel) locations.
	allEndpoints := llmcfg.Endpoints{}
	for k, v := range cfg.LLM.Endpoints {
		allEndpoints[k] = v
	}
	for k, v := range cfg.BuiltinStreamD.LLM.Endpoints {
		allEndpoints[k] = v
	}

	logger.Debugf(ctx, "imported %d LLM endpoints from %q", len(allEndpoints), expandedPath)

	par := defaultParallelism
	if par <= 0 {
		par = 1
	}

	var result []llm.ProviderEntry
	for name, endpoint := range allEndpoints {
		if endpoint == nil {
			continue
		}

		logger.Debugf(ctx, "  endpoint %q: provider=%q api_url=%q model=%q",
			name, endpoint.Provider, endpoint.APIURL, endpoint.ModelName)

		p, err := endpointToProvider(endpoint)
		if err != nil {
			logger.Warnf(ctx, "skipping endpoint %q: %v", name, err)
			continue
		}

		result = append(result, llm.ProviderEntry{Provider: p, Parallelism: par, Timeout: defaultTimeout})
	}

	return result, nil
}

func endpointToProvider(endpoint *llmcfg.Endpoint) (llm.Provider, error) {
	switch endpoint.Provider {
	case llmcfg.ProviderChatGPT:
		apiURL := endpoint.APIURL
		if apiURL == "" {
			apiURL = defaultOpenAIURL
		}
		return &llm.OpenAIProvider{
			APIURL: apiURL,
			APIKey: endpoint.APIKey,
			Model:  endpoint.ModelName,
		}, nil
	default:
		if endpoint.APIURL == "" {
			return nil, fmt.Errorf("unsupported provider %q with no api_url", endpoint.Provider)
		}
		// Treat unknown providers with a URL as Ollama-compatible.
		return &llm.OllamaProvider{
			APIURL: endpoint.APIURL,
			Model:  endpoint.ModelName,
		}, nil
	}
}
