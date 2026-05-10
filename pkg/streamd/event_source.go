package streamd

import "github.com/xaionaro-go/streamctl/pkg/streamcontrol"

// eventSource identifies which (platform, listener-type) pair produced an
// event. The same tuple serves two roles in streamd:
//
//  1. Handler-process registry key. Each external chat handler subprocess
//     is uniquely identified by its (platform, listener-type) — that's the
//     producer's identity, and externalChatHandlers maps each source to
//     exactly one running handler.
//
//  2. Cross-source content-collapse dedup tag. The collapse index records
//     which sources have already contributed an emission for a given
//     content fingerprint, so a same-content event from a not-yet-seen
//     source links onto the existing group instead of publishing again.
//
// One type, one canonical (platform, listener-type) tuple — no duplication.
type eventSource struct {
	Platform     streamcontrol.PlatformName
	ListenerType streamcontrol.ChatListenerType
}
