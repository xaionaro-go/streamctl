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
	Host        *string `yaml:"host,omitempty"         json:"host,omitempty"`
	WindowID    *uint64 `yaml:"window_id,omitempty"    json:"window_id,omitempty"`
	WindowTitle *string `yaml:"window_title,omitempty" json:"window_title,omitempty"`
	UserID      *uint64 `yaml:"user_id,omitempty"      json:"user_id,omitempty"`
	ProcessID   *uint64 `yaml:"process_id,omitempty"   json:"process_id,omitempty"`
	ProcessName *string `yaml:"process_name,omitempty" json:"process_name,omitempty"`
	IsFocused   *bool   `yaml:"is_focused,omitempty"   json:"is_focused,omitempty"`
}

func (ev *WindowFocusChange) Get() Event { return ev }

func (ev *WindowFocusChange) Match(cmpIface Event) bool {
	cmp, ok := cmpIface.(*WindowFocusChange)
	if !ok {
		return false
	}

	if !fieldMatch(ev.Host, cmp.Host) {
		return false
	}
	if !fieldMatch(ev.WindowID, cmp.WindowID) {
		return false
	}
	if !fieldMatch(ev.WindowTitle, cmp.WindowTitle) {
		return false
	}
	if !fieldMatch(ev.UserID, cmp.UserID) {
		return false
	}
	if !fieldMatch(ev.ProcessID, cmp.ProcessID) {
		return false
	}
	if !fieldMatch(ev.ProcessName, cmp.ProcessName) {
		return false
	}
	if !fieldMatch(ev.IsFocused, cmp.IsFocused) {
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
