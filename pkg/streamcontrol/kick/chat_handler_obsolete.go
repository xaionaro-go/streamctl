package kick

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
	messagesOutChan chan streamcontrol.ChatMessage
	onClose         func(context.Context, *ChatHandlerOBSOLETE)
}

func (k *Kick) newChatHandlerOBSOLETE(
	ctx context.Context,
	channelSlug string,
	onClose func(context.Context, *ChatHandlerOBSOLETE),
) (*ChatHandlerOBSOLETE, error) {
	reverseEngClient, err := kickcom.New()
	if err != nil {
		return nil, fmt.Errorf("unable to initialize a client to Kick: %w", err)
	}
	resp, err := reverseEngClient.GetChannelV1(ctx, channelSlug)
	if err != nil {
		return nil, fmt.Errorf("unable to get channel '%s' info: %w", channelSlug, err)
	}
	return NewChatHandlerOBSOLETE(ctx, reverseEngClient, resp.ID, onClose)
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
		currentCursor:   0,
		client:          chatClient,
		channelID:       channelID,
		cancelFunc:      cancelFn,
		messagesOutChan: make(chan streamcontrol.ChatMessage, 100),
		onClose:         onClose,
	}

	observability.Go(ctx, func(ctx context.Context) {
		if onClose != nil {
			defer func() {
				onClose(ctx, h)
			}()
		}
		defer func() {
			close(h.messagesOutChan)
		}()
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
	})

	return h, nil
}

func (h *ChatHandlerOBSOLETE) iterate(ctx context.Context) error {
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
	case h.messagesOutChan <- streamcontrol.ChatMessage{
		CreatedAt: msg.CreatedAt,
		EventType: streamcontrol.EventTypeChatMessage,
		UserID:    streamcontrol.ChatUserID(fmt.Sprintf("%d", msg.UserID)),
		Username:  msg.Sender.Slug,
		MessageID: streamcontrol.ChatMessageID(msg.ID),
		Message:   msg.Content,
	}:
	default:
	}
}
func (h *ChatHandlerOBSOLETE) MessagesChan() <-chan streamcontrol.ChatMessage {
	return h.messagesOutChan
}
