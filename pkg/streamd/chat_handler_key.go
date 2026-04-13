package streamd

import (
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
)

// chatHandlerKey identifies a specific chat handler process by platform and listener type.
type chatHandlerKey struct {
	Platform     streamcontrol.PlatformName
	ListenerType streamcontrol.ChatListenerType
}
