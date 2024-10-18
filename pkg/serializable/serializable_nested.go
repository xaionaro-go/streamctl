package serializable

import (
	"context"
	"fmt"
	"runtime/debug"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/goccy/go-yaml"
	"github.com/xaionaro-go/streamctl/pkg/serializable/registry"
)

type SerializableNested[T any] struct {
	Value T
}

type serializableNested[T any] struct {
	Type  string `yaml:"type"`
	Value T      `yaml:"value"`
}

var _ serializableInterface = (*SerializableNested[struct{}])(nil)

func (s *SerializableNested[T]) UnmarshalYAML(b []byte) (_err error) {
	var value T
	intermediateStep0 := serializableNested[struct{}]{}
	defer func() {
		if r := recover(); r != nil {
			_err = fmt.Errorf("got a panic (t:%s:%T): %v\n%s\n%s", intermediateStep0.Type, value, r, debug.Stack(), b)
		}
	}()

	err := yaml.Unmarshal(b, &intermediateStep0)
	if err != nil {
		return fmt.Errorf("unable to unmarshal the intermediate structure for %T: %w", s.Value, err)
	}

	if intermediateStep0.Type == "" {
		return fmt.Errorf("'type' is not set")
	}

	var ok bool
	value, ok = NewByTypeName[T](intermediateStep0.Type)
	if !ok {
		v, _ := NewByTypeName[any](intermediateStep0.Type)
		return fmt.Errorf("unknown/unregistered/not-fitting-%T value type name: '%v' (%T): %s", value, intermediateStep0.Type, v, b)
	}
	logger.Tracef(context.TODO(), "value == %T", value)

	m := map[string]any{}
	err = yaml.Unmarshal(b, &m)
	if err != nil {
		return fmt.Errorf("unable to unmarshal to the %T structure: %w: %s", value, err, b)
	}

	b, err = yaml.Marshal(m["value"])
	if err != nil {
		return fmt.Errorf("unable to temporary-YAML-ize: %w", err)
	}

	err = yaml.Unmarshal(b, value)
	if err != nil {
		return fmt.Errorf("unable to un-YAML-ize the temporary YAML: %w", err)
	}
	logger.Tracef(context.TODO(), "value == %T:%#+v", value, value)

	s.Value = value
	logger.Tracef(context.TODO(), "s.Value == %s", s.Value)
	return nil
}

func (s SerializableNested[T]) MarshalYAML() (_ []byte, _err error) {
	defer func() {
		if r := recover(); r != nil {
			_err = fmt.Errorf("got a panic: %v\n%s", r, debug.Stack())
		}
	}()

	return yaml.Marshal(serializableNested[T]{
		Type:  registry.ToTypeName(s.Value),
		Value: s.Value,
	})
}
