package action

import (
	"encoding/json"

	"github.com/xaionaro-go/serializable"
)

func init() {
	serializable.RegisterType[OBSItemShowHide]()
}

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

func tryJSON(value any) []byte {
	b, _ := json.Marshal(value)
	return b
}
