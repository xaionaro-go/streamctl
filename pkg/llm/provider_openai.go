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
