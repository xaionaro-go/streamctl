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

type Serializable[T comparable] struct {
	Value T
}

type serializable[T any] struct {
	Type  string `yaml:"type"`
	Value T      `yaml:",inline"`
}

var _ serializableInterface = (*Serializable[struct{}])(nil)

func (s *Serializable[T]) UnmarshalYAML(b []byte) (_err error) {
	var value T
	intermediate := serializable[struct{}]{}
	defer func() {
		if r := recover(); r != nil {
			_err = fmt.Errorf("got a panic (t:%s:%T): %v\n%s\n%s", intermediate.Type, value, r, debug.Stack(), b)
		}
	}()

	err := yaml.Unmarshal(b, &intermediate)
	if err != nil {
		return fmt.Errorf("unable to unmarshal the intermediate structure for %T: %w", s.Value, err)
	}

	if intermediate.Type == "" {
		return fmt.Errorf("'type' is not set")
	}

	var ok bool
	value, ok = NewByTypeName[T](intermediate.Type)
	if !ok {
		v, _ := NewByTypeName[any](intermediate.Type)
		return fmt.Errorf("unknown/unregistered/not-fitting-%T value type name: '%v' (%T)", value, intermediate.Type, v)
	}

	err = yaml.Unmarshal(b, value)
	if err != nil {
		return fmt.Errorf("unable to unmarshal to the %T structure: %w", value, err)
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
