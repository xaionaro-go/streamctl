package chatmessagesstorage

import (
	"github.com/xaionaro-go/streamctl/pkg/streamd/api"
	"github.com/xaionaro-go/xsync"
)

const (
	MaxMessages = 1000
)

type ChatMessagesStorage struct {
	xsync.Mutex
	FilePath  string
	IsSorted  bool
	IsChanged bool
	Messages  []api.ChatMessage
}

func New(
	filePath string,
) *ChatMessagesStorage {
	return &ChatMessagesStorage{
		FilePath: filePath,
	}
}
