package colorx

import (
	"image/color"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseHex(t *testing.T) {
	c, err := ParseHex("010a030F")
	require.NoError(t, err)
	require.Equal(t, color.RGBA{
		R: 1,
		G: 0xA,
		B: 3,
		A: 0xF,
	}, c)
}
