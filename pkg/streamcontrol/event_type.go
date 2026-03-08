package streamcontrol

import (
	"fmt"
)

type EventType int

const (
	UndefinedEventType = EventType(iota)
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
	endOfEventType
)

func (t EventType) String() string {
	switch t {
	case UndefinedEventType:
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
	case EventTypeGiftedSubscription:
		return "gifted_subscription"
	case EventTypeStreamOnline:
		return "stream_online"
	case EventTypeStreamOffline:
		return "stream_offline"
	case EventTypeStreamInfoUpdate:
		return "stream_info_update"
	case EventTypeOther:
		return "other"
	default:
		return fmt.Sprintf("unknown_%d", int(t))
	}
}

func ParseEventType(s string) EventType {
	for c := UndefinedEventType; c < endOfEventType; c++ {
		if c.String() == s {
			return c
		}
	}
	return UndefinedEventType
}
