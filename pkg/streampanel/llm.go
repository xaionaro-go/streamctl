package streampanel

import (
	"context"
	"fmt"
	"strings"
)

func (p *Panel) generateNewTitle(
	ctx context.Context,
) {
	// the prompt is developed by noguri
	t, err := p.generateAlternativeTextFor(ctx, `Based on my keywords, generate one short YouTube single-line title for a stream.

Start with a red dot emoji (🔴).

Keep it under 95 characters.

Use not loud / not overhyped language.

Make it sound viral and inviting but still relaxed.

Always include the word "Stream" or "Live" naturally in the title.`)
	if err != nil {
		p.DisplayError(err)
		return
	}
	t = strings.ReplaceAll(t, "\n", " | ")
	t = strings.Join(strings.Fields(t), " ") // multi-spaces -> single-spaces
	p.streamTitleField.SetText(t)
	p.streamTitleLabel.SetText(t)
}

func (p *Panel) generateNewDescription(
	ctx context.Context,
) {
	t, err := p.generateAlternativeTextFor(
		ctx,
		fmt.Sprintf(
			"I'm about to go live on YouTube and Twitch. Suggest a viral description alternative for the stream, given the current description is '%s'.",
			p.streamDescriptionField.Text,
		),
	)
	if err != nil {
		p.DisplayError(err)
		return
	}
	p.streamDescriptionField.SetText(t)
	p.streamDescriptionLabel.SetText(t)
}

func (p *Panel) generateAlternativeTextFor(
	ctx context.Context,
	what string,
) (string, error) {
	streamD, err := p.GetStreamD(ctx)
	if err != nil {
		return "", fmt.Errorf("unable to get StreamD client: %w", err)
	}

	return streamD.LLMGenerate(ctx, what)
}
