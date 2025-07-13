package action

import (
	"github.com/xaionaro-go/serializable"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
)

func init() {
	serializable.RegisterType[StartStreamByProfileName]()
}

type StartStreamByProfileName struct {
	PlatID      streamcontrol.PlatformName
	Title       *string
	Description *string
	ProfileName string
}

var _ Action = (*StartStreamByProfileName)(nil)

func (*StartStreamByProfileName) isAction() {}

func (a *StartStreamByProfileName) String() string {
	if a == nil {
		return "null"
	}
	return string(tryJSON(*a))
}
