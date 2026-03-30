package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/goccy/go-yaml"
	"github.com/xaionaro-go/streamctl/pkg/streamd/config"
	"github.com/xaionaro-go/xpath"
)

const (
	streamdConfigPrefix     = "streamdcfg:"
	streampanelConfigPrefix = "streampanelcfg:"
)

// ResolvedLLMConfig holds the resolved LLM API URL, key, and model.
type ResolvedLLMConfig struct {
	APIURL string
	APIKey string
	Model  string
}

// resolveLLMConfig resolves the --llm-provider value.
// If it starts with "streamdcfg:" or "streampanelcfg:", it reads the
// LLM config from the specified YAML config file. Otherwise it's
// used as a direct LLM API URL (Ollama or OpenAI-compatible).
func resolveLLMConfig(
	ctx context.Context,
	llmProvider string,
	llmModel string,
) (_ret ResolvedLLMConfig, _err error) {
	logger.Tracef(ctx, "resolveLLMConfig")
	defer func() { logger.Tracef(ctx, "/resolveLLMConfig: %v", _err) }()

	var cfgPath string
	switch {
	case strings.HasPrefix(llmProvider, streamdConfigPrefix):
		cfgPath = strings.TrimPrefix(llmProvider, streamdConfigPrefix)
	case strings.HasPrefix(llmProvider, streampanelConfigPrefix):
		cfgPath = strings.TrimPrefix(llmProvider, streampanelConfigPrefix)
	default:
		logger.Debugf(ctx, "using direct LLM URL: %s, model: %s", llmProvider, llmModel)
		return ResolvedLLMConfig{
			APIURL: llmProvider,
			Model:     llmModel,
		}, nil
	}

	logger.Debugf(ctx, "reading LLM config from %q", cfgPath)

	endpoint, err := readLLMEndpointFromConfig(ctx, cfgPath)
	if err != nil {
		return ResolvedLLMConfig{}, fmt.Errorf("read LLM config from %q: %w", cfgPath, err)
	}

	resolved := ResolvedLLMConfig{
		APIURL: endpoint.APIURL,
		APIKey: endpoint.APIKey,
		Model:  endpoint.ModelName,
	}

	// CLI flag overrides config value.
	if llmModel != llmDefaultModel && llmModel != "" {
		resolved.Model = llmModel
	}

	logger.Debugf(ctx, "resolved LLM config: url=%s, model=%s", resolved.APIURL, resolved.Model)
	return resolved, nil
}

func readLLMEndpointFromConfig(
	ctx context.Context,
	cfgPath string,
) (_ *config.LLMEndpoint, _err error) {
	logger.Tracef(ctx, "readLLMEndpointFromConfig")
	defer func() { logger.Tracef(ctx, "/readLLMEndpointFromConfig: %v", _err) }()

	expandedPath, err := xpath.Expand(cfgPath)
	if err != nil {
		return nil, fmt.Errorf("expand path %q: %w", cfgPath, err)
	}

	data, err := os.ReadFile(expandedPath)
	if err != nil {
		return nil, fmt.Errorf("read %q: %w", expandedPath, err)
	}

	var cfg config.Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse %q: %w", expandedPath, err)
	}

	for name, endpoint := range cfg.LLM.Endpoints {
		if endpoint == nil {
			continue
		}

		// Default API URL for known providers.
		if endpoint.APIURL == "" && endpoint.Provider == config.LLMProviderChatGPT {
			endpoint.APIURL = "https://api.openai.com"
		}

		if endpoint.APIURL == "" {
			continue
		}

		logger.Debugf(ctx, "using LLM endpoint %q: provider=%s, url=%s, model=%s",
			name, endpoint.Provider, endpoint.APIURL, endpoint.ModelName)
		return endpoint, nil
	}

	return nil, fmt.Errorf("no usable LLM endpoint found in %q", expandedPath)
}
