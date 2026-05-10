package llm

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/facebookincubator/go-belt/tool/logger"
)

const (
	defaultClaudeCodeEffort = "low"
	claudeCodeSettingsJSON  = `{"apiKeyHelper":"jq -r .claudeAiOauth.accessToken ~/.claude/.credentials.json"}`
)

type ClaudeCodeProvider struct {
	Model  string
	Effort string
}

func (p *ClaudeCodeProvider) Name() string {
	return "claude-code"
}

// TranslateWithMetadata satisfies Provider. The claude CLI wrapper does
// not surface per-stage timing back to us (no JSON envelope to read), so
// timings are nil for this backend.
func (p *ClaudeCodeProvider) TranslateWithMetadata(
	ctx context.Context,
	systemPrompt string,
	userPrompt string,
) (string, *TranslateMetadata, error) {
	result, err := p.Translate(ctx, systemPrompt, userPrompt)
	return result, nil, err
}

func (p *ClaudeCodeProvider) Translate(
	ctx context.Context,
	systemPrompt string,
	userPrompt string,
) (_ret string, _err error) {
	logger.Tracef(ctx, "ClaudeCodeProvider.Translate")
	defer func() { logger.Tracef(ctx, "/ClaudeCodeProvider.Translate: %v", _err) }()

	// Use --bare for minimal overhead. OAuth token is read via
	// apiKeyHelper from ~/.claude/.credentials.json.
	args := []string{
		"-p",
		"--bare",
		"--settings", claudeCodeSettingsJSON,
		"--allowedTools", "",
		"--system-prompt", systemPrompt,
	}

	if p.Model != "" {
		args = append(args, "--model", p.Model)
	}

	effort := p.Effort
	if effort == "" {
		effort = defaultClaudeCodeEffort
	}
	args = append(args, "--effort", effort)

	args = append(args, userPrompt)

	logger.Debugf(ctx, "running: claude %s", strings.Join(args[:len(args)-1], " "))
	cmd := exec.CommandContext(ctx, "claude", args...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = strings.TrimSpace(stdout.String())
		}
		switch {
		case detail != "":
			return "", fmt.Errorf("claude CLI: %w: %s", err, detail)
		default:
			return "", fmt.Errorf("claude CLI: %w", err)
		}
	}

	return strings.TrimSpace(stdout.String()), nil
}
