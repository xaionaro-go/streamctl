package youtube

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
)

const (
	// Real-run wrapper bytes for the same logical YouTube chat message
	// emitted by two different PACE listeners (see also the streamd-side
	// dedup-key tests). Both decode to the inner ID below.
	ytIDPrimary     = "EhwKGkNMdnAyS191a0pRREZRZmVQd1FkN0tNbGVR"
	ytIDContingency = "ChwKGkNMdnAyS191a0pRREZRZmVQd1FkN0tNbGVR"
	ytInnerID       = "CLvp2K_ukJQDFQfePwQd7KMleQ"
)

func TestExtractInnerYoutubeID(t *testing.T) {
	a, okA := extractInnerYoutubeID(ytIDPrimary)
	b, okB := extractInnerYoutubeID(ytIDContingency)
	assert.True(t, okA)
	assert.True(t, okB)
	assert.Equal(t, a, b)
	assert.Equal(t, ytInnerID, a)
}

func TestExtractInnerYoutubeID_GarbageReturnsFalse(t *testing.T) {
	// Input that base64-decodes but parses as protobuf with an
	// unexpected wire type must be rejected, not silently returned.
	_, ok := extractInnerYoutubeID("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")
	assert.False(t, ok)

	// Input that does not base64-decode at all must also be rejected.
	_, ok = extractInnerYoutubeID("not_a_real_yt_id!!!")
	assert.False(t, ok)
}

func TestYoutubeNormalize_GarbageReturnsIdentity(t *testing.T) {
	n := youtubeEventIDNormalizer{}
	// A non-decodable string falls back to identity so distinct
	// unknown-format IDs still produce distinct keys at the caller.
	assert.Equal(t, "not_a_real_yt_id", n.NormalizeEventID("not_a_real_yt_id"))
}

func TestYoutubeNormalize_EmptyReturnsEmpty(t *testing.T) {
	n := youtubeEventIDNormalizer{}
	assert.Empty(t, n.NormalizeEventID(""))
}

func TestYoutubeNormalize_PrimaryAndContingencyMatch(t *testing.T) {
	n := youtubeEventIDNormalizer{}
	a := n.NormalizeEventID(streamcontrol.EventID("LCC." + ytIDPrimary))
	b := n.NormalizeEventID(streamcontrol.EventID(ytIDContingency))
	assert.Equal(t, ytInnerID, a)
	assert.Equal(t, ytInnerID, b)
}

func TestYoutubeNormalize_DeletePrefixPreserved(t *testing.T) {
	n := youtubeEventIDNormalizer{}
	got := n.NormalizeEventID(streamcontrol.EventID("delete-LCC." + ytIDPrimary))
	assert.Equal(t, "delete-"+ytInnerID, got)
}

func TestYoutubeNormalize_DeletePrefixDistinctFromOriginal(t *testing.T) {
	n := youtubeEventIDNormalizer{}
	original := n.NormalizeEventID(streamcontrol.EventID("LCC." + ytIDPrimary))
	deleted := n.NormalizeEventID(streamcontrol.EventID("delete-LCC." + ytIDPrimary))
	assert.NotEqual(t, original, deleted)
}

func TestYoutubeNormalize_OnlyChannelPrefixReturnsIdentity(t *testing.T) {
	n := youtubeEventIDNormalizer{}
	// Pathological input: just "LCC." with nothing after. The
	// normalizer must not panic and must return identity so distinct
	// inputs stay distinct.
	assert.Equal(t, "LCC.", n.NormalizeEventID("LCC."))
}
