//go:build integration

package llm

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testOllamaURLChain = "http://192.168.0.171:11434"

func newTestChain(t *testing.T) *TranslatorChain {
	t.Helper()
	return NewTranslatorChain("English", 20, []ProviderEntry{{
		Provider:    &OllamaProvider{APIURL: testOllamaURLChain, Model: "qwen3:8b"},
		Parallelism: 1,
	}})
}

func TestTranslate(t *testing.T) {
	type testCase struct {
		name     string
		user     string
		input    string
		wantSame bool              // expect output == input (English, emoji-only)
		contains []string          // output must contain all of these (case-insensitive)
		notEqual bool              // output must differ from input
		check    func(t *testing.T, input, output string) // custom check
	}

	cases := []testCase{
		{
			name:     "Spanish",
			user:     "IvanMartines",
			input:    "ola mi amor estás muy linda 😍",
			contains: []string{"😍"},
			notEqual: true,
		},
		{
			name:     "Turkish",
			user:     "Zafer",
			input:    "merhaba arkadaşım",
			contains: []string{"hello"},
			notEqual: true,
		},
		{
			name:     "Turkish/cold",
			user:     "Zafer",
			input:    "orası çok soğuk",
			contains: []string{"cold"},
			notEqual: true,
		},
		{
			name:     "Mixed Turkish+English",
			user:     "Zekeriya",
			input:    "Hiiiiiiiiii Aşkim Aşkim",
			notEqual: true,
			contains: []string{"love"},
		},
		{
			name: "Hindi meaning not transliteration",
			user: "tushar",
			input: "नमस्कार ☺️",
			contains: []string{"hello", "☺️"},
			check: func(t *testing.T, _, output string) {
				assert.NotContains(t, output, "Namaste", "should translate meaning, not transliterate")
			},
		},
		{
			name:     "Turkish with typo",
			user:     "Zafer",
			input:    "içecek neiçirirsin?",
			notEqual: true,
			contains: []string{"drink"},
			check: func(t *testing.T, _, output string) {
				assert.NotContains(t, strings.ToLower(output), "not drink",
					"should not misinterpret typo as negation")
			},
		},
		{
			name:     "Portuguese",
			user:     "Adelson",
			input:    "Oi Belém do Pará Brasil norte Amazônia",
			notEqual: true,
		},
		{
			name:     "Indonesian",
			user:     "agam",
			input:    "halo",
			contains: []string{"hello"},
		},
		{
			name:     "English unchanged",
			user:     "Daniel",
			input:    "Hi How r U today good morning",
			wantSame: true,
		},
		{
			name:     "English with typos unchanged",
			user:     "Faouzi",
			input:    "Hello Vickey. faouzi fom Tunisia",
			wantSame: true,
		},
		{
			name:     "English informal unchanged",
			user:     "Malaya",
			input:    "YT having problem with the livestream viewers count section if u are seeing less viewers just bear with it for now🤣",
			wantSame: true,
		},
		{
			name:     "Emoji only",
			user:     "user",
			input:    "🎉",
			wantSame: true,
		},
		{
			name:     "Emoji only angry",
			user:     "Zafer",
			input:    "😡",
			wantSame: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			chain := newTestChain(t)
			result, err := chain.Translate(ctx, tc.user, tc.input)
			require.NoError(t, err)
			t.Logf("input:  %q", tc.input)
			t.Logf("output: %q", result)

			if tc.wantSame {
				assert.Equal(t, tc.input, result, "should be returned unchanged")
				return
			}

			if tc.notEqual {
				assert.NotEqual(t, tc.input, result, "should be translated")
			}

			for _, s := range tc.contains {
				assert.Contains(t, strings.ToLower(result), strings.ToLower(s))
			}

			if tc.check != nil {
				tc.check(t, tc.input, result)
			}
		})
	}
}
