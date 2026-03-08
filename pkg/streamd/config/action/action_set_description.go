package action

import (
	"encoding/json"

	"github.com/xaionaro-go/serializable"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
)

func init() {
	serializable.RegisterType[SetDescription]()
}

type SetDescription struct {
	StreamID    streamcontrol.StreamIDFullyQualified `yaml:"stream_id,omitempty" json:"stream_id,omitempty"`
	Description string                               `yaml:"description,omitempty" json:"description,omitempty"`
}

var _ Action = (*SetDescription)(nil)

func (*SetDescription) isAction() {}

func (a *SetDescription) String() string {
	if a == nil {
		return "null"
	}
	b, _ := json.Marshal(a)
	return string(b)
}
