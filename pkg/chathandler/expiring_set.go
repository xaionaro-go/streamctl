package chathandler

import (
	"container/list"
	"sync"
	"time"
)

// expiringSet is a bounded set of comparable keys with a TTL.
//
// checkAndAdd reports whether key has been seen within the TTL window. When
// the set is full, the oldest entry is evicted to make room. Eviction by TTL
// runs amortized inside every call.
//
// Used by Runner.injectEvent to drop repeat ev.ID values before the gRPC
// InjectChatMessage call. Events with empty ev.ID bypass this gate; streamd's
// content-fingerprint fallback (event_dedup_key.go) is the backstop.
//
// Defense-in-depth note: the underlying root cause of the duplicate
// chat-event spam is in the YouTube primary listener — see
// pkg/chathandler/platform/youtube/chat_listener_grpc_stream.go:recvBatch
// (around line 209), which does not thread LiveChatMessageListResponse's
// NextPageToken into the next LiveChatMessageListRequest's PageToken. Every
// reconnect or batch pull therefore re-fetches the recent message window.
// This gate stops the resulting subprocess→streamd IPC spam; the proper
// fix lives at the listener and is tracked separately.
type expiringSet[K comparable] struct {
	mu       sync.Mutex
	capacity int
	ttl      time.Duration
	order    *list.List
	index    map[K]*list.Element
}

type expiringSetEntry[K comparable] struct {
	key       K
	expiresAt time.Time
}

func newExpiringSet[K comparable](capacity int, ttl time.Duration) *expiringSet[K] {
	return &expiringSet[K]{
		capacity: capacity,
		ttl:      ttl,
		order:    list.New(),
		index:    make(map[K]*list.Element),
	}
}

// checkAndAdd returns true if key was already in the set (and is still
// within its TTL); false otherwise. In the "false" case, key is added.
//
// When the set reaches capacity, the oldest entry is evicted to make room
// for the new key.
func (s *expiringSet[K]) checkAndAdd(key K) bool {
	now := time.Now()

	s.mu.Lock()
	defer s.mu.Unlock()

	s.evictExpiredLocked(now)

	if _, ok := s.index[key]; ok {
		// TTL is anchored to first insertion; not refreshed on hit.
		// Mirrors streamd's injectedEvents.LoadOrStore semantics — see
		// pkg/streamd/chat.go (injectedEvents LoadOrStore at the top of
		// InjectChatMessage). For YouTube long-poll, this means after
		// `ttl` the same ID can slip through once per cycle by design.
		return true
	}

	if s.order.Len() >= s.capacity {
		oldest := s.order.Front()
		if oldest != nil {
			s.order.Remove(oldest)
			delete(s.index, oldest.Value.(*expiringSetEntry[K]).key)
		}
	}

	entry := &expiringSetEntry[K]{key: key, expiresAt: now.Add(s.ttl)}
	s.index[key] = s.order.PushBack(entry)
	return false
}

func (s *expiringSet[K]) evictExpiredLocked(now time.Time) {
	for {
		front := s.order.Front()
		if front == nil {
			return
		}
		entry := front.Value.(*expiringSetEntry[K])
		if now.Before(entry.expiresAt) {
			return
		}
		s.order.Remove(front)
		delete(s.index, entry.key)
	}
}
