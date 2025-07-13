package streamcontrol

import "time"

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
