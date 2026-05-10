// Package dedup carries shared constants for the chat-event dedup gates so the
// subprocess-side runner gate (pkg/chathandler.Runner.injectEvent) and the
// streamd-side gate (pkg/streamd.StreamD.InjectChatMessage's injectedEvents
// LoadOrStore) cannot drift apart.
package dedup

import "time"

// TTL is the retention window for an event-ID dedup entry. Both the
// subprocess gate and the streamd-side gate use this value so the two
// dedup horizons stay aligned. It is ALSO used as the time-bucket grain
// (Truncate()) for fingerprintEvent and fingerprintEventForCollapse in
// pkg/streamd/event_dedup_key.go — changing this value re-keys every
// in-flight content-fingerprint bucket. Must be longer than any plausible
// overlap during Level 2 (cross-source collapse) transitions.
const TTL = 5 * time.Minute
