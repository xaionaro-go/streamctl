package streamcontrol

import (
	"encoding/json"
	"fmt"

	"github.com/goccy/go-yaml"
)

type RawMessage json.RawMessage

var _ yaml.BytesUnmarshaler = (*RawMessage)(nil)

func (m RawMessage) GetStreamProfiles() map[StreamID]StreamProfiles[RawMessage] {
	var v struct {
		StreamProfiles map[StreamID]StreamProfiles[RawMessage] `yaml:"stream_profiles"`
	}
	err := yaml.Unmarshal(m, &v)
	if err != nil {
		return nil
	}
	return v.StreamProfiles
}

func (m RawMessage) IsEnabled() bool {
	var v struct {
		Enable *bool `yaml:"enable"`
	}
	err := yaml.Unmarshal(m, &v)
	if err != nil || v.Enable == nil {
		return false
	}
	return *v.Enable
}

func (m *RawMessage) SetEnabled(enabled bool) {
	var v map[string]any
	_ = yaml.Unmarshal(*m, &v)
	if v == nil {
		v = make(map[string]any)
	}
	v["enable"] = enabled
	b, _ := yaml.Marshal(v)
	*m = RawMessage(b)
}

func (m RawMessage) GetParent() (ProfileName, bool) {
	var v struct {
		Parent ProfileName `yaml:"parent"`
	}
	err := yaml.Unmarshal(m, &v)
	if err != nil || v.Parent == "" {
		return "", false
	}
	return v.Parent, true
}

func (m RawMessage) GetOrder() int {
	var v struct {
		Order int `yaml:"order"`
	}
	_ = yaml.Unmarshal(m, &v)
	return v.Order
}

func (m RawMessage) GetTitle() (string, bool) {
	var v struct {
		Title string `yaml:"title"`
	}
	_ = yaml.Unmarshal(m, &v)
	if v.Title == "" {
		return "", false
	}
	return v.Title, true
}

func (m RawMessage) GetDescription() (string, bool) {
	var v struct {
		Description string `yaml:"description"`
	}
	_ = yaml.Unmarshal(m, &v)
	if v.Description == "" {
		return "", false
	}
	return v.Description, true
}

func (m RawMessage) IsInitialized() bool {
	return len(m) > 0 && string(m) != "{}" && string(m) != "null"
}

func (m *RawMessage) UnmarshalJSON(b []byte) error {
	return (*json.RawMessage)(m).UnmarshalJSON(b)
}

func (m *RawMessage) UnmarshalYAML(b []byte) error {
	if m == nil {
		return fmt.Errorf("a nil receiver")
	}
	*m = append((*m)[0:0], b...)
	return nil
}

func (m RawMessage) MarshalJSON() ([]byte, error) {
	v := map[string]any{}
	err := yaml.Unmarshal(m, &v)
	if err != nil {
		return nil, fmt.Errorf("unable to unmarshal YAML: %w", err)
	}
	return json.Marshal(v)
}

func (m RawMessage) MarshalYAML() (any, error) {
	if len(m) == 0 {
		return nil, nil
	}
	var res any
	err := yaml.Unmarshal(m, &res)
	return res, err
}

func ToRawMessage(v any) RawMessage {
	if rm, ok := v.(RawMessage); ok {
		return rm
	}
	b, err := yaml.Marshal(v)
	if err != nil {
		panic(err)
	}
	return RawMessage(b)
}
