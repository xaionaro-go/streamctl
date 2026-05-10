package youtube

import (
	"encoding/base64"
	"strings"

	"google.golang.org/protobuf/encoding/protowire"

	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
)

const (
	// youtubeDeletePrefix marks events emitted for chat-message deletions
	// (see pkg/streamcontrol/youtube/chat_listener.go). The prefix must be
	// preserved through normalization so a deletion event does not collide
	// with the original message.
	youtubeDeletePrefix = "delete-"

	// youtubeChannelPrefix is the channel-binding prefix the gRPC stream
	// listener emits in front of the base64-encoded wrapper. The obsolete
	// scrape listener emits no such prefix.
	youtubeChannelPrefix = "LCC."

	// youtubeInnerWrapperField is the protobuf field number of the inner
	// length-delimited submessage that carries the message ID. Both wrapper
	// shapes ("LCC.Eh..." outer field 2 and "Ch..." outer field 1) place
	// the inner message ID under field 1 within the inner submessage.
	youtubeInnerWrapperField protowire.Number = 1
)

// youtubeEventIDNormalizer collapses the YouTube ID variants observed
// across PACE listener types onto a single canonical inner-ID key:
//
//   - Primary (gRPC stream): "LCC.Eh..." — outer protobuf field tag 2
//     wrapped, with an "LCC." channel-binding prefix.
//   - Alternate (REST poll): "LCC.<base64>" — same wrapper as Primary.
//   - Contingency (innertube scrape): "Ch..." — outer protobuf field tag 1
//     wrapped, no "LCC." prefix.
//
// All three forms decode to a protobuf message whose single
// length-delimited submessage holds (under field tag 1, wire type 2) the
// platform's stable per-message ID (e.g. "CLvp2K_ukJQDFQfePwQd7KMleQ").
// Extracting that inner string makes the three forms hash to the same
// dedup key.
type youtubeEventIDNormalizer struct{}

// NormalizeEventID extracts the canonical inner message ID from id. On
// success the returned string is the inner ID (with any leading
// "delete-" prefix preserved). On any structural mismatch — empty input,
// failed base64 decode, unexpected wire types — it returns id unchanged
// so the dedup gate still distinguishes distinct unknown-format IDs.
//
// The function never mutates id; it only inspects the value.
func (youtubeEventIDNormalizer) NormalizeEventID(
	id streamcontrol.EventID,
) string {
	s := string(id)
	if s == "" {
		return ""
	}

	prefix := ""
	if strings.HasPrefix(s, youtubeDeletePrefix) {
		prefix = youtubeDeletePrefix
		s = s[len(youtubeDeletePrefix):]
	}
	s = strings.TrimPrefix(s, youtubeChannelPrefix)
	if s == "" {
		// Pathological: nothing left after stripping wrappers. Return
		// the caller's identity so distinct inputs stay distinct.
		return string(id)
	}

	inner, ok := extractInnerYoutubeID(s)
	if !ok {
		// Unknown format: fall back to identity so the dedup gate still
		// distinguishes different unknown-format IDs from each other.
		return string(id)
	}
	return prefix + inner
}

// youtubeBase64Decoders lists the base64 alphabets to try, in order. The
// real-run wrapper bytes round-trip cleanly under all four for the IDs
// observed empirically; trying multiple in order tolerates any future
// variant that switches alphabet without breaking the normalizer.
var youtubeBase64Decoders = []*base64.Encoding{
	base64.RawURLEncoding,
	base64.URLEncoding,
	base64.RawStdEncoding,
	base64.StdEncoding,
}

// extractInnerYoutubeID base64-decodes a YouTube outer-wrapper string
// and parses the resulting bytes as a protobuf message of the shape:
//
//	outer { tag=<field 1 or 2>, wireType=2 } -> inner_bytes
//	inner { tag=<youtubeInnerWrapperField>, wireType=2 } -> id_bytes
//
// On a structural match it returns the inner ID as a string and true.
// On any mismatch (decode failure, wrong wire type, leftover bytes) it
// returns ("", false).
func extractInnerYoutubeID(
	s string,
) (string, bool) {
	for _, dec := range youtubeBase64Decoders {
		raw, err := dec.DecodeString(s)
		if err != nil {
			continue
		}
		if id, ok := parseYoutubeIDWrapper(raw); ok {
			return id, true
		}
	}
	return "", false
}

// parseYoutubeIDWrapper decodes the protobuf wrapper bytes documented on
// extractInnerYoutubeID. Returns the inner ID bytes as a string on
// success.
func parseYoutubeIDWrapper(
	b []byte,
) (string, bool) {
	_, outerType, tagLen := protowire.ConsumeTag(b)
	if tagLen < 0 || outerType != protowire.BytesType {
		return "", false
	}
	innerBytes, innerLen := protowire.ConsumeBytes(b[tagLen:])
	if innerLen < 0 {
		return "", false
	}

	innerField, innerType, innerTagLen := protowire.ConsumeTag(innerBytes)
	if innerTagLen < 0 {
		return "", false
	}
	if innerField != youtubeInnerWrapperField || innerType != protowire.BytesType {
		return "", false
	}
	idBytes, idLen := protowire.ConsumeBytes(innerBytes[innerTagLen:])
	if idLen < 0 {
		return "", false
	}
	return string(idBytes), true
}
