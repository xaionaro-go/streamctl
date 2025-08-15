package twitch

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/joeyak/go-twitch-eventsub/v3"
	twitcheventsub "github.com/joeyak/go-twitch-eventsub/v3"
	"github.com/nicklaw5/helix/v2"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/xsync"
)

type ChatHandlerSub struct {
	client        TwitchSubscriptionClient
	wsConn        *websocket.Conn
	broadcasterID string
	cancelFunc    context.CancelFunc
	waitGroup     sync.WaitGroup

	messagesOutChan       chan streamcontrol.ChatMessage
	messagesOutChanLocker xsync.Mutex
}

var _ ChatHandler = (*ChatHandlerSub)(nil)

type TwitchSubscriptionClient interface {
	CreateEventSubSubscription(payload *helix.EventSubSubscription) (*helix.EventSubSubscriptionsResponse, error)
	GetUsers(params *helix.UsersParams) (*helix.UsersResponse, error)
}

func NewChatHandlerSub(
	ctx context.Context,
	client TwitchSubscriptionClient,
	broadcasterID string,
	onClose func(context.Context),
) (_ret *ChatHandlerSub, _err error) {
	logger.Debugf(ctx, "NewChatHandlerSub")
	defer func() { logger.Debugf(ctx, "/NewChatHandlerSub: %v", _err) }()

	const urlString = "wss://eventsub.wss.twitch.tv/ws"
	c, _, err := websocket.Dial(ctx, urlString, nil)
	if err != nil {
		return nil, fmt.Errorf("unable to initiate a websocket connection to '%s': %w", urlString, err)
	}

	var myUserID string
	{
		resp, err := client.GetUsers(&helix.UsersParams{})
		if err != nil {
			return nil, fmt.Errorf("unable to get my user info: %w", err)
		}
		if len(resp.Data.Users) != 1 {
			return nil, fmt.Errorf("expected to get one user info, but received %d", len(resp.Data.Users))
		}
		myUserID = resp.Data.Users[0].ID
		logger.Debugf(ctx, "my user ID: %v", myUserID)
	}

	ctx, cancelFn := context.WithCancel(ctx)
	h := &ChatHandlerSub{
		client:          client,
		wsConn:          c,
		broadcasterID:   broadcasterID,
		cancelFunc:      cancelFn,
		messagesOutChan: make(chan streamcontrol.ChatMessage, 100),
	}
	defer func() {
		if _err != nil {
			_ = h.Close(ctx)
		}
	}()

	eventSubClient := twitcheventsub.NewClientWithUrl(urlString)

	var errCallback func(ctx context.Context, err error)
	errCallback = func(ctx context.Context, err error) {
		logger.Errorf(ctx, "unable to read from the socket: %v", err)
		go func() {
			for {
				err = eventSubClient.ConnectWithContext(ctx, errCallback)
				if err == nil {
					break
				}
				time.Sleep(time.Second)
				logger.Errorf(ctx, "unable to connect to '%s': %v", urlString, err)
			}
		}()
	}
	eventSubClient.OnEventAutomodMessageHold(func(event twitcheventsub.EventAutomodMessageHold, msg twitcheventsub.NotificationMessage) {
		h.sendMessage(ctx, streamcontrol.ChatMessage{
			CreatedAt:  msg.Metadata.MessageTimestamp,
			EventType:  streamcontrol.EventTypeAutoModHold,
			UserID:     streamcontrol.ChatUserID(event.UserID),
			Username:   event.UserName,
			MessageID:  streamcontrol.ChatMessageID(msg.Metadata.MessageID),
			Message:    event.Message.Text,
			FormatType: streamcontrol.TextFormatTypePlain,
		})
	})
	eventSubClient.OnEventChannelAdBreakBegin(func(event twitcheventsub.EventChannelAdBreakBegin, msg twitcheventsub.NotificationMessage) {
		h.sendMessage(ctx, streamcontrol.ChatMessage{
			CreatedAt:  msg.Metadata.MessageTimestamp,
			EventType:  streamcontrol.EventTypeAdBreak,
			UserID:     "twitch",
			Username:   "Twitch",
			MessageID:  streamcontrol.ChatMessageID(msg.Metadata.MessageID),
			Message:    fmt.Sprintf("%d seconds", event.DurationSeconds),
			FormatType: streamcontrol.TextFormatTypePlain,
		})
	})
	eventSubClient.OnEventChannelBan(func(event twitcheventsub.EventChannelBan, msg twitcheventsub.NotificationMessage) {
		h.sendMessage(ctx, streamcontrol.ChatMessage{
			CreatedAt:  msg.Metadata.MessageTimestamp,
			EventType:  streamcontrol.EventTypeBan,
			UserID:     streamcontrol.ChatUserID(event.UserID),
			Username:   event.UserName,
			MessageID:  streamcontrol.ChatMessageID(msg.Metadata.MessageID),
			Message:    event.Reason,
			FormatType: streamcontrol.TextFormatTypePlain,
		})
	})
	eventSubClient.OnEventChannelCheer(func(event twitcheventsub.EventChannelCheer, msg twitcheventsub.NotificationMessage) {
		h.sendMessage(ctx, streamcontrol.ChatMessage{
			CreatedAt: msg.Metadata.MessageTimestamp,
			EventType: streamcontrol.EventTypeCheer,
			UserID:    streamcontrol.ChatUserID(event.UserID),
			Username:  event.UserName,
			MessageID: streamcontrol.ChatMessageID(msg.Metadata.MessageID),
			Message:   event.Message,
			Paid: streamcontrol.Money{
				Currency: streamcontrol.CurrencyBits,
				Amount:   float64(event.Bits),
			},
			FormatType: streamcontrol.TextFormatTypePlain,
		})
	})
	eventSubClient.OnEventChannelFollow(func(event twitcheventsub.EventChannelFollow, msg twitcheventsub.NotificationMessage) {
		h.sendMessage(ctx, streamcontrol.ChatMessage{
			CreatedAt:  msg.Metadata.MessageTimestamp,
			EventType:  streamcontrol.EventTypeFollow,
			UserID:     streamcontrol.ChatUserID(event.UserID),
			Username:   event.UserName,
			MessageID:  streamcontrol.ChatMessageID(msg.Metadata.MessageID),
			Message:    "",
			FormatType: streamcontrol.TextFormatTypePlain,
		})
	})
	eventSubClient.OnEventChannelRaid(func(event twitcheventsub.EventChannelRaid, msg twitcheventsub.NotificationMessage) {
		h.sendMessage(ctx, streamcontrol.ChatMessage{
			CreatedAt:  msg.Metadata.MessageTimestamp,
			EventType:  streamcontrol.EventTypeRaid,
			UserID:     streamcontrol.ChatUserID(event.FromBroadcasterUserId),
			Username:   event.FromBroadcasterUserName,
			MessageID:  streamcontrol.ChatMessageID(msg.Metadata.MessageID),
			Message:    fmt.Sprintf("%d viewers", event.Viewers),
			FormatType: streamcontrol.TextFormatTypePlain,
		})
	})
	eventSubClient.OnEventChannelShoutoutReceive(func(event twitcheventsub.EventChannelShoutoutReceive, msg twitcheventsub.NotificationMessage) {
		h.sendMessage(ctx, streamcontrol.ChatMessage{
			CreatedAt:  msg.Metadata.MessageTimestamp,
			EventType:  streamcontrol.EventTypeChannelShoutoutReceive,
			UserID:     streamcontrol.ChatUserID(event.FromBroadcasterUserId),
			Username:   event.FromBroadcasterUserName,
			MessageID:  streamcontrol.ChatMessageID(msg.Metadata.MessageID),
			Message:    fmt.Sprintf("%d viewers", event.ViewerCount),
			FormatType: streamcontrol.TextFormatTypePlain,
		})
	})
	eventSubClient.OnEventChannelSubscribe(func(event twitcheventsub.EventChannelSubscribe, msg twitcheventsub.NotificationMessage) {
		var description []string
		switch {
		case event.IsGift:
			description = append(description, "gift:")
		}
		description = append(description, fmt.Sprintf("tier '%s'", event.Tier))
		h.sendMessage(ctx, streamcontrol.ChatMessage{
			CreatedAt:  msg.Metadata.MessageTimestamp,
			EventType:  streamcontrol.EventTypeSubscribe,
			UserID:     streamcontrol.ChatUserID(event.UserID),
			Username:   event.UserName,
			MessageID:  streamcontrol.ChatMessageID(msg.Metadata.MessageID),
			Message:    strings.Join(description, " "),
			FormatType: streamcontrol.TextFormatTypePlain,
		})
	})
	eventSubClient.OnEventChannelSubscriptionMessage(func(event twitcheventsub.EventChannelSubscriptionMessage, msg twitcheventsub.NotificationMessage) {
		h.sendMessage(ctx, streamcontrol.ChatMessage{
			CreatedAt: msg.Metadata.MessageTimestamp,
			EventType: streamcontrol.EventTypeSubscribe,
			UserID:    streamcontrol.ChatUserID(event.UserID),
			Username:  event.UserName,
			MessageID: streamcontrol.ChatMessageID(msg.Metadata.MessageID),
			Message: fmt.Sprintf(
				"%d months (%d in total), tier '%s', message: %s",
				event.DurationMonths, event.CumulativeMonths, event.Tier, event.Message.Text,
			),
			FormatType: streamcontrol.TextFormatTypePlain,
		})
	})
	eventSubClient.OnEventChannelSubscriptionGift(func(event twitcheventsub.EventChannelSubscriptionGift, msg twitcheventsub.NotificationMessage) {
		h.sendMessage(ctx, streamcontrol.ChatMessage{
			CreatedAt: msg.Metadata.MessageTimestamp,
			EventType: streamcontrol.EventTypeSubscribe,
			UserID:    streamcontrol.ChatUserID(event.UserID),
			Username:  event.UserName,
			MessageID: streamcontrol.ChatMessageID(msg.Metadata.MessageID),
			Message: fmt.Sprintf(
				"gift: %d subs, tier '%s'",
				event.Total, event.Tier,
			),
			FormatType: streamcontrol.TextFormatTypePlain,
		})
	})
	eventSubClient.OnEventStreamOnline(func(event twitcheventsub.EventStreamOnline, msg twitcheventsub.NotificationMessage) {
		h.sendMessage(ctx, streamcontrol.ChatMessage{
			CreatedAt:  msg.Metadata.MessageTimestamp,
			EventType:  streamcontrol.EventTypeStreamOnline,
			UserID:     streamcontrol.ChatUserID(event.BroadcasterUserId),
			Username:   event.BroadcasterUserName,
			MessageID:  streamcontrol.ChatMessageID(msg.Metadata.MessageID),
			Message:    event.StartedAt.Format(time.DateTime),
			FormatType: streamcontrol.TextFormatTypePlain,
		})
	})
	eventSubClient.OnEventStreamOffline(func(event twitcheventsub.EventStreamOffline, msg twitcheventsub.NotificationMessage) {
		h.sendMessage(ctx, streamcontrol.ChatMessage{
			CreatedAt:  msg.Metadata.MessageTimestamp,
			EventType:  streamcontrol.EventTypeStreamOffline,
			UserID:     streamcontrol.ChatUserID(event.BroadcasterUserId),
			Username:   event.BroadcasterUserName,
			MessageID:  streamcontrol.ChatMessageID(msg.Metadata.MessageID),
			Message:    "",
			FormatType: streamcontrol.TextFormatTypePlain,
		})
	})
	eventSubClient.OnEventChannelChatMessage(func(chatEvent twitcheventsub.EventChannelChatMessage, msg twitcheventsub.NotificationMessage) {
		logger.Tracef(ctx, "chat message: %#+v", chatEvent)
		eventType := streamcontrol.EventTypeChatMessage
		if chatEvent.Cheer != nil {
			eventType = streamcontrol.EventTypeCheer
		}
		h.sendMessage(ctx, streamcontrol.ChatMessage{
			CreatedAt:  msg.Metadata.MessageTimestamp,
			EventType:  eventType,
			UserID:     streamcontrol.ChatUserID(chatEvent.ChatterUserId),
			Username:   chatEvent.ChatterUserName,
			MessageID:  streamcontrol.ChatMessageID(chatEvent.MessageId),
			Message:    chatEvent.Message.Text,
			FormatType: streamcontrol.TextFormatTypePlain,
		})
	})

	eventSubClient.OnWelcome(func(sessMsg twitcheventsub.WelcomeMessage) {
		sessID := sessMsg.Payload.Session.ID
		logger.Debugf(ctx, "session ID: '%s'", sessID)

		eventMap := twitcheventsub.SubMetadata()

		params := &helix.EventSubSubscription{
			Type:    string(twitcheventsub.SubChannelChatMessage),
			Version: eventMap[twitcheventsub.SubChannelChatMessage].Version,
			Condition: helix.EventSubCondition{
				BroadcasterUserID: broadcasterID,
				ModeratorUserID:   broadcasterID,
				UserID:            myUserID,
			},
			Transport: helix.EventSubTransport{
				Method:    "websocket",
				SessionID: sessID,
			},
		}
		resp, err := client.CreateEventSubSubscription(params)
		if err != nil {
			logger.Errorf(ctx, "unable to create a subscription (%#+v): %v", params, err)
			return
		}
		if resp.ErrorMessage != "" {
			logger.Errorf(ctx, "got an error during subscription (%#+v): %s", params, resp.ErrorMessage)
			return
		}

		go func() {
			for chanName, metadata := range eventMap {
				params := &helix.EventSubSubscription{
					Type:    string(chanName),
					Version: metadata.Version,
					Condition: helix.EventSubCondition{
						BroadcasterUserID: broadcasterID,
						ModeratorUserID:   broadcasterID,
						UserID:            myUserID,
					},
					Transport: helix.EventSubTransport{
						Method:    "websocket",
						SessionID: sessID,
					},
				}
				switch chanName {
				case twitcheventsub.SubChannelRaid:
					params.Condition.ToBroadcasterUserID = broadcasterID
				case twitcheventsub.SubChannelChatMessage:
					continue
				case twitcheventsub.SubConduitShardDisabled,
					twitch.SubExtensionBitsTransactionCreate,
					twitch.SubDropEntitlementGrant,
					twitch.SubUserAuthorizationRevoke,
					twitch.SubUserAuthorizationGrant:
					continue

				}
				resp, err := client.CreateEventSubSubscription(params)
				if err != nil {
					logger.Errorf(ctx, "unable to create a subscription (%#+v): %w", params, err)
				}
				if resp.ErrorMessage != "" {
					logger.Warnf(ctx, "unable to subscribe to '%s': %v", chanName, err)
					continue
				}
				logger.Debugf(ctx, "successfully subscribed to '%s'", chanName)
			}
		}()
	})

	err = eventSubClient.ConnectWithContext(ctx, errCallback)
	if err != nil {
		return nil, fmt.Errorf("unable to connect to '%s': %w", urlString, err)
	}

	h.waitGroup.Add(1)
	observability.Go(ctx, func(ctx context.Context) {
		defer h.waitGroup.Done()
		defer func() {
			h.messagesOutChanLocker.Do(ctx, func() {
				close(h.messagesOutChan)
			})
		}()
		if onClose != nil {
			defer onClose(ctx)
		}
		defer logger.Debugf(ctx, "NewChatHandlerSub: closed")
		eventSubClient.Wait()
	})

	return h, nil
}

func (h *ChatHandlerSub) Close(ctx context.Context) error {
	h.cancelFunc()
	return nil
}

func (h *ChatHandlerSub) MessagesChan() <-chan streamcontrol.ChatMessage {
	return h.messagesOutChan
}

func (h *ChatHandlerSub) sendMessage(
	ctx context.Context,
	chatMsg streamcontrol.ChatMessage,
) {
	h.messagesOutChanLocker.Do(ctx, func() {
		logger.Tracef(ctx, "resulting chat: %#+v", chatMsg)
		select {
		case h.messagesOutChan <- chatMsg:
		default:
			logger.Errorf(ctx, "the queue is full, have to drop %#+v", chatMsg)
		}
	})
}
