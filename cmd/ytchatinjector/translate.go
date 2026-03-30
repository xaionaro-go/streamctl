package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/facebookincubator/go-belt/tool/logger"
)

const (
	llmDefaultModel      = "qwen3:8b"
	chatHistoryMaxLength = 20
)

type chatHistoryEntry struct {
	User    string
	Message string
}

// Translator translates chat messages using an OpenAI-compatible LLM API.
// Works with both Ollama (via /api/chat) and OpenAI (via /v1/chat/completions).
type Translator struct {
	APIURL     string
	APIKey     string
	Model      string
	TargetLang string

	historyMu sync.Mutex
	history   []chatHistoryEntry
}

func (t *Translator) addToHistory(user string, message string) {
	t.historyMu.Lock()
	defer t.historyMu.Unlock()

	t.history = append(t.history, chatHistoryEntry{User: user, Message: message})
	if len(t.history) > chatHistoryMaxLength {
		t.history = t.history[len(t.history)-chatHistoryMaxLength:]
	}
}

func (t *Translator) formatHistory() string {
	t.historyMu.Lock()
	defer t.historyMu.Unlock()

	var b strings.Builder
	for _, e := range t.history {
		fmt.Fprintf(&b, "<%s> %s\n", e.User, e.Message)
	}
	return b.String()
}

// Translate translates the given message to the target language.
func (t *Translator) Translate(
	ctx context.Context,
	user string,
	message string,
) (_ret string, _err error) {
	logger.Tracef(ctx, "Translate")
	defer func() { logger.Tracef(ctx, "/Translate: %v", _err) }()

	history := t.formatHistory()

	systemPrompt := fmt.Sprintf(
		`You are a translator. Translate the content of <message> to %s.

The input uses XML tags:
- <history> contains recent chat messages for context. Do NOT translate these.
- <message> contains the single message to translate.

RULES:
- Output ONLY the translated text of <message>. Nothing else.
- No XML tags, no explanations, no quotes, no prefixes, no labels in your output.
- If the message is already in %s, output it unchanged.
- Preserve emoji and special characters as-is.`,
		t.TargetLang, t.TargetLang,
	)

	userPrompt := fmt.Sprintf(
		"<history>\n%s</history>\n<message author=\"%s\">%s</message>",
		history, user, message,
	)

	translated, err := t.chatCompletion(ctx, systemPrompt, userPrompt)
	if err != nil {
		return "", fmt.Errorf("LLM chat completion: %w", err)
	}

	translated = strings.TrimSpace(translated)
	t.addToHistory(user, message)

	logger.Debugf(ctx, "translated [%s]: %q -> %q", user, message, translated)
	return translated, nil
}

// chatMessage is shared between Ollama and OpenAI request/response formats.
type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// chatCompletion calls the LLM API. It auto-detects whether to use the
// Ollama /api/chat endpoint or the OpenAI /v1/chat/completions endpoint
// based on whether an API key is configured.
func (t *Translator) chatCompletion(
	ctx context.Context,
	systemPrompt string,
	userPrompt string,
) (_ret string, _err error) {
	logger.Tracef(ctx, "chatCompletion")
	defer func() { logger.Tracef(ctx, "/chatCompletion: %v", _err) }()

	messages := []chatMessage{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userPrompt},
	}

	if t.APIKey != "" {
		return t.openAIChatCompletion(ctx, messages)
	}
	return t.ollamaChatCompletion(ctx, messages)
}

// ollamaChatCompletion uses the Ollama-native /api/chat endpoint.
func (t *Translator) ollamaChatCompletion(
	ctx context.Context,
	messages []chatMessage,
) (string, error) {
	type request struct {
		Model    string        `json:"model"`
		Messages []chatMessage `json:"messages"`
		Stream   bool          `json:"stream"`
		Options  struct {
			Temperature float64 `json:"temperature"`
		} `json:"options"`
	}

	req := request{
		Model:    t.Model,
		Messages: messages,
		Stream:   false,
	}
	req.Options.Temperature = 0.1

	type response struct {
		Message chatMessage `json:"message"`
	}

	url := strings.TrimRight(t.APIURL, "/") + "/api/chat"
	var resp response
	if err := t.doPost(ctx, url, req, &resp); err != nil {
		return "", err
	}
	return resp.Message.Content, nil
}

// openAIChatCompletion uses the OpenAI-compatible /v1/chat/completions endpoint.
func (t *Translator) openAIChatCompletion(
	ctx context.Context,
	messages []chatMessage,
) (string, error) {
	type request struct {
		Model       string        `json:"model"`
		Messages    []chatMessage `json:"messages"`
		Temperature float64       `json:"temperature"`
	}

	type choice struct {
		Message chatMessage `json:"message"`
	}
	type response struct {
		Choices []choice `json:"choices"`
	}

	url := strings.TrimRight(t.APIURL, "/") + "/v1/chat/completions"
	var resp response
	if err := t.doPost(ctx, url, request{
		Model:       t.Model,
		Messages:    messages,
		Temperature: 0.1,
	}, &resp); err != nil {
		return "", err
	}

	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("no choices in response")
	}
	return resp.Choices[0].Message.Content, nil
}

func (t *Translator) doPost(
	ctx context.Context,
	url string,
	reqBody any,
	respBody any,
) error {
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if t.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+t.APIKey)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("post %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("LLM returned %d: %s", resp.StatusCode, body)
	}

	if err := json.NewDecoder(resp.Body).Decode(respBody); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}
