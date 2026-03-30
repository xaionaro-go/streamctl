package config

import "github.com/xaionaro-go/streamctl/pkg/streamd/config/llm"

type LLM = llm.Config
type LLMEndpoint = llm.Endpoint
type LLMProvider = llm.Provider
type LLMEndpoints = llm.Endpoints

const (
	UndefinedLLMProvider = llm.UndefinedProvider
	LLMProviderChatGPT   = llm.ProviderChatGPT
)
