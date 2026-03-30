package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/facebookincubator/go-belt/tool/logger"
)

const maxErrorBodySize = 1024

// Provider translates text using a specific LLM backend.
type Provider interface {
	Name() string
	Translate(
		ctx context.Context,
		systemPrompt string,
		userPrompt string,
	) (string, error)
}

// ChatMessage is the message format shared by Ollama and OpenAI APIs.
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func DoPost(
	ctx context.Context,
	url string,
	headers map[string]string,
	reqBody any,
	respBody any,
) (_err error) {
	logger.Tracef(ctx, "DoPost")
	defer func() { logger.Tracef(ctx, "/DoPost: %v", _err) }()

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
