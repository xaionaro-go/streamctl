package action

import (
	"encoding/json"
	"fmt"

	"github.com/xaionaro-go/streamctl/pkg/expression"
	"github.com/xaionaro-go/streamctl/pkg/serializable"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
)

func init() {
	serializable.RegisterType[Noop]()
	serializable.RegisterType[OBSElementShowHide]()
	serializable.RegisterType[OBSWindowCaptureSetSource]()
	serializable.RegisterType[StartStream]()
	serializable.RegisterType[EndStream]()
}

type Action interface {
	fmt.Stringer
	isAction() // just to enable build-time type checks
}

type ValueExpression = expression.Expression

type OBSElementShowHide struct {
	ElementName     *string         `yaml:"element_name,omitempty"     json:"element_name,omitempty"`
	ElementUUID     *string         `yaml:"element_uuid,omitempty"     json:"element_uuid,omitempty"`
	ValueExpression ValueExpression `yaml:"value_expression,omitempty" json:"value_expression,omitempty"`
}

var _ Action = (*OBSElementShowHide)(nil)

func (OBSElementShowHide) isAction() {}

func (a OBSElementShowHide) String() string {
	return string(tryJSON(a))
}

type OBSWindowCaptureSetSource struct {
	ElementName     *string         `yaml:"element_name,omitempty"     json:"element_name,omitempty"`
	ElementUUID     *string         `yaml:"element_uuid,omitempty"     json:"element_uuid,omitempty"`
	ValueExpression ValueExpression `yaml:"value_expression,omitempty" json:"value_expression,omitempty"`
}

var _ Action = (*OBSWindowCaptureSetSource)(nil)

func (OBSWindowCaptureSetSource) isAction() {}

func (a OBSWindowCaptureSetSource) String() string {
	return string(tryJSON(a))
}

type Noop struct{}

var _ Action = (*Noop)(nil)

func (*Noop) isAction() {}

func (a Noop) String() string {
	return string(tryJSON(a))
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

func (a StartStream) String() string {
	return string(tryJSON(a))
}

type EndStream struct {
	PlatID streamcontrol.PlatformName
}

var _ Action = (*EndStream)(nil)

func (*EndStream) isAction() {}

func (a EndStream) String() string {
	return string(tryJSON(a))
}

func tryJSON(value any) []byte {
	b, _ := json.Marshal(value)
	return b
}
