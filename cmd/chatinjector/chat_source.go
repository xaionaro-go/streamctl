package main

import (
	"context"

	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
)

// messageContent returns the text content of an event's message,
// or empty string if no message is attached.
func messageContent(ev streamcontrol.Event) string {
	if ev.Message == nil {
		return ""
	}
	return ev.Message.Content
}

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
