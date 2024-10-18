package event

import (
	"encoding/json"
	"fmt"

	"github.com/xaionaro-go/streamctl/pkg/serializable"
)

func init() {
	serializable.RegisterType[WindowFocusChange]()
}

type Event interface {
	fmt.Stringer
	Get() Event
	Match(Event) bool
}

type WindowFocusChange struct {
	WindowID           *uint64 `yaml:"window_id,omitempty"            json:"window_id,omitempty"`
	WindowTitle        *string `yaml:"window_title,omitempty"         json:"window_title,omitempty"`
	WindowTitlePartial *string `yaml:"window_title_partial,omitempty" json:"window_title_partial,omitempty"`
	UserID             *uint64 `yaml:"user_id,omitempty"              json:"user_id,omitempty"`

	//lint:ignore U1000 this field is used by reflection
	uiComment struct{} `uicomment:"This action will also add field .IsFocused to the event."`
}

func (ev *WindowFocusChange) Get() Event { return ev }

func (ev *WindowFocusChange) Match(cmpIface Event) bool {
	cmp, ok := cmpIface.(*WindowFocusChange)
	if !ok {
		return false
	}

	if !fieldMatch(ev.WindowID, cmp.WindowID) {
		return false
	}
	if !fieldMatch(ev.WindowTitle, cmp.WindowTitle) {
		return false
	}
	if !partialFieldMatch(ev.WindowTitle, cmp.WindowTitlePartial) &&
		!partialFieldMatch(cmp.WindowTitle, ev.WindowTitlePartial) {
		return false
	}
	if !fieldMatch(ev.WindowID, cmp.WindowID) {
		return false
	}

	return true
}

func (ev *WindowFocusChange) String() string {
	return string(tryJSON(ev))
}

func tryJSON(value any) []byte {
	b, _ := json.Marshal(value)
	return b
}
