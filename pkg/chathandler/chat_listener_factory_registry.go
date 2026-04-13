package chathandler

import (
	"sync"

	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
)

var (
	factoriesMu sync.RWMutex
	factories   = map[streamcontrol.PlatformName]ChatListenerFactory{}
)

// RegisterChatListenerFactory registers a factory for a given platform.
// Subsequent calls for the same platform overwrite the previous factory.
func RegisterChatListenerFactory(f ChatListenerFactory) {
	factoriesMu.Lock()
	defer factoriesMu.Unlock()
	factories[f.PlatformName()] = f
}

// GetChatListenerFactory returns the registered factory for the given
// platform, or nil if none is registered.
func GetChatListenerFactory(name streamcontrol.PlatformName) ChatListenerFactory {
	factoriesMu.RLock()
	defer factoriesMu.RUnlock()
	return factories[name]
}
