package streampanel

import (
	"context"
	"fmt"
	"strings"

	"github.com/facebookincubator/go-belt/tool/logger"
)

func (p *Panel) generateNewTitle(
	ctx context.Context,
) {
	profile := p.getSelectedProfile()

	tagsString := strings.Join(profile.TopicTags, ", ")
	logger.Debugf(ctx, "tags == %s", tagsString)

	// the prompt is developed by noguri
	t, err := p.generateAlternativeTextFor(ctx, fmt.Sprintf(`Based on my keywords, generate one short YouTube title for a stream.

Start with a red dot emoji (🔴).

Keep it under 140 characters.

Use a calm, cozy, aesthetic vibe (not loud, not overhyped).

Make it sound viral and inviting but still relaxed.

Always include the word "Stream" or "Live" naturally in the title.

End with 4-6 high-rated fitting hashtags related to the topic.

My keywords: %s`, tagsString))
	if err != nil {
		p.DisplayError(err)
		return
	}
	p.streamTitleField.SetText(t)
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
