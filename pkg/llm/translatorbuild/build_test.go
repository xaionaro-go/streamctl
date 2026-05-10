package translatorbuild

import (
	"context"
	"strings"
	"testing"

	tassert "github.com/stretchr/testify/assert"
	trequire "github.com/stretchr/testify/require"
)

// TestBuildChain_ExplicitDuplicateNamesReject locks in the contract that
// two providers sharing an explicit non-empty Name are rejected. Without
// the rejection, `streamcli translator providers remove <name>` would
// silently target the first matching entry and operators would lose track
// of which one they removed.
func TestBuildChain_ExplicitDuplicateNamesReject(t *testing.T) {
	cfg := Config{
		TargetLanguage: "English",
		Providers: []ProviderConfig{
			{Name: "primary", Type: "ollama", APIURL: "http://localhost:11434", Model: "llama3"},
			{Name: "primary", Type: "ollama", APIURL: "http://localhost:11435", Model: "llama3"},
		},
	}

	chain, err := BuildChain(context.Background(), cfg)
	trequire.Error(t, err, "explicit duplicate Name must error")
	tassert.Nil(t, chain, "rejected config must not return a chain")
	tassert.True(t, strings.Contains(err.Error(), "duplicate provider name"),
		"error must mention duplicate provider name; got %q", err.Error())
	tassert.True(t, strings.Contains(err.Error(), "primary"),
		"error must echo the offending name; got %q", err.Error())
}

// TestBuildChain_UnnamedSameTypeAutoDisambiguate is the migration safety
// net: two `ollama` blocks with no .name set must NOT collide. Pre-fix,
// both defaulted to Name="ollama" and BuildChain rejected the config —
// breaking every existing multi-ollama setup. Post-fix the auto-assigned
// names are "ollama#0" and "ollama#1" and the build succeeds.
func TestBuildChain_UnnamedSameTypeAutoDisambiguate(t *testing.T) {
	cfg := Config{
		TargetLanguage: "English",
		Providers: []ProviderConfig{
			{Type: "ollama", APIURL: "http://localhost:11434", Model: "llama3"},
			{Type: "ollama", APIURL: "http://localhost:11435", Model: "llama3"},
		},
	}

	chain, err := BuildChain(context.Background(), cfg)
	trequire.NoError(t, err, "two unnamed same-type providers must build cleanly")
	trequire.NotNil(t, chain, "successful build must return a chain")

	// The auto-assignment must mutate cfg.Providers in place so the names
	// used downstream are visible to operators inspecting the config later
	// (e.g. via the providers-list RPC).
	tassert.Equal(t, "ollama#0", cfg.Providers[0].Name,
		"first unnamed provider must be auto-assigned <type>#0")
	tassert.Equal(t, "ollama#1", cfg.Providers[1].Name,
		"second unnamed provider must be auto-assigned <type>#1")
}

// TestBuildChain_ExplicitNameWinsOverAuto locks in that auto-assignment
// only fires when Name is empty: explicit names are kept verbatim and
// participate in the duplicate check unchanged.
func TestBuildChain_ExplicitNameWinsOverAuto(t *testing.T) {
	cfg := Config{
		TargetLanguage: "English",
		Providers: []ProviderConfig{
			{Name: "fast", Type: "ollama", APIURL: "http://localhost:11434", Model: "llama3"},
			{Type: "ollama", APIURL: "http://localhost:11435", Model: "llama3"},
		},
	}
	_, err := BuildChain(context.Background(), cfg)
	trequire.NoError(t, err)
	tassert.Equal(t, "fast", cfg.Providers[0].Name, "explicit Name preserved")
	tassert.Equal(t, "ollama#1", cfg.Providers[1].Name,
		"unnamed sibling auto-disambiguated by index, not by available type slot")
}
