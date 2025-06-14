package config

import (
	"context"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/xpath"
)

const (
	DefaultChatMessagesPath = "~/.streampanel.chat-messages"
)

func (cfg *Config) GetChatMessageStorage(
	ctx context.Context,
) string {
	if cfg.ChatMessagesStorage != nil {
		return *cfg.ChatMessagesStorage
	}

	path, err := xpath.Expand(DefaultChatMessagesPath)
	if err != nil {
		logger.Errorf(ctx, "unable to expand '%s': %w", DefaultChatMessagesPath, err)
		return ".streampanel.chat-messages"
	}

	return path
}
