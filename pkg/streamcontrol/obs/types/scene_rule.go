package types

import (
	"fmt"
	"runtime/debug"

	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/obs/types/action"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/obs/types/registry"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/obs/types/trigger"
	"gopkg.in/yaml.v2"
)

type SceneRules []SceneRule

type SceneRule struct {
	Description  string        `yaml:"description,omitempty" json:"description,omitempty"`
	TriggerQuery trigger.Query `yaml:"trigger" json:"trigger"`
	Action       action.Action `yaml:"action" json:"action"`
}

func (sr *SceneRule) UnmarshalYAML(b []byte) (_err error) {
	defer func() {
		if r := recover(); r != nil {
			_err = fmt.Errorf("got a panic: %v\n%s", r, debug.Stack())
		}
	}()
	if sr == nil {
		return fmt.Errorf("nil SceneRule")
	}

	intermediate := serializableSceneRule{}
	err := yaml.Unmarshal(b, &intermediate)
	if err != nil {
		return fmt.Errorf("unable to unmarshal the MonitorElementConfig: %w", err)
	}

	triggerQueryType, _ := intermediate.Trigger["type"].(string)
	if triggerQueryType == "" {
		return fmt.Errorf("trigger type is not set")
	}

	actionType, _ := intermediate.Action["type"].(string)
	if actionType == "" {
		return fmt.Errorf("action type is not set")
	}

	triggerQuery := trigger.NewByTypeName(triggerQueryType)
	if triggerQuery == nil {
		return fmt.Errorf("unknown trigger type name: '%v'", triggerQueryType)
	}

	action := action.NewByTypeName(actionType)
	if action == nil {
		return fmt.Errorf("unknown action type name: '%v'", actionType)
	}

	*sr = SceneRule{
		Description:  intermediate.Description,
		TriggerQuery: triggerQuery,
		Action:       action,
	}
	return nil
}

func (sr SceneRule) MarshalYAML() (b []byte, _err error) {
	defer func() {
		if r := recover(); r != nil {
			_err = fmt.Errorf("got a panic: %v\n%s", r, debug.Stack())
		}
	}()

	triggerBytes, err := yaml.Marshal(sr.TriggerQuery)
	if err != nil {
		return nil, fmt.Errorf("unable to serialize the trigger %T:%#+v: %w", sr.TriggerQuery, sr.TriggerQuery, err)
	}

	triggerMap := map[string]any{}
	err = yaml.Unmarshal(triggerBytes, &triggerMap)
	if err != nil {
		return nil, fmt.Errorf("unable to unserialize the trigger '%s' into a map: %w", triggerBytes, err)
	}

	triggerMap["type"] = registry.ToTypeName(sr.TriggerQuery)

	actionBytes, err := yaml.Marshal(sr.Action)
	if err != nil {
		return nil, fmt.Errorf("unable to serialize the action %T:%#+v: %w", sr.Action, sr.Action, err)
	}

	actionMap := map[string]any{}
	err = yaml.Unmarshal(actionBytes, &actionMap)
	if err != nil {
		return nil, fmt.Errorf("unable to unserialize the action '%s' into a map: %w", actionBytes, err)
	}

	actionMap["type"] = registry.ToTypeName(sr.Action)

	intermediate := serializableSceneRule{
		Description: sr.Description,
		Trigger:     triggerMap,
		Action:      actionMap,
	}
	return yaml.Marshal(intermediate)
}

type serializableSceneRule struct {
	Description string         `yaml:"description,omitempty" json:"description,omitempty"`
	Trigger     map[string]any `yaml:"trigger" json:"trigger"`
	Action      map[string]any `yaml:"action" json:"action"`
}
