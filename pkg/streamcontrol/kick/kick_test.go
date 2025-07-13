package kick

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestParseTimestamp(t *testing.T) {
	type testCase struct {
		Input  string
		Output time.Time
	}
	for _, testCase := range []testCase{
		{Input: "2025-02-21T23:24:36Z", Output: time.Date(2025, 02, 21, 23, 24, 36, 0, time.UTC)},
	} {
		t.Run(testCase.Input, func(t *testing.T) {
			result, err := ParseTimestamp(testCase.Input)
			require.NoError(t, err)
			require.Equal(t, testCase.Output.UTC(), result.UTC())
		})
	}
}
