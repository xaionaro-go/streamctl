package defaultaudiotheme

import (
	_ "embed"
	"slices"

	"github.com/xaionaro-go/streamctl/pkg/audiotheme"
)

var (
	//go:embed resources/chat_message.ogg
	chatMessage []byte
)

func AudioTheme() audiotheme.AudioTheme {
	return audiotheme.AudioTheme{
		ChatMessage: slices.Clone(chatMessage),
	}
}
