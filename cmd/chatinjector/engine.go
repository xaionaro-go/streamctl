package main

import (
	"context"
	"fmt"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/streamctl/pkg/llm"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	scgoconv "github.com/xaionaro-go/streamctl/pkg/streamcontrol/protobuf/goconv"
	"github.com/xaionaro-go/streamctl/pkg/streamd/grpc/go/streamd_grpc"
)

const translationSeparator = " -文A-> "

// Engine reads ChatEvents, optionally translates message content, and injects
// them into streamd. It is platform-agnostic.
type Engine struct {
	StreamdClient streamd_grpc.StreamDClient
	Chain         *llm.TranslatorChain // nil if translation disabled
}

// Run consumes events from the channel, translates concurrently while
// injecting sequentially (preserving message order), and returns when the
// channel is closed or the context is cancelled.
func (e *Engine) Run(
	ctx context.Context,
	events <-chan ChatEvent,
) (_err error) {
	logger.Tracef(ctx, "Engine.Run")
	defer func() { logger.Tracef(ctx, "/Engine.Run: %v", _err) }()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case chatEv, ok := <-events:
			if !ok {
				return nil
			}
			e.processBatch(ctx, chatEv, events)
		}
	}
}

// processBatch handles the first event plus any additional events already
// buffered in the channel. It translates concurrently but injects in order.
func (e *Engine) processBatch(
	ctx context.Context,
	first ChatEvent,
	events <-chan ChatEvent,
) {
	// Collect the first event plus everything buffered.
	batch := []ChatEvent{first}
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				break
			}
			batch = append(batch, ev)
			continue
		default:
		}
		break
	}

	// Translate concurrently, inject in order.
	type result struct {
		ev       streamcontrol.Event
		platform string
	}
	slots := make([]chan result, len(batch))

	for i, chatEv := range batch {
		slots[i] = make(chan result, 1)
		i, chatEv := i, chatEv
		observability.Go(ctx, func(ctx context.Context) {
			ev := chatEv.Event
			// Guarantee the slot is always filled, even on panic.
			sent := false
			defer func() {
				if !sent {
					slots[i] <- result{ev: chatEv.Event, platform: string(chatEv.Platform)}
				}
			}()

			if e.Chain != nil && ev.Message != nil && ev.Message.Content != "" {
				translated, translateErr := e.Chain.Translate(ctx, ev.User.Name, ev.Message.Content)
				switch {
				case translateErr != nil:
					logger.Warnf(ctx, "translation failed for %s: %v", ev.ID, translateErr)
				case translated != ev.Message.Content:
					ev.Message.Content = ev.Message.Content + translationSeparator + translated
				}
			}

			slots[i] <- result{ev: ev, platform: string(chatEv.Platform)}
			sent = true
		})
	}

	for _, slot := range slots {
		r := <-slot
		logger.Debugf(ctx, "injecting %s event from %s: %s",
			r.ev.Type, r.ev.User.Name, messagePreview(&r.ev))

		_, injectErr := e.StreamdClient.InjectChatMessage(ctx, &streamd_grpc.InjectChatMessageRequest{
			PlatID: r.platform,
			Event:  scgoconv.EventGo2GRPC(r.ev),
		})
		if injectErr != nil {
			logger.Errorf(ctx, "InjectChatMessage failed for %s: %v", r.ev.ID, injectErr)
		}
	}
}

func messagePreview(ev *streamcontrol.Event) string {
	if ev.Message == nil {
		return fmt.Sprintf("(%s)", ev.Type)
	}

	content := ev.Message.Content
	if len(content) > messagePreviewMax {
		content = content[:messagePreviewMax] + "..."
	}

	return content
}
