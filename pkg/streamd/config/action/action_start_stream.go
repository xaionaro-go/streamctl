package action

import (
	"github.com/xaionaro-go/serializable"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
)

func init() {
	serializable.RegisterType[StartStream]()
}

type StartStream struct {
	PlatID      streamcontrol.PlatformName
	Title       string
	Description string
	Profile     streamcontrol.AbstractStreamProfile
	CustomArgs  []any

	//lint:ignore U1000 this field is used by reflection
	uiDisable struct{} // currently out current reflect-y generator of fyne-Entry-ies does not support interfaces like field 'Profile' here, so we just forbid using this action.
}

var _ Action = (*StartStream)(nil)

func (*StartStream) isAction() {}

func (a *StartStream) String() string {
	if a == nil {
		return "null"
	}
	return string(tryJSON(*a))
}
