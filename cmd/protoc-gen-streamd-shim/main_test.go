package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// TestProxyOptionFullName guards against accidental rename of the extension
// full name; the constant is the single point of contact between the plugin
// and streamd_options.proto. Drift here would silently produce zero AUTO
// matches at generation time.
func TestProxyOptionFullName(t *testing.T) {
	assert.Equal(t, protoreflect.FullName("streamd.proxy"), proxyOptionFullName)
}

// TestProxyAutoEnumNumber pins the wire value of streamd.ProxyMode.AUTO. The
// .proto file declares `AUTO = 1`; if either side drifts, AUTO RPCs will be
// silently treated as not-AUTO and emit no proxy code.
func TestProxyAutoEnumNumber(t *testing.T) {
	assert.Equal(t, protoreflect.EnumNumber(1), proxyAutoEnumNumber)
}

// TestScanUnknownForAutoProxy exercises the wire-level fallback path used when
// protogen has not registered the (streamd.proxy) extension at the time
// MethodOptions are built. Three cases:
//   - empty buffer → false
//   - tag matches proxy field number with AUTO value → true
//   - tag matches proxy field number with a non-AUTO value → false
//   - foreign tag is skipped (and a following AUTO tag still matches)
func TestScanUnknownForAutoProxy(t *testing.T) {
	encode := func(num protowire.Number, val uint64) []byte {
		buf := protowire.AppendTag(nil, num, protowire.VarintType)
		return protowire.AppendVarint(buf, val)
	}

	assert.False(t, scanUnknownForAutoProxy(nil), "empty buffer must yield false")
	assert.True(t, scanUnknownForAutoProxy(encode(proxyOptionFieldNumber, 1)), "AUTO tag must match")
	assert.False(t, scanUnknownForAutoProxy(encode(proxyOptionFieldNumber, 0)), "non-AUTO value must not match")
	assert.False(t, scanUnknownForAutoProxy(encode(proxyOptionFieldNumber, 99)), "out-of-range enum must not match")

	// Foreign tag (e.g. some other unknown extension) followed by AUTO must
	// still resolve to true; the scanner uses ConsumeFieldValue to skip past
	// the foreign tag without losing track of the buffer.
	mixed := append(encode(60002, 7), encode(proxyOptionFieldNumber, 1)...)
	assert.True(t, scanUnknownForAutoProxy(mixed), "foreign tag must not block subsequent AUTO match")
}
