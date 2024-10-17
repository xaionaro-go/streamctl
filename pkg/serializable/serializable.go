package serializable

import (
	"fmt"
	"runtime/debug"

	"github.com/goccy/go-yaml"
	"github.com/xaionaro-go/streamctl/pkg/serializable/registry"
)

type serializableInterface interface {
	yaml.BytesMarshaler
	yaml.BytesUnmarshaler
}

type Serializable[T any] struct {
	Value T
}

type serializable[T any] struct {
	Type  string `yaml:"type"`
	Value T      `yaml:",inline"`
}

var _ serializableInterface = (*Serializable[struct{}])(nil)

func (s *Serializable[T]) UnmarshalYAML(b []byte) (_err error) {
	defer func() {
		if r := recover(); r != nil {
			_err = fmt.Errorf("got a panic: %v\n%s", r, debug.Stack())
		}
	}()

	intermediate := serializable[struct{}]{}
	err := yaml.Unmarshal(b, &intermediate)
	if err != nil {
		return fmt.Errorf("unable to unmarshal the intermediate structure for %T: %w", s.Value, err)
	}

	if intermediate.Type == "" {
		return fmt.Errorf("'type' is not set")
	}

	value, ok := NewByTypeName[T](intermediate.Type)
	if !ok {
		return fmt.Errorf("unknown/unregistered value type name: '%v'", intermediate.Type)
	}

	err = yaml.Unmarshal(b, value)
	if err != nil {
		return fmt.Errorf("unable to unmarshal the %T structure: %w", s.Value, err)
	}

	s.Value = value
	return nil
}

func (s Serializable[T]) MarshalYAML() (_ []byte, _err error) {
	defer func() {
		if r := recover(); r != nil {
			_err = fmt.Errorf("got a panic: %v\n%s", r, debug.Stack())
		}
	}()

	return yaml.Marshal(serializable[T]{
		Type:  registry.ToTypeName(s.Value),
		Value: s.Value,
	})
}
