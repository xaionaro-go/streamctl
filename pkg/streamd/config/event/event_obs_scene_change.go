package event

import (
	"github.com/xaionaro-go/serializable"
)

func init() {
	serializable.RegisterType[OBSSceneChange]()
}

type OBSSceneChange struct {
	From *string `yaml:"from,omitempty" json:"from,omitempty"`
	To   *string `yaml:"to,omitempty"   json:"to,omitempty"`
}

func (ev *OBSSceneChange) Get() Event { return ev }

func (ev *OBSSceneChange) Match(cmpIface Event) bool {
	cmp, ok := cmpIface.(*OBSSceneChange)
	if !ok {
		return false
	}

	if !fieldMatch(ev.From, cmp.From) {
		return false
	}
	if !fieldMatch(ev.To, cmp.To) {
		return false
	}

	return true
}

func (ev *OBSSceneChange) String() string {
	if ev == nil {
		return "null"
	}
	return string(tryJSON(*ev))
}
