package action

import (
	"encoding/json"
	"fmt"

	"github.com/xaionaro-go/streamctl/pkg/expression"
	"github.com/xaionaro-go/serializable"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
)

func init() {
	serializable.RegisterType[Noop]()
	serializable.RegisterType[OBSItemShowHide]()
	serializable.RegisterType[OBSWindowCaptureSetSource]()
	serializable.RegisterType[StartStream]()
	serializable.RegisterType[EndStream]()
}

type Action interface {
	fmt.Stringer
	isAction() // just to enable build-time type checks
}

type ValueExpression = expression.Expression

type OBSItemShowHide struct {
	ItemName        *string         `yaml:"item_name,omitempty"        json:"item_name,omitempty"`
	ItemUUID        *string         `yaml:"item_uuid,omitempty"        json:"item_uuid,omitempty"`
	ValueExpression ValueExpression `yaml:"value_expression,omitempty" json:"value_expression,omitempty"`
}

var _ Action = (*OBSItemShowHide)(nil)

func (OBSItemShowHide) isAction() {}

func (a *OBSItemShowHide) String() string {
	if a == nil {
		return "null"
	}
	return string(tryJSON(*a))
}

type OBSWindowCaptureSetSource struct {
	ItemName        *string         `yaml:"item_name,omitempty"        json:"item_name,omitempty"`
	ItemUUID        *string         `yaml:"item_uuid,omitempty"        json:"item_uuid,omitempty"`
	ValueExpression ValueExpression `yaml:"value_expression,omitempty" json:"value_expression,omitempty"`
}

var _ Action = (*OBSWindowCaptureSetSource)(nil)

func (OBSWindowCaptureSetSource) isAction() {}

func (a *OBSWindowCaptureSetSource) String() string {
	if a == nil {
		return "null"
	}
	return string(tryJSON(*a))
}

type Noop struct{}

var _ Action = (*Noop)(nil)

func (*Noop) isAction() {}

func (a *Noop) String() string {
	if a == nil {
		return "null"
	}
	return string(tryJSON(*a))
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

func tryJSON(value any) []byte {
	b, _ := json.Marshal(value)
	return b
}
