package llm

import (
	"context"
	"fmt"
	"strings"

	"github.com/facebookincubator/go-belt/tool/logger"
)

const openaiTemperature = 0

type OpenAIProvider struct {
	APIURL string
	APIKey string
	Model  string
}

func (p *OpenAIProvider) Name() string {
	return fmt.Sprintf("openai(%s)", p.Model)
}

// TranslateWithMetadata satisfies Provider. OpenAI's /v1/chat/completions
// response carries token counts (usage.prompt_tokens / completion_tokens)
// but no per-stage timing breakdown, and we have not fetched the current
// API spec for this PR. Returning nil keeps the implementation honest:
// callers see "no timings available" rather than a fabricated subset.
func (p *OpenAIProvider) TranslateWithMetadata(
	ctx context.Context,
	systemPrompt string,
	userPrompt string,
) (string, *TranslateMetadata, error) {
	result, err := p.Translate(ctx, systemPrompt, userPrompt)
	return result, nil, err
}

func (p *OpenAIProvider) Translate(
	ctx context.Context,
	systemPrompt string,
	userPrompt string,
) (_ret string, _err error) {
	logger.Tracef(ctx, "OpenAIProvider.Translate")
	defer func() { logger.Tracef(ctx, "/OpenAIProvider.Translate: %v", _err) }()

	type request struct {
		Model       string        `json:"model"`
		Messages    []ChatMessage `json:"messages"`
		Temperature float64       `json:"temperature"`
	}

	type choice struct {
		Message ChatMessage `json:"message"`
	}

	type response struct {
		Choices []choice `json:"choices"`
	}

	url := strings.TrimRight(p.APIURL, "/") + "/v1/chat/completions"

	headers := map[string]string{
		"Authorization": "Bearer " + p.APIKey,
	}

	var resp response
	if err := DoPost(ctx, url, headers, request{
		Model: p.Model,
		Messages: []ChatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
		Temperature: openaiTemperature,
	}, &resp); err != nil {
		return "", err
	}

	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("no choices in OpenAI response")
	}

	return resp.Choices[0].Message.Content, nil
}
