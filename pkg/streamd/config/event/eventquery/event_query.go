package eventquery

import (
	"github.com/xaionaro-go/streamctl/pkg/serializable"
	"github.com/xaionaro-go/streamctl/pkg/streamd/config/event"
)

func init() {
	serializable.RegisterType[EventType[event.WindowFocusChange]]()
}

type EventQuery interface {
	Match(event.Event) bool
}

type Event serializable.Serializable[event.Event]

func (ev Event) Match(cmp event.Event) bool {
	return ev.Value == cmp
}

type EventType[T event.Event] struct{}

func (EventType[T]) Match(ev event.Event) bool {
	_, ok := ev.(T)
	return ok
}
