package streamd

import (
	"sync"
	"time"
)

// sourceGroup tracks one logical chat event observed under a content
// fingerprint, plus the set of sources (platform+listener-type tuples)
// that have already contributed an emission for it. The DedupKey is the
// canonical key of the FIRST emission of the group; subsequent
// cross-source arrivals link onto it via Layer 1's back-reference write.
type sourceGroup struct {
	DedupKey   dedupKey
	Sources    map[eventSource]struct{}
	InsertedAt time.Time
}

// fpEntry is the value stored in contentFingerprintIndex. It holds an
// ordered list of source-groups for a single fingerprint. Groups are
// kept in FIFO order so an incoming emission links into the OLDEST
// group whose source-set does not yet contain the incoming source —
// matching the user-observed semantics where "first 'lol' from A then
// from B" links into one group, "second 'lol' from A then from B"
// links into the next.
type fpEntry struct {
	mu     sync.Mutex
	groups []*sourceGroup
}
