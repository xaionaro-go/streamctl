//go:build integration

package llm

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testOllamaURL = "http://192.168.0.171:11434"

func TestOllamaTranslate(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	p := &OllamaProvider{
		APIURL: testOllamaURL,
		Model:  "qwen3:30b-instruct",
	}

	result, err := p.Translate(
		ctx,
		"You are a translator. Translate to English. Output ONLY the translated text. Nothing else.",
		"Bonjour le monde",
	)
	require.NoError(t, err)
	t.Logf("translated: %q", result)
	assert.Contains(t, result, "Hello")
}

func BenchmarkOllamaTranslate(b *testing.B) {
	ctx := context.Background()

	models := []string{"qwen3:30b-instruct"}

	for _, model := range models {
		b.Run("model="+model, func(b *testing.B) {
			p := &OllamaProvider{
				APIURL: testOllamaURL,
				Model:  model,
			}

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, err := p.Translate(
					ctx,
					"You are a translator. Translate to English. Output ONLY the translated text.",
					"Bonjour le monde",
				)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
