package streamcontrol

import (
	"fmt"
	"time"
)

type ChatUserID string
type ChatMessageID string

type ChatMessage struct {
	CreatedAt time.Time
	EventType EventType
	UserID    ChatUserID
	Username  string
	MessageID ChatMessageID
	Message   string
	Paid      Money
}

type EventType int

const (
	EventTypeUndefined = EventType(iota)
	EventTypeChatMessage
	EventTypeCheer
	EventTypeAutoModHold
	EventTypeAdBreak
	EventTypeBan
	EventTypeFollow
	EventTypeRaid
	EventTypeChannelShoutoutReceive
	EventTypeSubscribe
	EventTypeStreamOnline
	EventTypeStreamOffline
	EventTypeOther
)

func (t EventType) String() string {
	switch t {
	case EventTypeUndefined:
		return "undefined"
	case EventTypeChatMessage:
		return "chat_message"
	case EventTypeCheer:
		return "cheer"
	case EventTypeAutoModHold:
		return "automod_hold"
	case EventTypeAdBreak:
		return "ad_break"
	case EventTypeBan:
		return "ban"
	case EventTypeFollow:
		return "follow"
	case EventTypeRaid:
		return "raid"
	case EventTypeChannelShoutoutReceive:
		return "channel_shoutout_receive"
	case EventTypeSubscribe:
		return "subscribe"
	case EventTypeStreamOnline:
		return "stream_online"
	case EventTypeStreamOffline:
		return "stream_offline"
	case EventTypeOther:
		return "other"
	}
	return fmt.Sprintf("unknown_%d", int(t))
}
