package oto

import (
	"github.com/ebitengine/oto/v3"
	"github.com/xaionaro-go/streamctl/pkg/audio/types"
)

func FormatToOto(f types.PCMFormat) oto.Format {
	switch f {
	case types.PCMFormatFloat32LE:
		return oto.FormatFloat32LE
	}
	return oto.Format(-1)
}
