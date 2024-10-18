package eventquery

import (
	"fmt"

	"github.com/goccy/go-yaml"
	"github.com/xaionaro-go/streamctl/pkg/serializable"
	"github.com/xaionaro-go/streamctl/pkg/serializable/registry"
	"github.com/xaionaro-go/streamctl/pkg/streamd/config/event"
)

type serializableInterface interface {
	yaml.BytesMarshaler
	yaml.BytesUnmarshaler
}

func init() {
	//serializable.RegisterType[EventType[event.WindowFocusChange]]()
	serializable.RegisterType[Event]()
}

type EventQuery interface {
	fmt.Stringer
	Match(event.Event) bool
	Get() EventQuery
}

type Event struct{ event.Event }

var _ serializableInterface = (*Event)(nil)

func (ev *Event) Match(cmp event.Event) bool {
	return ev.Event.Match(cmp)
}
func (ev *Event) Get() EventQuery { return ev }

func (ev Event) MarshalYAML() ([]byte, error) {
	r := &serializable.Serializable[event.Event]{Value: ev.Event}
	b, err := r.MarshalYAML()
	if err != nil {
		return nil, fmt.Errorf("unable to serialize eventquery.Query (%#+v): %w", ev, err)
	}
	return b, nil
}

func (ev *Event) UnmarshalYAML(b []byte) error {
	r := &serializable.Serializable[event.Event]{}
	if err := r.UnmarshalYAML(b); err != nil {
		return fmt.Errorf("unable to unserialize eventquery.Query (%#+v): %w", ev, err)
	}
	ev.Event = r.Value
	return nil
}

func (ev *Event) String() string {
	content := ev.Event.String()
	if content == "{}" {
		content = ""
	} else {
		content = ":" + content
	}
	return fmt.Sprintf("%s%s", registry.ToTypeName(ev.Event), content)
}

type EventType[T event.Event] struct{}

func (*EventType[T]) Match(ev event.Event) bool {
	_, ok := ev.(T)
	return ok
}
func (ev *EventType[T]) Get() EventQuery { return ev }
func (*EventType[T]) String() string {
	return fmt.Sprintf("event_type:%s", registry.ToTypeName(new(T)))
}
