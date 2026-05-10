package chathandler

import (
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
)

// EventIDNormalizer collapses platform-specific event-ID variants onto a
// single canonical key so that the streamd dedup gate sees one logical
// chat message as one entry across multiple PACE listener implementations
// of the same platform.
//
// Returning "" instructs the dedup gate to fall back to a content
// fingerprint — the right behaviour for events whose platform emits no
// usable ID at all.
type EventIDNormalizer interface {
	NormalizeEventID(streamcontrol.EventID) string
}

// EventIDNormalizerProvider is implemented by ChatListenerFactory values
// that supply an EventIDNormalizer for their platform. Implementing this
// interface is optional; platforms whose listener types share an ID
// format do not need to implement it.
type EventIDNormalizerProvider interface {
	EventIDNormalizer() EventIDNormalizer
}

// LookupEventIDNormalizer returns the normalizer for platID, or nil if
// the platform has no factory registered or the factory does not provide
// one.
func LookupEventIDNormalizer(
	platID streamcontrol.PlatformName,
) EventIDNormalizer {
	f := GetChatListenerFactory(platID)
	if f == nil {
		return nil
	}
	p, ok := f.(EventIDNormalizerProvider)
	if !ok {
		return nil
	}
	return p.EventIDNormalizer()
}
