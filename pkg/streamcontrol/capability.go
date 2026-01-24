package streamcontrol

import "fmt"

type Capability uint

const (
	UndefinedCapability = Capability(iota)
	CapabilitySendChatMessage
	CapabilityDeleteChatMessage
	CapabilityBanUser
	CapabilityShoutout
	CapabilityIsChannelStreaming
	CapabilityRaid
	EndOfCapability
)

func (c Capability) String() string {
	switch c {
	case UndefinedCapability:
		return "undefined"
	case CapabilitySendChatMessage:
		return "send_chat_message"
	case CapabilityDeleteChatMessage:
		return "delete_chat_message"
	case CapabilityBanUser:
		return "ban_user"
	case CapabilityShoutout:
		return "shoutout"
	case CapabilityIsChannelStreaming:
		return "is_channel_streaming"
	case CapabilityRaid:
		return "raid"
	default:
		return fmt.Sprintf("unknown_%d", int(c))
	}
}

func ParseCapability(s string) Capability {
	for c := UndefinedCapability; c < EndOfCapability; c++ {
		if c.String() == s {
			return c
		}
	}
	return UndefinedCapability
}
