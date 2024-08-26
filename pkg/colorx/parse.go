package colorx

import (
	"fmt"
	"image/color"
	"strings"
)

func Parse(s string) (color.Color, error) {
	if len(s) == 0 {
		return nil, fmt.Errorf("empty string")
	}

	if strings.HasPrefix(s, "#") {
		return ParseHex(s[1:])
	}

	// TODO: add support of other formats
	return ParseHex(s)
}

func hexToByte(in byte) uint8 {
	switch {
	case '0' <= in && in <= '9':
		return in - '0'
	case 'A' <= in && in <= 'F':
		return 10 + (in - 'A')
	case 'a' <= in && in <= 'f':
		return 10 + (in - 'a')
	}
	panic(fmt.Errorf("unexpected character '%c'", in))
}

func ParseHex(s string) (_ret color.RGBA, _err error) {
	defer func() {
		if r := recover(); r != nil {
			_err = fmt.Errorf("%v", r)
		}
	}()
	switch len(s) {
	case 6:
		return color.RGBA{
			R: hexToByte(s[0])<<4 | hexToByte(s[1]),
			G: hexToByte(s[2])<<4 | hexToByte(s[3]),
			B: hexToByte(s[4])<<4 | hexToByte(s[5]),
			A: 1,
		}, nil
	case 8:
		return color.RGBA{
			R: hexToByte(s[0])<<4 | hexToByte(s[1]),
			G: hexToByte(s[2])<<4 | hexToByte(s[3]),
			B: hexToByte(s[4])<<4 | hexToByte(s[5]),
			A: hexToByte(s[6])<<4 | hexToByte(s[7]),
		}, nil
	}
	return color.RGBA{}, fmt.Errorf("unexpected length: %d", len(s))
}
