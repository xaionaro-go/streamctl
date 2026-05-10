package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/facebookincubator/go-belt/tool/logger"
)

const maxErrorBodySize = 1024

// TranslateMetadata carries optional per-call backend telemetry surfaced
// alongside the translated text. Only providers that expose timing/token
// fields on their responses populate any of these — others return nil from
// TranslateWithMetadata and the operator-facing render simply omits the
// secondary line.
//
// All fields are nanoseconds (durations) or absolute counts (tokens). int64
// matches the existing latencyNanos width on the proto so wire-level
// representation stays uniform.
//
// As of this change, only Ollama populates TranslateMetadata via the timing
// fields it returns on every /api/chat response (total/load/prompt-eval/eval
// durations + prompt/eval token counts; see ollama/docs/api.md).
type TranslateMetadata struct {
	TotalDurationNanos      int64
	LoadDurationNanos       int64
	PromptEvalDurationNanos int64
	EvalDurationNanos       int64
	PromptEvalTokens        int64
	EvalTokens              int64
}

// Provider translates text using a specific LLM backend.
//
// Translate is the legacy entry point retained for callers that don't need
// per-call backend telemetry. New code SHOULD prefer TranslateWithMetadata
// so a slow round-trip can be split into model-load vs prompt-eval vs
// generation when the backend exposes those fields. Implementations make
// Translate a thin shim around TranslateWithMetadata so both stay in sync.
type Provider interface {
	Name() string
	Translate(
		ctx context.Context,
		systemPrompt string,
		userPrompt string,
	) (string, error)
	TranslateWithMetadata(
		ctx context.Context,
		systemPrompt string,
		userPrompt string,
	) (string, *TranslateMetadata, error)
}

// ChatMessage is the message format shared by Ollama and OpenAI APIs.
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// DoPost owns the full LLM HTTP-request lifecycle: marshal the request body,
// dispatch the POST, validate the status, decode the response. The Debug
// entry/exit pair below records every attempt and its terminal disposition —
// transport error, non-200 status, JSON marshal/decode failure, or success —
// so per-request observability is uniform across all callers (Ollama, OpenAI,
// Anthropic, future backends) and no failure mode is silent.
func DoPost(
	ctx context.Context,
	url string,
	headers map[string]string,
	reqBody any,
	respBody any,
) (_err error) {
	logger.Debugf(ctx, "DoPost: POST %s", url)
	start := time.Now()
	defer func() { logger.Debugf(ctx, "/DoPost: POST %s in %v: %v", url, time.Since(start), _err) }()

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("post %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodySize))
		return fmt.Errorf("LLM returned %d: %s", resp.StatusCode, body)
	}

	if err := json.NewDecoder(resp.Body).Decode(respBody); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}

	return nil
}
