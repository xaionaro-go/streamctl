package action

import (
	"github.com/xaionaro-go/serializable"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
)

func init() {
	serializable.RegisterType[EndStream]()
}

type EndStream struct {
	PlatID streamcontrol.PlatformName
}

var _ Action = (*EndStream)(nil)

func (*EndStream) isAction() {}

func (a *EndStream) String() string {
	if a == nil {
		return "null"
	}
	return string(tryJSON(*a))
}
