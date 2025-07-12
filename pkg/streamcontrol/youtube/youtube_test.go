package youtube

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestParseTimestamp(t *testing.T) {
	type testCase struct {
		input  string
		output time.Time
	}

	for _, testCase := range []testCase{
		{input: "2025-07-09T11:49:53Z", output: time.Date(2025, 7, 9, 11, 49, 53, 0, time.UTC)},
	} {
		t.Run(testCase.input, func(t *testing.T) {
			r, err := ParseTimestamp(testCase.input)
			require.NoError(t, err)
			require.Equal(t, testCase.output, r.UTC())
		})
	}
}
