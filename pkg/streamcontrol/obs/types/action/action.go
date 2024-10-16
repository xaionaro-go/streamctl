package action

func init() {
	registry.RegisterType((*ElementShowHide)(nil))
	registry.RegisterType((*WindowCaptureSetSource)(nil))
}

type Action interface {
	isAction()
}

type ValueExpression string

type ElementShowHide struct {
	ElementName     *string         `yaml:"element_name,omitempty"     json:"element_name,omitempty"`
	ElementUUID     *string         `yaml:"element_uuid,omitempty"     json:"element_uuid,omitempty"`
	ValueExpression ValueExpression `yaml:"value_expression,omitempty" json:"value_expression,omitempty"`
}

func (ElementShowHide) isAction() {}

type WindowCaptureSetSource struct {
	ElementName     *string         `yaml:"element_name,omitempty"     json:"element_name,omitempty"`
	ElementUUID     *string         `yaml:"element_uuid,omitempty"     json:"element_uuid,omitempty"`
	ValueExpression ValueExpression `yaml:"value_expression,omitempty" json:"value_expression,omitempty"`
}

func (WindowCaptureSetSource) isAction() {}
