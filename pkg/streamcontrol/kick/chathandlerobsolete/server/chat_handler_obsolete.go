package server

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/kickcom"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
)

type ChatClientOBSOLETE interface {
	GetChatMessagesV2(
		ctx context.Context,
		channelID uint64,
		cursor uint64,
	) (*kickcom.ChatMessagesV2Reply, error)
}

type ChatHandlerOBSOLETE struct {
	currentCursor   uint64
	channelID       uint64
	lastMessageID   string
	client          ChatClientOBSOLETE
	cancelFunc      context.CancelFunc
	messagesOutChan chan streamcontrol.Event
	onClose         func(context.Context, *ChatHandlerOBSOLETE)
}

func NewChatHandlerOBSOLETE(
	ctx context.Context,
	chatClient ChatClientOBSOLETE,
	channelID uint64,
	onClose func(context.Context, *ChatHandlerOBSOLETE),
) (_ret *ChatHandlerOBSOLETE, _err error) {
	logger.Debugf(ctx, "NewChatHandlerOBSOLETE(ctx, client, %d, %p)", channelID, onClose)
	defer func() {
		logger.Debugf(ctx, "/NewChatHandlerOBSOLETE(ctx, client, %d, %p): %#+v %v", channelID, onClose, _ret, _err)
	}()

	ctx, cancelFn := context.WithCancel(ctx)
	h := &ChatHandlerOBSOLETE{
		currentCursor: 0,
		client:        chatClient,
		channelID:     channelID,
		cancelFunc:    cancelFn,
		onClose:       onClose,
	}
	h.init(ctx)
	return h, nil
}

func (h *ChatHandlerOBSOLETE) init(ctx context.Context) {
	h.messagesOutChan = make(chan streamcontrol.Event, 100)
	observability.Go(ctx, func(ctx context.Context) {
		if h.onClose != nil {
			defer h.onClose(ctx, h)
		}
		defer close(h.messagesOutChan)
		h.loop(ctx)
	})
}

func (h *ChatHandlerOBSOLETE) loop(ctx context.Context) {
	err := h.iterate(ctx)
	if err != nil {
		logger.Errorf(ctx, "unable to perform an iteration: %v", err)
		return
	}

	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		err := h.iterate(ctx)
		if err != nil {
			logger.Errorf(ctx, "unable to perform an iteration: %v", err)
			return
		}
	}
}

func (h *ChatHandlerOBSOLETE) iterate(ctx context.Context) error {
	ctx, cancelFn := context.WithTimeout(ctx, 5*time.Second)
	defer cancelFn()
	startTS := time.Now()
	reply, err := h.client.GetChatMessagesV2(ctx, uint64(h.channelID), 0)
	if err != nil {
		return fmt.Errorf("unable to get the chat messages of channel with ID %d: %w", h.channelID, err)
	}
	rtt := time.Since(startTS)
	logger.Tracef(ctx, "round trip time == %v (messages count: %d)", rtt, len(reply.Data.Messages))

	// TODO: use the cursor instead of message ID to avoid duplicates
	if reply.Data.Cursor != "" {
		cursor, err := strconv.ParseUint(reply.Data.Cursor, 10, 64)
		if err != nil {
			return fmt.Errorf("unable to parse the cursor value '%s': %w", reply.Data.Cursor, err)
		}
		h.currentCursor = cursor
	} else {
		h.currentCursor = 0
	}

	// assuming reply.Data is already sorted by CreatedAt DESC

	slices.Reverse(reply.Data.Messages)

	// skipping everything we already notified about
	var firstNewIdx int
	for idx, msg := range reply.Data.Messages {
		if msg.ID == h.lastMessageID {
			firstNewIdx = idx + 1
			break
		}
	}

	for _, msg := range reply.Data.Messages[firstNewIdx:] {
		h.sendMessage(msg)
	}
	return nil
}

func (h *ChatHandlerOBSOLETE) sendMessage(
	msg kickcom.ChatMessageV2,
) {
	h.lastMessageID = msg.ID
	select {
	case h.messagesOutChan <- streamcontrol.Event{
		CreatedAt: msg.CreatedAt,
		Type:      streamcontrol.EventTypeChatMessage,
		User: streamcontrol.User{
			ID:   streamcontrol.UserID(fmt.Sprintf("%d", msg.UserID)),
			Slug: msg.Sender.Slug,
			Name: msg.Sender.Username,
		},
		ID: streamcontrol.EventID(msg.ID),
		Message: &streamcontrol.Message{
			Content: msg.Content,
			Format:  streamcontrol.TextFormatTypePlain,
		},
	}:
	default:
	}
}
func (h *ChatHandlerOBSOLETE) MessagesChan() <-chan streamcontrol.Event {
	return h.messagesOutChan
}

func (h *ChatHandlerOBSOLETE) Close(ctx context.Context) error {
	h.cancelFunc()
	if h.client != nil {
		logger.Errorf(ctx, "Close is not implemented in the reversed engineered client, yet")
	}
	return nil
}
