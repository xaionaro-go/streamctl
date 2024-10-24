package secret

import (
	"encoding/json"
	"fmt"

	"github.com/goccy/go-yaml"
)

// Any stores data in an encrypted state in memory, so that
// you don't accidentally leak these secrets via logging or whatever.
type Any[T any] struct {
	encryptedMessage
	plainText T
}

func New[T any](in T) Any[T] {
	var s Any[T]
	s.Set(in)
	return s
}

func (s Any[T]) Get() T {
	if len(s.encryptedMessage.data) == 0 {
		var zeroValue T
		return zeroValue
	}
	b := decrypt(s.encryptedMessage)
	var t T
	err := json.Unmarshal(b, &t)
	if err != nil {
		panic(err)
	}
	return t
}

func (s *Any[T]) GetPointer() *T {
	if s == nil {
		return nil
	}
	return ptr(s.Get())
}

func (s *Any[T]) Set(v T) {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	s.encryptedMessage = encrypt(b)
	if !secrecyEnabled {
		s.plainText = v
	}
}

func (s *Any[T]) String() string {
	if secrecyEnabled {
		return "<HIDDEN>"
	}
	return fmt.Sprintf("%#v", s.plainText)
}

func (s Any[T]) GoString() string {
	if secrecyEnabled {
		return "<HIDDEN>"
	}
	return fmt.Sprintf("%#+v", s.plainText)
}

func (s Any[T]) MarshalYAML() (_ret []byte, _err error) {
	defer func() {
		if r := recover(); r != nil {
			_err = fmt.Errorf("got a panic: %v", r)
		}
	}()

	b, err := yaml.Marshal(s.Get())
	return b, err
}

func (s *Any[T]) UnmarshalYAML(b []byte) (_err error) {
	defer func() {
		if r := recover(); r != nil {
			_err = fmt.Errorf("got a panic: %v", r)
		}
	}()

	var v T
	err := yaml.Unmarshal(b, &v)
	if err != nil {
		return err
	}
	s.Set(v)
	return nil
}
