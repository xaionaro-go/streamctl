package streamtypes

import (
	"github.com/xaionaro-go/xsync"
)

type OBSState struct {
	xsync.Mutex
	VolumeMeters map[string][][3]float64
}
