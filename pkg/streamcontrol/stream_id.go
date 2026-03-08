package streamcontrol

type StreamID string

// TODO: delete me; this exists only as an intermediate step between
// single-stream (per account) and multi-stream (per account) versions of streamd
const DefaultStreamID = StreamID("default")
