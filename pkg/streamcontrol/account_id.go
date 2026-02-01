package streamcontrol

// AccountID represents a unique identifier for a user account on a streaming platform.
//
// The value can never be empty.
type AccountID string

// TODO: delete me; this exists only as an intermediate step between
// single-account and multi-account versions of streamd
const DefaultAccountID = AccountID("default")
