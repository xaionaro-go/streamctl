package streamcontrol

type Capability uint

const (
	CapabilityUndefined = Capability(iota)
	CapabilitySendChatMessage
	CapabilityDeleteChatMessage
	CapabilityBanUser
	EndOfCapability
)
