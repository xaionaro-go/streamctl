package subtitleswindow

import (
	"sync/atomic"
)

type onceCloser atomic.Uint64

func (oc *onceCloser) Do(fn func()) {
	if (*atomic.Uint64)(oc).Add(1) != 1 {
		return
	}
	fn()
}
