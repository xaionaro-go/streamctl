package config

import (
	"context"
	"fmt"
	"strings"

	"github.com/facebookincubator/go-belt/tool/logger"
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
		return fmt.Errorf("unable to unmarshal the TriggerRule: %w: %s", err, b)
	}

	*tr = TriggerRule{
		Description: intermediate.Description,
		EventQuery:  intermediate.EventQuery.Value,
		Action:      intermediate.Action.Value,
	}
	logger.Tracef(context.TODO(), "triggerRule == %s", *tr)
	if tr.EventQuery == nil {
		return fmt.Errorf("tr.EventQuery == nil")
	}
	return nil
}

func (tr TriggerRule) MarshalYAML() (b []byte, _err error) {
	logger.Debugf(context.TODO(), "triggerRule == %s", tr.String())
	return yaml.Marshal(serializableTriggerRule{
		Description: tr.Description,
		EventQuery:  serializable.SerializableNested[eventquery.EventQuery]{Value: tr.EventQuery},
		Action:      serializable.Serializable[action.Action]{Value: tr.Action},
	})
}

type serializableTriggerRule struct {
	Description string                                                 `yaml:"description,omitempty" json:"description,omitempty"`
	EventQuery  serializable.SerializableNested[eventquery.EventQuery] `yaml:"trigger"               json:"trigger"`
	Action      serializable.Serializable[action.Action]               `yaml:"action"                json:"action"`
}

func (tr *TriggerRule) String() string {
	if tr == nil {
		return "null"
	}

	descr := tr.Description
	if descr != "" {
		descr += ": "
	}
	eventQueryString := tr.EventQuery.String()
	if eventQueryString != "{}" {
		eventQueryString = ":" + eventQueryString
	} else {
		eventQueryString = ""
	}
	actionString := strings.Trim(tr.Action.String(), "&")
	if actionString != "{}" {
		actionString = ":" + actionString
	} else {
		actionString = ""
	}
	return fmt.Sprintf(
		"%s%s%s -> %s%s",
		descr,
		typeName(tr.EventQuery), eventQueryString,
		typeName(tr.Action), actionString,
	)
}

func typeName(value any) string {
	return registry.ToTypeName(value)
}
