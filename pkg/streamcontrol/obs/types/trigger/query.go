package trigger

func init() {
	//registry.RegisterType((*Not)(nil))
	registry.RegisterType((*WindowFocusChange)(nil))
}

type Query interface {
	isTriggerQuery()
}

type Not struct {
	Query `yaml:"query"`
}

type WindowFocusChange struct {
	WindowID           *uint64 `yaml:"window_id,omitempty"            json:"window_id,omitempty"`
	WindowTitle        *string `yaml:"window_title,omitempty"         json:"window_title,omitempty"`
	WindowTitlePartial *string `yaml:"window_title_partial,omitempty" json:"window_title_partial,omitempty"`
	UserID             *uint64 `yaml:"user_id,omitempty"              json:"user_id,omitempty"`

	//lint:ignore U1000 this field is used by reflection
	uiComment struct{} `uicomment:"This action will also add field .IsFocused to the event."`
}

func (WindowFocusChange) isTriggerQuery() {}
