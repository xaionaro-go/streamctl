package clock

import (
	"github.com/benbjohnson/clock"
)

var globalClock clock.Clock = clock.New()

func Get() clock.Clock {
	return globalClock
}

func Set(clk clock.Clock) {
	globalClock = clk
}

type Timer = clock.Timer
type Mock = clock.Mock

func New() clock.Clock {
	return clock.New()
}

func NewMock() *Mock {
	return clock.NewMock()
}
