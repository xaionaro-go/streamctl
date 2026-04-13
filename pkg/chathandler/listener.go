package chathandler

import (
	"context"

	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
)

// ChatListener abstracts a platform-specific chat event source.
// Each platform provides a primary and fallback implementation.
type ChatListener interface {
	// Name returns a human-readable name for this listener (e.g., "EventSub", "IRC").
	Name() string

	// Listen starts receiving chat events and returns a channel of events.
	// The channel is closed when the context is cancelled or the listener fails permanently.
	Listen(ctx context.Context) (<-chan streamcontrol.Event, error)

	// Close stops the listener and releases resources.
	Close(ctx context.Context) error
}
