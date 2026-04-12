package chathandler

import (
	"fmt"

	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
)

// ErrChatListenerTypeNotImplemented is returned when a factory does
// not support the requested ChatListenerType.
type ErrChatListenerTypeNotImplemented struct {
	PlatformName streamcontrol.PlatformName
	ListenerType streamcontrol.ChatListenerType
}

func (e ErrChatListenerTypeNotImplemented) Error() string {
	return fmt.Sprintf(
		"chat listener type %s is not implemented for platform %s",
		e.ListenerType, e.PlatformName,
	)
}
