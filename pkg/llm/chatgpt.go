package llm

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/schema"
	"github.com/facebookincubator/go-belt/tool/logger"
	llmtypes "github.com/xaionaro-go/streamctl/pkg/llm/types"
	"github.com/xaionaro-go/streamctl/pkg/secret"
)

type ChatGPT struct {
	Model *openai.ChatModel
}

var _ llmtypes.LLM = (*ChatGPT)(nil)

func NewChatGPT(
	ctx context.Context,
	modelName string,
	apiKey secret.String,
) (_ret *ChatGPT, _err error) {
	logger.Debugf(ctx, "NewChatGPT(ctx, '%s', apiKey)", modelName)
	defer func() { logger.Debugf(ctx, "/NewChatGPT(ctx, '%s', apiKey): %v", modelName, _err) }()
	chatModel, err := openai.NewChatModel(ctx, &openai.ChatModelConfig{
		Model:  modelName,
		APIKey: apiKey.Get(),
	})
	if err != nil {
		return nil, fmt.Errorf("unable to initialize the ChatGPT model '%s': %w", modelName, err)
	}
	return &ChatGPT{
		Model: chatModel,
	}, nil
}

func (gpt *ChatGPT) Close() error {
	return nil
}

func (gpt *ChatGPT) Generate(
	ctx context.Context,
	prompt string,
) (string, error) {
	r, err := gpt.Model.Generate(ctx, []*schema.Message{
		{
			Role:    schema.System,
			Content: "You are a generator of texts that assists a streaming creator to increase the audience. You need to always answer only the resulting/required text itself that could be easily copy&paste-d.",
		},
		{
			Role:    schema.User,
			Content: prompt,
		},
	})
	if err != nil {
		return "", fmt.Errorf("unable to generate text: %w", err)
	}
	return r.Content, nil
}
