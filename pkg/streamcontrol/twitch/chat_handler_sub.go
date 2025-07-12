package twitch

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/facebookincubator/go-belt/tool/logger"
	twitcheventsub "github.com/joeyak/go-twitch-eventsub/v3"
	"github.com/nicklaw5/helix/v2"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
)

type ChatHandlerSub struct {
	client          TwitchSubscriptionClient
	wsConn          *websocket.Conn
	broadcasterID   string
	cancelFunc      context.CancelFunc
	waitGroup       sync.WaitGroup
	messagesOutChan chan streamcontrol.ChatMessage
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

	_, sessMsgBytes, err := c.Read(ctx)
	if err != nil {
		return nil, fmt.Errorf("unable to get my session ID: %w", err)
	}
	var sessMsg SessionWelcomeMessage
	err = json.Unmarshal(sessMsgBytes, &sessMsg)
	if err != nil {
		return nil, fmt.Errorf("unable to deserialize the session ID message '%s': %w", sessMsgBytes, err)
	}
	sessID := sessMsg.Payload.Session.ID
	logger.Debugf(ctx, "session ID: '%s' (%s)", sessID, sessMsgBytes)

	params := &helix.EventSubSubscription{
		Type:    string(twitcheventsub.SubChannelChatMessage),
		Version: "1",
		Condition: helix.EventSubCondition{
			BroadcasterUserID: broadcasterID,
			UserID:            myUserID,
		},
		Transport: helix.EventSubTransport{
			Method:    "websocket",
			SessionID: sessID,
		},
	}
	resp, err := client.CreateEventSubSubscription(params)
	if err != nil {
		return nil, fmt.Errorf("unable to create a subscription (%#+v): %w", params, err)
	}
	if resp.ErrorMessage != "" {
		return nil, fmt.Errorf("got an error during subscription (%#+v): %s", params, resp.ErrorMessage)
	}

	h.waitGroup.Add(1)
	observability.Go(ctx, func(ctx context.Context) {
		defer logger.Debugf(ctx, "NewChatHandlerSub: closed")
		if onClose != nil {
			defer onClose(ctx)
		}
		defer h.waitGroup.Done()
		defer func() {
			close(h.messagesOutChan)
		}()
		for {
			select {
			case <-ctx.Done():
				logger.Debugf(ctx, "NewChatHandlerSub: context closed: %v", ctx.Err())
				return
			default:
			}
			_, messageSerialized, err := c.Read(ctx)
			if err != nil {
				logger.Errorf(ctx, "unable to read the message: %v", err)
				return
			}
			var header struct {
				Metadata MessageMetadata `json:"metadata"`
			}
			if err := json.Unmarshal(messageSerialized, &header); err != nil {
				logger.Errorf(ctx, "unable to un-JSON-ize message '%s': %v", messageSerialized, err)
				return
			}

			t, err := time.Parse(time.RFC3339Nano, header.Metadata.MessageTimestamp)
			if err != nil {
				logger.Errorf(ctx, "unable to parse timestamp '%s': %v", header.Metadata.MessageTimestamp, err)
				t = time.Now()
			}

			var msgAbstract any
			switch header.Metadata.MessageType {
			case "session_welcome":
				msgAbstract = &SessionWelcomeMessage{}
			case "notification":
				var msg NotificationMessage
				err := json.Unmarshal(messageSerialized, &msg)
				if err != nil {
					logger.Errorf(ctx, "unable to unserialize the notification message '%s': %v", messageSerialized, err)
					continue
				}
				logger.Tracef(ctx, "notification: %#+v", msg)
				switch twitcheventsub.EventSubscription(msg.Payload.Subscription.Type) {
				case twitcheventsub.SubChannelChatMessage:
					var chatEvent ChatMessageEvent
					err := json.Unmarshal(msg.Payload.Event, &chatEvent)
					if err != nil {
						logger.Errorf(ctx, "unable to unserialize the chat message '%s': %v", msg.Payload.Event, err)
						continue
					}
					logger.Tracef(ctx, "chat message: %#+v", msg)
					msg := streamcontrol.ChatMessage{
						CreatedAt: t,
						UserID:    streamcontrol.ChatUserID(chatEvent.ChatterUserID),
						Username:  chatEvent.ChatterUserName,
						MessageID: streamcontrol.ChatMessageID(chatEvent.MessageID),
						Message:   chatEvent.Message.Text,
					}
					logger.Tracef(ctx, "resulting chat: %#+v", msg)
					select {
					case h.messagesOutChan <- msg:
					default:
						logger.Errorf(ctx, "the queue is full, have to drop %#+v", msg)
					}
				default:
					logger.Warnf(ctx, "got an event on a channel I haven't subscribed to: '%s': %s", msg.Payload.Subscription.Type, messageSerialized)
				}
				continue
			case "session_keepalive":
				msgAbstract = &KeepaliveMessage{}
			case "reconnect":
				msgAbstract = &ReconnectMessage{}
			case "revocation":
				msgAbstract = &RevocationMessage{}
			default:
				logger.Debugf(ctx, "unknown message type: '%s': '%s'", header.Metadata.MessageType, messageSerialized)
				continue
			}
			err = json.Unmarshal(messageSerialized, &msgAbstract)
			if err != nil {
				logger.Errorf(ctx, "unable to unserialize the %T message '%s': %v", msgAbstract, messageSerialized, err)
				continue
			}
			logger.Tracef(ctx, "received %T: %#+v", msgAbstract, msgAbstract)
		}
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

// Common metadata for all messages
type MessageMetadata struct {
	MessageID        string `json:"message_id"`
	MessageType      string `json:"message_type"`
	MessageTimestamp string `json:"message_timestamp"`
}

// Session (used in session_welcome and reconnect)
type Session struct {
	ID                      string  `json:"id"`
	Status                  string  `json:"status"`
	KeepaliveTimeoutSeconds int     `json:"keepalive_timeout_seconds"`
	ReconnectURL            *string `json:"reconnect_url"` // can be null
	ConnectedAt             string  `json:"connected_at"`
}

// Payload for session_welcome
type WelcomePayload struct {
	Session Session `json:"session"`
}

// Session Welcome message
type SessionWelcomeMessage struct {
	Metadata MessageMetadata `json:"metadata"`
	Payload  WelcomePayload  `json:"payload"`
}

// Subscription info (used in notifications)
type Subscription struct {
	ID        string            `json:"id"`
	Type      string            `json:"type"`
	Version   string            `json:"version"`
	Status    string            `json:"status"`
	Cost      int               `json:"cost"`
	Condition map[string]string `json:"condition"`
	CreatedAt string            `json:"created_at"`
	Transport struct {
		Method    string `json:"method"`
		SessionID string `json:"session_id"`
	} `json:"transport"`
}

// Example: Channel Chat Message Event (payload.event for chat messages)
type ChatMessageEvent struct {
	BroadcasterUserID    string `json:"broadcaster_user_id"`
	BroadcasterUserLogin string `json:"broadcaster_user_login"`
	BroadcasterUserName  string `json:"broadcaster_user_name"`
	ChatterUserID        string `json:"chatter_user_id"`
	ChatterUserLogin     string `json:"chatter_user_login"`
	ChatterUserName      string `json:"chatter_user_name"`
	MessageID            string `json:"message_id"`
	Message              struct {
		Text string `json:"text"`
	} `json:"message"`
	Color  string `json:"color"`
	Badges []struct {
		SetID string `json:"set_id"`
		ID    string `json:"id"`
		Info  string `json:"info"`
	} `json:"badges"`
}

type NotificationPayload struct {
	Subscription Subscription    `json:"subscription"`
	Event        json.RawMessage `json:"event"`
}

type NotificationMessage struct {
	Metadata MessageMetadata     `json:"metadata"`
	Payload  NotificationPayload `json:"payload"`
}

type KeepaliveMessage struct {
	Metadata MessageMetadata `json:"metadata"`
}

type ReconnectPayload struct {
	Session Session `json:"session"`
}

type ReconnectMessage struct {
	Metadata MessageMetadata  `json:"metadata"`
	Payload  ReconnectPayload `json:"payload"`
}

type RevocationPayload struct {
	Subscription Subscription `json:"subscription"`
}

type RevocationMessage struct {
	Metadata MessageMetadata   `json:"metadata"`
	Payload  RevocationPayload `json:"payload"`
}
