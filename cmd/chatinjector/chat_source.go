package main

import (
	"context"
	"fmt"

	"github.com/facebookincubator/go-belt/tool/logger"
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

// FallbackSource tries each source in order. When the active source's Run()
// returns an error, it logs the failure and moves to the next source.
// Returns nil only if a source's Run() returned nil (clean shutdown).
// Returns error only if ALL sources have been exhausted.
type FallbackSource struct {
	Sources []ChatSource
}

func (s *FallbackSource) PlatformID() streamcontrol.PlatformName {
	return s.Sources[0].PlatformID()
}

func (s *FallbackSource) Run(
	ctx context.Context,
	events chan<- ChatEvent,
) error {
	for i, src := range s.Sources {
		logger.Debugf(ctx, "trying source %d/%d: %s", i+1, len(s.Sources), src.PlatformID())
		err := src.Run(ctx, events)
		if err == nil || ctx.Err() != nil {
			return err
		}
		logger.Warnf(ctx, "source %s failed: %v", src.PlatformID(), err)
		if i < len(s.Sources)-1 {
			logger.Debugf(ctx, "falling back to next source")
		}
	}
	return fmt.Errorf("all %d sources exhausted for %s", len(s.Sources), s.Sources[0].PlatformID())
}
