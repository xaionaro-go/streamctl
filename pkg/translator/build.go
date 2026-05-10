package translator

import (
	"context"

	llms "github.com/xaionaro-go/streamctl/pkg/llm"
	"github.com/xaionaro-go/streamctl/pkg/llm/translatorbuild"
)

// Config is the YAML schema the translator subprocess receives via the
// Reconfigure RPC. It is the canonical translatorbuild.Config re-exported so
// existing call sites that import "pkg/translator" do not change. New code
// SHOULD import translatorbuild directly.
type Config = translatorbuild.Config

// ProviderConfig is one entry in Config.Providers. Re-exported alias for
// translatorbuild.ProviderConfig.
type ProviderConfig = translatorbuild.ProviderConfig

// ParseConfigYAML decodes the wire YAML into a Config. Thin wrapper around
// translatorbuild.ParseConfigYAML kept here so the subprocess server.go does
// not have to add a second import.
func ParseConfigYAML(b []byte) (Config, error) {
	return translatorbuild.ParseConfigYAML(b)
}

// BuildChain builds a TranslatorChain from a parsed Config. Thin wrapper
// around translatorbuild.BuildChain — single source of truth lives there.
func BuildChain(
	ctx context.Context,
	cfg Config,
) (*llms.TranslatorChain, error) {
	return translatorbuild.BuildChain(ctx, cfg)
}
