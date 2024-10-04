package types

type StreamForward[T any] struct {
	StreamID         StreamID
	DestinationID    DestinationID
	Enabled          bool
	Quirks           ForwardingQuirks
	ActiveForwarding T
	NumBytesWrote    uint64
	NumBytesRead     uint64
}
