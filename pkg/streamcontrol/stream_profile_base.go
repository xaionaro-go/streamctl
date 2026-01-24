package streamcontrol

type StreamProfileBase struct {
	Parent      ProfileName `yaml:"parent,omitempty"`
	Order       int         `yaml:"order,omitempty"`
	Title       string      `yaml:"title,omitempty"`
	Description string      `yaml:"description,omitempty"`
}

func (profile StreamProfileBase) GetParent() (ProfileName, bool) {
	if profile.Parent == "" {
		return "", false
	}
	return profile.Parent, true
}

func (profile StreamProfileBase) GetOrder() int {
	return profile.Order
}

func (profile *StreamProfileBase) SetOrder(v int) {
	profile.Order = v
}

func (profile StreamProfileBase) GetTitle() (string, bool) {
	if profile.Title == "" {
		return "", false
	}
	return profile.Title, true
}

func (profile StreamProfileBase) GetDescription() (string, bool) {
	if profile.Description == "" {
		return "", false
	}
	return profile.Description, true
}
