package streamcontrol

func Stream[
	ProfileType StreamProfile,
](
	account AccountGeneric[ProfileType],
	streamID StreamID,
) StreamGeneric[ProfileType] {
	if streamID == "" {
		panic("streamID must be non-empty")
	}
	return &streamController[ProfileType]{
		AccountGeneric: account,
		StreamID:       streamID,
	}
}
