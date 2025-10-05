package secret

import (
	"encoding/base64"
	"fmt"

	"github.com/xaionaro-go/secret"
	"gopkg.in/yaml.v2"
)

type Any[T any] struct {
	secret.Any[T]
}

func New[T any](in T) Any[T] {
	return Any[T]{Any: secret.New(in)}
}

func (s Any[T]) MarshalYAML() (_ret []byte, _err error) {
	defer func() {
		if r := recover(); r != nil {
			_err = fmt.Errorf("got a panic: %v", r)
		}
	}()

	var v any = s.Get()
	if b, ok := v.([]byte); ok {
		v = base64.StdEncoding.EncodeToString(b)
	}
	b, err := yaml.Marshal(v)
	return b, err
}

func (s *Any[T]) UnmarshalYAML(b []byte) (_err error) {
	defer func() {
		if r := recover(); r != nil {
			_err = fmt.Errorf("got a panic: %v", r)
		}
	}()

	var v T
	if _, ok := any(v).([]byte); ok {
		var str string
		err := yaml.Unmarshal(b, &str)
		if err != nil {
			return fmt.Errorf("unable to yaml.Unmarshal: %w", err)
		}
		b, err := base64.StdEncoding.DecodeString(str)
		if err != nil {
			return fmt.Errorf("unable to decode '%s' as base64: %w", str, err)
		}
		s.Set(any(b).(T))
	} else {
		err := yaml.Unmarshal(b, &v)
		if err != nil {
			return fmt.Errorf("unable to yaml.Unmarshal: %w", err)
		}
		s.Set(v)
	}
	return nil
}
