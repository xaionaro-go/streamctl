package youtube

import (
	"fmt"
)

type ErrChatNotFound struct {
	ChatID string
}

func (e ErrChatNotFound) Error() string {
	return fmt.Sprintf("chat '%s' not found", e.ChatID)
}

type ErrChatDisabled struct {
	ChatID string
}

func (e ErrChatDisabled) Error() string {
	return fmt.Sprintf("chat '%s' is disabled", e.ChatID)
}

type ErrChatEnded struct {
	ChatID string
}

func (e ErrChatEnded) Error() string {
	return fmt.Sprintf("chat '%s' ended", e.ChatID)
}
