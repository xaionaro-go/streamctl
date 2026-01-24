package action

import (
	"encoding/json"

	"github.com/xaionaro-go/serializable"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
)

func init() {
	serializable.RegisterType[SetTitle]()
}

type SetTitle struct {
	StreamID streamcontrol.StreamIDFullyQualified `yaml:"stream_id,omitempty" json:"stream_id,omitempty"`
	Title    string                               `yaml:"title,omitempty" json:"title,omitempty"`
}

var _ Action = (*SetTitle)(nil)

func (*SetTitle) isAction() {}

func (a *SetTitle) String() string {
	if a == nil {
		return "null"
	}
	b, _ := json.Marshal(a)
	return string(b)
}
