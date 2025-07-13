package event

import (
	"github.com/xaionaro-go/serializable"
)

func init() {
	serializable.RegisterType[OBSSceneChange]()
}

type OBSSceneChange struct {
	UUIDFrom *string `yaml:"uuid_from,omitempty" json:"uuid_from,omitempty"`
	UUIDTo   *string `yaml:"uuid_to,omitempty"   json:"uuid_to,omitempty"`
	NameFrom *string `yaml:"name_from,omitempty" json:"name_from,omitempty"`
	NameTo   *string `yaml:"name_to,omitempty"   json:"name_to,omitempty"`
}

func (ev *OBSSceneChange) Get() Event { return ev }

func (ev *OBSSceneChange) Match(cmpIface Event) bool {
	cmp, ok := cmpIface.(*OBSSceneChange)
	if !ok {
		return false
	}

	if !fieldMatch(ev.UUIDFrom, cmp.UUIDFrom) {
		return false
	}
	if !fieldMatch(ev.UUIDTo, cmp.UUIDTo) {
		return false
	}
	if !fieldMatch(ev.NameFrom, cmp.NameFrom) {
		return false
	}
	if !fieldMatch(ev.NameTo, cmp.NameTo) {
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
