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

type ChatClient interface {
	GetChatMessagesV2(
		ctx context.Context,
		channelID uint64,
		cursor uint64,
	) (*kickcom.ChatMessagesV2Reply, error)
}

type ChatHandler struct {
	currentCursor   uint64
	channelID       uint64
	lastMessageID   string
	client          Client
	cancelFunc      context.CancelFunc
	messagesOutChan chan streamcontrol.ChatMessage
}

func (k *Kick) newChatHandler(
	ctx context.Context,
	channelID uint64,
) (*ChatHandler, error) {
	return NewChatHandler(ctx, k.Client, channelID)
}

func NewChatHandler(
	ctx context.Context,
	chatClient Client,
	channelID uint64,
) (*ChatHandler, error) {

	ctx, cancelFn := context.WithCancel(ctx)
	h := &ChatHandler{
		currentCursor:   0,
		client:          chatClient,
		channelID:       channelID,
		cancelFunc:      cancelFn,
		messagesOutChan: make(chan streamcontrol.ChatMessage, 100),
	}

	observability.Go(ctx, func() {
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

func (h *ChatHandler) iterate(ctx context.Context) error {
	startTS := time.Now()
	reply, err := h.client.GetChatMessagesV2(ctx, h.channelID, 0)
	if err != nil {
		return fmt.Errorf("unable to get the chat messages of channel with ID %d: %w", h.channelID, err)
	}
	rtt := time.Since(startTS)
	logger.Tracef(ctx, "round trip time == %v", rtt)

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

func (h *ChatHandler) sendMessage(
	msg kickcom.ChatMessageV2,
) {
	h.lastMessageID = msg.ID
	select {
	case h.messagesOutChan <- streamcontrol.ChatMessage{
		CreatedAt: msg.CreatedAt,
		UserID:    streamcontrol.ChatUserID(fmt.Sprintf("%d", msg.UserID)),
		Username:  msg.Sender.Slug,
		MessageID: streamcontrol.ChatMessageID(msg.ID),
		Message:   msg.Content,
	}:
	default:
	}
}
func (h *ChatHandler) MessagesChan() <-chan streamcontrol.ChatMessage {
	return h.messagesOutChan
}
