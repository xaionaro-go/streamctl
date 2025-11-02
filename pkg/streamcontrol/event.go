package streamcontrol

import (
	"fmt"
	"time"
)

type EventID string
type UserID string
type Tier string

type User struct {
	ID   UserID
	Slug string
	Name string
}

type Message struct {
	Content   string
	Format    TextFormatType
	InReplyTo *EventID
}

type Event struct {
	ID            EventID
	CreatedAt     time.Time
	ExpiresAt     *time.Time
	Type          EventType
	User          User
	TargetUser    *User
	TargetChannel *User
	Message       *Message
	Paid          *Money
	Tier          *Tier
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
	EventTypeSubscriptionNew
	EventTypeSubscriptionRenewed
	EventTypeGiftedSubscription
	EventTypeStreamOnline
	EventTypeStreamOffline
	EventTypeStreamInfoUpdate
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
	case EventTypeSubscriptionNew:
		return "subscription_new"
	case EventTypeSubscriptionRenewed:
		return "subscription_renewed"
	case EventTypeStreamOnline:
		return "stream_online"
	case EventTypeStreamOffline:
		return "stream_offline"
	case EventTypeStreamInfoUpdate:
		return "stream_info_update"
	case EventTypeOther:
		return "other"
	}
	return fmt.Sprintf("unknown_%d", int(t))
}
