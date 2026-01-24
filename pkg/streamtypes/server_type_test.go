package streamtypes

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestServerType_String(t *testing.T) {
	for c := UndefinedServerType; c < endOfServerType; c++ {
		assert.NotContains(t, c.String(), "unknown", "ServerType %d does not have a proper string defined", c)
	}
}
