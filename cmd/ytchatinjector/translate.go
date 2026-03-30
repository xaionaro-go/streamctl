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
	ollamaDefaultModel   = "qwen3:8b"
	chatHistoryMaxLength = 20
)

type chatHistoryEntry struct {
	User    string
	Message string
}

// Translator translates chat messages using an Ollama LLM.
// It maintains a sliding window of recent chat history for context.
type Translator struct {
	OllamaURL  string
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
// The user and message are added to the chat history after translation.
func (t *Translator) Translate(
	ctx context.Context,
	user string,
	message string,
) (_ret string, _err error) {
	logger.Tracef(ctx, "Translate")
	defer func() { logger.Tracef(ctx, "/Translate: %v", _err) }()

	history := t.formatHistory()

	prompt := fmt.Sprintf(
		`You are a translator. Translate the following chat message to %s.

RULES:
- Output ONLY the translated message text. Nothing else.
- No explanations, no quotes, no prefixes, no labels.
- If the message is already in %s, output it unchanged.
- Preserve emoji and special characters as-is.

Recent chat history for context:
%s
Message to translate (by <%s>):
%s`,
		t.TargetLang, t.TargetLang, history, user, message,
	)

	translated, err := t.callOllama(ctx, prompt)
	if err != nil {
		return "", fmt.Errorf("ollama: %w", err)
	}

	translated = strings.TrimSpace(translated)
	t.addToHistory(user, message)
	return translated, nil
}

type ollamaChatRequest struct {
	Model    string              `json:"model"`
	Messages []ollamaChatMessage `json:"messages"`
	Stream   bool                `json:"stream"`
	Options  *ollamaOptions      `json:"options,omitempty"`
}

type ollamaChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ollamaOptions struct {
	Temperature float64 `json:"temperature"`
}

type ollamaChatResponse struct {
	Message ollamaChatMessage `json:"message"`
}

func (t *Translator) callOllama(
	ctx context.Context,
	prompt string,
) (_ret string, _err error) {
	logger.Tracef(ctx, "callOllama")
	defer func() { logger.Tracef(ctx, "/callOllama: %v", _err) }()

	model := t.Model
	if model == "" {
		model = ollamaDefaultModel
	}

	reqBody := ollamaChatRequest{
		Model: model,
		Messages: []ollamaChatMessage{
			{Role: "user", Content: prompt},
		},
		Stream:  false,
		Options: &ollamaOptions{Temperature: 0.1},
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	url := strings.TrimRight(t.OllamaURL, "/") + "/api/chat"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("post %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return "", fmt.Errorf("ollama returned %d: %s", resp.StatusCode, body)
	}

	var chatResp ollamaChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}

	return chatResp.Message.Content, nil
}
