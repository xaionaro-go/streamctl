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
	CapabilityCreateStream
	CapabilityDeleteStream
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
	case CapabilityCreateStream:
		return "create_stream"
	case CapabilityDeleteStream:
		return "delete_stream"
	default:
		return fmt.Sprintf("unknown_%d", int(c))
	}
}

func ParseCapability(s string) Capability {
	for c := range EndOfCapability {
		if c.String() == s {
			return c
		}
	}
	return UndefinedCapability
}
