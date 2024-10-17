package config

import (
	"encoding/json"
	"fmt"

	"github.com/goccy/go-yaml"

	"github.com/xaionaro-go/streamctl/pkg/serializable"
	"github.com/xaionaro-go/streamctl/pkg/serializable/registry"
	"github.com/xaionaro-go/streamctl/pkg/streamd/config/action"
	"github.com/xaionaro-go/streamctl/pkg/streamd/config/event"
	"github.com/xaionaro-go/streamctl/pkg/streamd/config/event/eventquery"
)

type TriggerRules []*TriggerRule
type Event = event.Event
type EventQuery = eventquery.EventQuery

type TriggerRule struct {
	Description string                `yaml:"description,omitempty" json:"description,omitempty"`
	EventQuery  eventquery.EventQuery `yaml:"trigger"               json:"trigger"`
	Action      action.Action         `yaml:"action"                json:"action"`
}

var _ yaml.BytesMarshaler = (*TriggerRule)(nil)
var _ yaml.BytesUnmarshaler = (*TriggerRule)(nil)

func (tr *TriggerRule) UnmarshalYAML(b []byte) (_err error) {
	if tr == nil {
		return fmt.Errorf("nil TriggerRule")
	}

	intermediate := serializableTriggerRule{}
	err := yaml.Unmarshal(b, &intermediate)
	if err != nil {
		return fmt.Errorf("unable to unmarshal the TriggerRule: %w", err)
	}

	*tr = TriggerRule{
		Description: intermediate.Description,
		EventQuery:  intermediate.EventQuery.Value,
		Action:      intermediate.Action.Value,
	}
	return nil
}

func (tr TriggerRule) MarshalYAML() (b []byte, _err error) {
	return yaml.Marshal(serializableTriggerRule{
		Description: "",
		EventQuery:  serializable.Serializable[eventquery.EventQuery]{Value: tr.EventQuery},
		Action:      serializable.Serializable[action.Action]{Value: tr.Action},
	})
}

type serializableTriggerRule struct {
	Description string                                           `yaml:"description,omitempty" json:"description,omitempty"`
	EventQuery  serializable.Serializable[eventquery.EventQuery] `yaml:"trigger"               json:"trigger"`
	Action      serializable.Serializable[action.Action]         `yaml:"action"                json:"action"`
}

func (tr TriggerRule) String() string {
	descr := tr.Description
	if descr != "" {
		descr += ": "
	}
	eventQueryJSON := string(tryJSON(tr.EventQuery))
	if eventQueryJSON != "{}" {
		eventQueryJSON = ":" + eventQueryJSON
	} else {
		eventQueryJSON = ""
	}
	actionJSON := string(tryJSON(tr.Action))
	if actionJSON != "{}" {
		actionJSON = ":" + actionJSON
	} else {
		actionJSON = ""
	}
	return fmt.Sprintf(
		"%s%s%s -> %s%s",
		descr,
		typeName(tr.EventQuery), eventQueryJSON,
		typeName(tr.Action), actionJSON,
	)
}

func typeName(value any) string {
	return registry.ToTypeName(value)
}

func tryJSON(value any) []byte {
	b, _ := json.Marshal(value)
	return b
}
