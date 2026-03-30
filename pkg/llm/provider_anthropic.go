package llm

import (
	"context"
	"fmt"
	"strings"

	"github.com/facebookincubator/go-belt/tool/logger"
)

const (
	anthropicAPIVersion = "2023-06-01"
	anthropicMaxTokens  = 1024
)

type AnthropicProvider struct {
	APIURL string
	APIKey string
	Model  string
}

func (p *AnthropicProvider) Name() string {
	return fmt.Sprintf("anthropic(%s)", p.Model)
}

func (p *AnthropicProvider) Translate(
	ctx context.Context,
	systemPrompt string,
	userPrompt string,
) (_ret string, _err error) {
	logger.Tracef(ctx, "AnthropicProvider.Translate")
	defer func() { logger.Tracef(ctx, "/AnthropicProvider.Translate: %v", _err) }()

	type message struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}

	type request struct {
		Model     string    `json:"model"`
		MaxTokens int       `json:"max_tokens"`
		System    string    `json:"system"`
		Messages  []message `json:"messages"`
	}

	type contentBlock struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}

	type response struct {
		Content []contentBlock `json:"content"`
	}

	url := strings.TrimRight(p.APIURL, "/") + "/v1/messages"

	headers := map[string]string{
		"x-api-key":         p.APIKey,
		"anthropic-version": anthropicAPIVersion,
	}

	var resp response
	if err := DoPost(ctx, url, headers, request{
		Model:     p.Model,
		MaxTokens: anthropicMaxTokens,
		System:    systemPrompt,
		Messages: []message{
			{Role: "user", Content: userPrompt},
		},
	}, &resp); err != nil {
		return "", err
	}

	if len(resp.Content) == 0 {
		return "", fmt.Errorf("no content blocks in Anthropic response")
	}

	return resp.Content[0].Text, nil
}
