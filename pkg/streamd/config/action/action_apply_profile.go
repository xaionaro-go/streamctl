package action

import (
	"encoding/json"

	"github.com/xaionaro-go/serializable"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
)

func init() {
	serializable.RegisterType[ApplyProfile]()
}

type ApplyProfile struct {
	StreamID streamcontrol.StreamIDFullyQualified `yaml:"stream_id,omitempty" json:"stream_id,omitempty"`
	Profile  streamcontrol.ProfileName            `yaml:"profile,omitempty" json:"profile,omitempty"`
}

var _ Action = (*ApplyProfile)(nil)

func (*ApplyProfile) isAction() {}

func (a *ApplyProfile) String() string {
	if a == nil {
		return "null"
	}
	b, _ := json.Marshal(a)
	return string(b)
}
