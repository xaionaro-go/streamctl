//go:build integration

package llm

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClaudeCodeTranslate(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	p := &ClaudeCodeProvider{
		Model:  "haiku",
		Effort: "low",
	}

	result, err := p.Translate(
		ctx,
		"You are a translator. Translate to English. Output ONLY the translated text. Nothing else.",
		"Bonjour le monde",
	)
	require.NoError(t, err)
	assert.Contains(t, result, "Hello")
	t.Logf("translated: %q", result)
}

func TestClaudeCodeTranslatePreservesEmoji(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	p := &ClaudeCodeProvider{
		Model:  "haiku",
		Effort: "low",
	}

	result, err := p.Translate(
		ctx,
		"You are a translator. Translate to English. Output ONLY the translated text. Nothing else. Preserve emoji.",
		"🎉",
	)
	require.NoError(t, err)
	assert.Contains(t, result, "🎉")
	t.Logf("translated: %q", result)
}

var benchModels = []string{"haiku", "sonnet"}
var benchEfforts = []string{"low", "medium", "high"}

func BenchmarkClaudeCodeTranslate(b *testing.B) {
	ctx := context.Background()

	for _, model := range benchModels {
		for _, effort := range benchEfforts {
			name := fmt.Sprintf("model=%s/effort=%s", model, effort)
			b.Run(name, func(b *testing.B) {
				p := &ClaudeCodeProvider{
					Model:  model,
					Effort: effort,
				}

				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					result, err := p.Translate(
						ctx,
						"You are a translator. Translate to English. Output ONLY the translated text.",
						"Bonjour le monde",
					)
					if err != nil {
						b.Fatal(err)
					}
					_ = result
				}
			})
		}
	}
}
