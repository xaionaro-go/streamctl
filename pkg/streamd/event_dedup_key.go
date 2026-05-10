package streamd

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"

	"github.com/facebookincubator/go-belt/tool/logger"

	"github.com/xaionaro-go/streamctl/pkg/chathandler"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
)

// dedupKey identifies an event for the InjectChatMessage dedup gate.
// Platform is always set so two platforms cannot collide on a short ID.
// Source is "id" when Key is the platform-normalized event ID and "fp"
// when Key is a content fingerprint computed because no usable ID was
// available.
type dedupKey struct {
	Platform streamcontrol.PlatformName
	Source   dedupKeySource
	Key      string
}

type dedupKeySource uint8

const (
	dedupKeyFromID dedupKeySource = iota
	dedupKeyFromFingerprint
)

func (s dedupKeySource) String() string {
	switch s {
	case dedupKeyFromID:
		return "id"
	case dedupKeyFromFingerprint:
		return "fp"
	}
	return fmt.Sprintf("unknown_%d", uint8(s))
}

func (k dedupKey) String() string {
	return fmt.Sprintf("%s/%s/%s", k.Platform, k.Source, k.Key)
}

// computeDedupKey produces a dedupKey for the given event. It first asks
// the platform's registered EventIDNormalizer (looked up via
// chathandler.LookupEventIDNormalizer) to canonicalize ev.ID; if that
// returns an empty string (raw ID was empty, or normalizer rejected the
// format), it falls back to a content fingerprint.
//
// ev.ID is never mutated — only the returned dedupKey carries the
// canonical value.
func computeDedupKey(
	ctx context.Context,
	platID streamcontrol.PlatformName,
	ev streamcontrol.Event,
) (_ret dedupKey) {
	logger.Tracef(ctx, "computeDedupKey(%s, %q)", platID, ev.ID)
	defer func() { logger.Tracef(ctx, "/computeDedupKey(%s, %q): %s", platID, ev.ID, _ret) }()

	id := string(ev.ID)
	if n := chathandler.LookupEventIDNormalizer(platID); n != nil {
		id = n.NormalizeEventID(ev.ID)
	}
	if id != "" {
		return dedupKey{Platform: platID, Source: dedupKeyFromID, Key: id}
	}
	return dedupKey{Platform: platID, Source: dedupKeyFromFingerprint, Key: fingerprintEvent(ev)}
}

// fingerprintEventForCollapse hashes the minimum identity-bearing fields
// of an event so that the same logical message reported by two different
// listener-types of the same platform produces equal fingerprints. We
// intentionally hash only fields that listeners are guaranteed to agree
// on: platform, event type, sender's User.ID, the time-bucket, and the
// message body. Optional fields (TargetUser, Paid, Tier, etc.) vary
// across listener implementations and including them risks fingerprint
// mismatch when collapse is most needed.
//
// Caller must ensure ev.Message != nil and ev.Message.Content != "".
func fingerprintEventForCollapse(
	platID streamcontrol.PlatformName,
	ev streamcontrol.Event,
) string {
	h := sha256.New()
	// Platform — keeps cross-platform same-content events distinct.
	h.Write([]byte(platID))
	h.Write([]byte{0})
	// EventType — discriminates kinds (chat, sub, cheer, etc.).
	_ = binary.Write(h, binary.LittleEndian, int64(ev.Type))
	// Sender — User.ID only. Slug/Name differ across listeners.
	h.Write([]byte(ev.User.ID))
	h.Write([]byte{0})
	// Time bucket — 5 min, matches injectedEventIDTTL.
	var tsBucket int64
	if !ev.CreatedAt.IsZero() {
		tsBucket = ev.CreatedAt.Truncate(injectedEventIDTTL).Unix()
	}
	_ = binary.Write(h, binary.LittleEndian, tsBucket)
	// Message body — the actual chat content.
	h.Write([]byte(ev.Message.Content))
	return hex.EncodeToString(h.Sum(nil))
}

// fingerprintEvent hashes the fields that uniquely identify a chat event
// when no usable ID is available.
//
// CreatedAt is bucketed to injectedEventIDTTL (5 minutes). Two listener
// subprocesses for the same platform can stamp the same source event
// with sub-second drift (parseYTTimestamp falls back to time.Now() on
// parse failure — see pkg/chathandler/platform/youtube/event_converter.go),
// and a coarser bucket is needed to absorb that drift across separate
// processes.
//
// Trade-off: any two empty-ID events with identical user, type, and
// content emitted within the same 5-minute bucket collapse onto one
// dedup entry. That window matches the dedup TTL — entries older than
// it are evicted anyway, so the bucket grain cannot create a false
// positive that lasts longer than the gate would have remembered the
// original event.
func fingerprintEvent(ev streamcontrol.Event) string {
	h := sha256.New()
	_ = binary.Write(h, binary.LittleEndian, int64(ev.Type))
	h.Write([]byte(ev.User.ID))
	h.Write([]byte{0})
	h.Write([]byte(ev.User.Slug))
	h.Write([]byte{0})
	h.Write([]byte(ev.User.Name))
	h.Write([]byte{0})
	var tsBucket int64
	if !ev.CreatedAt.IsZero() {
		tsBucket = ev.CreatedAt.Truncate(injectedEventIDTTL).Unix()
	}
	_ = binary.Write(h, binary.LittleEndian, tsBucket)
	if ev.Message != nil {
		h.Write([]byte(ev.Message.Content))
	}
	return hex.EncodeToString(h.Sum(nil))
}
