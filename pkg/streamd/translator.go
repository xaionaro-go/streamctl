package streamd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/goccy/go-yaml"
	llms "github.com/xaionaro-go/streamctl/pkg/llm"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamd/config"
	llmcfg "github.com/xaionaro-go/streamctl/pkg/streamd/config/llm"
	"github.com/xaionaro-go/xpath"
)

// translationSeparator marks the boundary between original message text and
// the translated text appended to it. Mirrors cmd/chatinjector/engine.go so
// downstream consumers see the same wire format whichever binary translates.
const translationSeparator = " -文A-> "

const (
	defaultOpenAIURL     = "https://api.openai.com"
	defaultOpenRouterURL = "https://openrouter.ai/api"
	defaultZenURL        = "https://api.zen.ai"
)

func (d *StreamD) initTranslator(
	ctx context.Context,
) (_err error) {
	logger.Tracef(ctx, "initTranslator")
	defer func() { logger.Tracef(ctx, "/initTranslator: %v", _err) }()

	tc := d.Config.Translation
	if tc.TargetLanguage == "" {
		logger.Debugf(ctx, "translation disabled: target_language is empty")
		return nil
	}

	entries, err := buildTranslationProviders(ctx, tc)
	if err != nil {
		return fmt.Errorf("build translation providers: %w", err)
	}
	if len(entries) == 0 {
		return fmt.Errorf("translation target_language=%q but no providers resolved", tc.TargetLanguage)
	}

	historySize := tc.ChatHistorySize
	if historySize <= 0 {
		historySize = 20
	}

	d.translator = llms.NewTranslatorChain(tc.TargetLanguage, historySize, entries)
	logger.Debugf(ctx, "translator initialized: target=%q providers=%d history=%d",
		tc.TargetLanguage, len(entries), historySize)
	return nil
}

// translateEventInPlace translates ev.Message.Content (when present) and, when
// the result differs from the original, appends it after translationSeparator.
// Errors and empty results are swallowed: chat must keep flowing even when the
// LLM is unreachable. The TranslatorChain itself returns the original message
// on full provider failure, so this function rarely sees a non-nil error.
func (d *StreamD) translateEventInPlace(
	ctx context.Context,
	ev *streamcontrol.Event,
) {
	if d.translator == nil {
		return
	}
	if ev == nil || ev.Message == nil || ev.Message.Content == "" {
		return
	}

	// Skip if upstream already appended a translation (e.g. when chatinjector
	// is also running). Translating "original -文A-> translated" again would
	// feed the LLM a garbled mixed string.
	if strings.Contains(ev.Message.Content, translationSeparator) {
		return
	}

	original := ev.Message.Content
	user := ev.User.Name
	if user == "" {
		user = string(ev.User.ID)
	}

	translated, err := d.translator.Translate(ctx, user, original)
	switch {
	case err != nil:
		logger.Warnf(ctx, "translation failed for %s: %v", ev.ID, err)
	case translated == "" || translated == original:
		// Already in target language or unchanged — leave the message alone.
	default:
		ev.Message.Content = original + translationSeparator + translated
	}
}

func buildTranslationProviders(
	ctx context.Context,
	tc config.TranslationConfig,
) (_ret []llms.ProviderEntry, _err error) {
	logger.Tracef(ctx, "buildTranslationProviders")
	defer func() { logger.Tracef(ctx, "/buildTranslationProviders: %v", _err) }()

	var entries []llms.ProviderEntry
	for _, pc := range tc.Providers {
		expanded, err := expandTranslationProvider(ctx, pc)
		if err != nil {
			return nil, fmt.Errorf("provider %q: %w", pc.Type, err)
		}
		entries = append(entries, expanded...)
	}
	return entries, nil
}

func expandTranslationProvider(
	ctx context.Context,
	pc config.TranslationProviderConfig,
) (_ret []llms.ProviderEntry, _err error) {
	logger.Tracef(ctx, "expandTranslationProvider type=%q", pc.Type)
	defer func() { logger.Tracef(ctx, "/expandTranslationProvider: %v", _err) }()

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

// importLLMProviders reads LLM endpoints from a streamd or streampanel
// config file and converts them into provider instances.
func importLLMProviders(
	ctx context.Context,
	cfgPath string,
	parallelism int,
	timeout time.Duration,
) (_ret []llms.ProviderEntry, _err error) {
	logger.Tracef(ctx, "importLLMProviders %q", cfgPath)
	defer func() { logger.Tracef(ctx, "/importLLMProviders: %v", _err) }()

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
