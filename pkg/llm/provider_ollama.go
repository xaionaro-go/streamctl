package llm

import (
	"context"
	"fmt"
	"strings"
	"time"

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
) (string, error) {
	result, _, err := p.TranslateWithMetadata(ctx, systemPrompt, userPrompt)
	return result, err
}

func (p *OllamaProvider) TranslateWithMetadata(
	ctx context.Context,
	systemPrompt string,
	userPrompt string,
) (_ret string, _meta *TranslateMetadata, _err error) {
	logger.Tracef(ctx, "OllamaProvider.TranslateWithMetadata")
	defer func() { logger.Tracef(ctx, "/OllamaProvider.TranslateWithMetadata: %v", _err) }()

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

	// Ollama returns timing fields on every /api/chat response (durations in
	// nanoseconds, counts in tokens). See ollama/docs/api.md. Capturing them
	// here splits a slow round-trip into model-load vs prompt-eval vs
	// generation, which is what we need to investigate latency outliers.
	type response struct {
		Message            ChatMessage `json:"message"`
		TotalDuration      int64       `json:"total_duration"`
		LoadDuration       int64       `json:"load_duration"`
		PromptEvalCount    int         `json:"prompt_eval_count"`
		PromptEvalDuration int64       `json:"prompt_eval_duration"`
		EvalCount          int         `json:"eval_count"`
		EvalDuration       int64       `json:"eval_duration"`
	}

	url := strings.TrimRight(p.APIURL, "/") + "/api/chat"

	logger.Debugf(ctx, "ollama %s request: msgs=%d sys_chars=%d user_chars=%d",
		p.Model, len(req.Messages), len(systemPrompt), len(userPrompt))

	var resp response
	if err := DoPost(ctx, url, nil, req, &resp); err != nil {
		return "", nil, err
	}

	logger.Debugf(ctx,
		"ollama %s timings: total=%v load=%v prompt=%v(%d tok) eval=%v(%d tok, %.1f tok/s)",
		p.Model,
		time.Duration(resp.TotalDuration),
		time.Duration(resp.LoadDuration),
		time.Duration(resp.PromptEvalDuration), resp.PromptEvalCount,
		time.Duration(resp.EvalDuration), resp.EvalCount,
		tokensPerSecond(resp.EvalCount, resp.EvalDuration),
	)

	meta := &TranslateMetadata{
		TotalDurationNanos:      resp.TotalDuration,
		LoadDurationNanos:       resp.LoadDuration,
		PromptEvalDurationNanos: resp.PromptEvalDuration,
		EvalDurationNanos:       resp.EvalDuration,
		PromptEvalTokens:        int64(resp.PromptEvalCount),
		EvalTokens:              int64(resp.EvalCount),
	}
	return resp.Message.Content, meta, nil
}

// tokensPerSecond converts an Ollama (count, duration-ns) pair into tok/s for
// log output. Returns 0 when duration is non-positive so a malformed or absent
// timing field cannot panic with a divide-by-zero.
func tokensPerSecond(count int, durationNs int64) float64 {
	if durationNs <= 0 {
		return 0
	}
	return float64(count) * float64(time.Second) / float64(durationNs)
}
