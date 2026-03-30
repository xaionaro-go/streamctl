package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/xaionaro-go/streamctl/pkg/streamd/config"
	"github.com/xaionaro-go/xpath"
)

const (
	streamdConfigPrefix      = "streamdcfg:"
	streampanelConfigPrefix  = "streampanelcfg:"
)

// ResolvedLLMConfig holds the resolved Ollama URL and model name.
type ResolvedLLMConfig struct {
	OllamaURL string
	Model     string
}

// resolveLLMConfig resolves the --ollama-url value.
// If it starts with "streamdcfg:", it reads the LLM config from the
// specified streampanel YAML config file. Otherwise it's used as a
// direct Ollama URL.
func resolveLLMConfig(
	ollamaURL string,
	ollamaModel string,
) (ResolvedLLMConfig, error) {
	var cfgPath string
	switch {
	case strings.HasPrefix(ollamaURL, streamdConfigPrefix):
		cfgPath = strings.TrimPrefix(ollamaURL, streamdConfigPrefix)
	case strings.HasPrefix(ollamaURL, streampanelConfigPrefix):
		cfgPath = strings.TrimPrefix(ollamaURL, streampanelConfigPrefix)
	default:
		return ResolvedLLMConfig{
			OllamaURL: ollamaURL,
			Model:     ollamaModel,
		}, nil
	}
	endpoint, err := readLLMEndpointFromConfig(cfgPath)
	if err != nil {
		return ResolvedLLMConfig{}, fmt.Errorf("read LLM config from %q: %w", cfgPath, err)
	}

	resolved := ResolvedLLMConfig{
		OllamaURL: endpoint.APIURL,
		Model:     endpoint.ModelName,
	}

	// CLI flag overrides config value.
	if ollamaModel != ollamaDefaultModel && ollamaModel != "" {
		resolved.Model = ollamaModel
	}

	return resolved, nil
}

func readLLMEndpointFromConfig(
	cfgPath string,
) (*config.LLMEndpoint, error) {
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

	for _, endpoint := range cfg.LLM.Endpoints {
		if endpoint != nil && endpoint.APIURL != "" {
			return endpoint, nil
		}
	}

	return nil, fmt.Errorf("no LLM endpoint with api_url found in %q", expandedPath)
}
