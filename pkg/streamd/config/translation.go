package config

import (
	"github.com/xaionaro-go/streamctl/pkg/llm/translatorbuild"
)

// TranslationConfig is the YAML schema for the chat-message translator.
// It is the canonical translatorbuild.Config type re-exported here so existing
// streamd YAML config files (which embed translation: {...}) keep working
// unchanged. Single source of truth — to add a field, edit
// pkg/llm/translatorbuild/config.go.
type TranslationConfig = translatorbuild.Config

// TranslationProviderConfig matches cmd/chatinjector's ProviderConfig so the
// same chain of fallback providers can be configured directly inside streamd.
// Re-exported alias for the canonical translatorbuild.ProviderConfig.
type TranslationProviderConfig = translatorbuild.ProviderConfig
