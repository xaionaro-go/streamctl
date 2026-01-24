package streamtypes

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStreamSinkType_String(t *testing.T) {
	for c := range endOfStreamSinkType {
		assert.NotContains(t, c.String(), "unknown", "StreamSinkType %d does not have a proper string defined", c)
	}
}
