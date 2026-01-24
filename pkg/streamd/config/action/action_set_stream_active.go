package action

import (
	"encoding/json"

	"github.com/xaionaro-go/serializable"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
)

func init() {
	serializable.RegisterType[SetStreamActive]()
}

type SetStreamActive struct {
	StreamID streamcontrol.StreamIDFullyQualified `yaml:"stream_id,omitempty" json:"stream_id,omitempty"`
	IsActive bool                                 `yaml:"is_active,omitempty" json:"is_active,omitempty"`
}

var _ Action = (*SetStreamActive)(nil)

func (*SetStreamActive) isAction() {}

func (a *SetStreamActive) String() string {
	if a == nil {
		return "null"
	}
	b, _ := json.Marshal(a)
	return string(b)
}
