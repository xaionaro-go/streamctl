package chathandler

import (
	"fmt"
	"strings"

	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
)

// ErrChatListenerMisconfigured is returned when a factory supports the
// requested ChatListenerType but cannot create it due to missing or
// incomplete configuration (e.g., missing credentials, missing proxy address).
type ErrChatListenerMisconfigured struct {
	PlatformName  streamcontrol.PlatformName
	ListenerType  streamcontrol.ChatListenerType
	MissingFields []string
}

func (e ErrChatListenerMisconfigured) Error() string {
	return fmt.Sprintf(
		"chat listener type %s for platform %s is misconfigured: missing %s",
		e.ListenerType, e.PlatformName, strings.Join(e.MissingFields, ", "),
	)
}
