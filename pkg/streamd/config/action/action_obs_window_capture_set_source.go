package action

import "github.com/xaionaro-go/serializable"

func init() {
	serializable.RegisterType[OBSWindowCaptureSetSource]()
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
