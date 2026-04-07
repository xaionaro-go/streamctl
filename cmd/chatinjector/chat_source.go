package main

import (
	"context"

	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
)

// ChatEvent pairs a platform-agnostic event with the platform it came from.
type ChatEvent struct {
	Event    streamcontrol.Event
	Platform streamcontrol.PlatformName
}

// ChatSource produces chat events from a single platform.
// Run blocks until the context is cancelled or an unrecoverable error occurs,
// sending events to the provided channel. The implementation owns reconnect
// logic and page-token tracking.
type ChatSource interface {
	Run(ctx context.Context, events chan<- ChatEvent) error
	PlatformID() streamcontrol.PlatformName
}
