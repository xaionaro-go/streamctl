package llm

import (
	"context"
	"fmt"
	"strings"

	"github.com/facebookincubator/go-belt/tool/logger"
)

const ollamaTemperature = 0

type OllamaProvider struct {
	APIURL string
	Model  string
}

func (p *OllamaProvider) Name() string {
	return fmt.Sprintf("ollama(%s)", p.Model)
}

func (p *OllamaProvider) Translate(
	ctx context.Context,
	systemPrompt string,
	userPrompt string,
) (_ret string, _err error) {
	logger.Tracef(ctx, "OllamaProvider.Translate")
	defer func() { logger.Tracef(ctx, "/OllamaProvider.Translate: %v", _err) }()

	type request struct {
		Model    string        `json:"model"`
		Messages []ChatMessage `json:"messages"`
		Stream   bool          `json:"stream"`
		Think    bool          `json:"think"`
		Options  struct {
			Temperature float64 `json:"temperature"`
		} `json:"options"`
	}

	req := request{
		Model: p.Model,
		Messages: []ChatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
		Think:  false,
		Stream: false,
	}
	req.Options.Temperature = ollamaTemperature

	type response struct {
		Message ChatMessage `json:"message"`
	}

	url := strings.TrimRight(p.APIURL, "/") + "/api/chat"

	var resp response
	if err := DoPost(ctx, url, nil, req, &resp); err != nil {
		return "", err
	}

	return resp.Message.Content, nil
}
