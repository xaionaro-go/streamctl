package action

import (
	"encoding/json"

	"github.com/xaionaro-go/serializable"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
)

func init() {
	serializable.RegisterType[StartStreamByProfileName]()
}

type StartStreamByProfileName struct {
	StreamID    streamcontrol.StreamIDFullyQualified `yaml:"stream_id,omitempty" json:"stream_id,omitempty"`
	ProfileName streamcontrol.ProfileName            `yaml:"profile_name,omitempty" json:"profile_name,omitempty"`
}

var _ Action = (*StartStreamByProfileName)(nil)

func (*StartStreamByProfileName) isAction() {}

func (a *StartStreamByProfileName) String() string {
	if a == nil {
		return "null"
	}
	b, _ := json.Marshal(a)
	return string(b)
}
