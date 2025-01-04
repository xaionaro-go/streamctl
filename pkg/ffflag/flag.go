package ffflag

import (
	"strconv"
	"time"

	loggertypes "github.com/facebookincubator/go-belt/tool/logger"
)

type OptionSettings struct {
	Name                  string
	CollectUnknownOptions bool
	WithArgument          bool
}

type GenericOption struct {
	OptionSettings
	Wrapped[any]
	CollectedUnknownOptions [][]string
}

type Option[V any, W Wrapped[V]] struct {
	*GenericOption
}

func (opt Option[V, W]) Value() V {
	if w, ok := opt.GenericOption.Wrapped.(abstractWrapper[V]); ok {
		return w.Wrapped.Value()
	}
	return opt.GenericOption.Wrapped.(Valuer[V]).Value()
}

type Wrapped[V any] interface {
	Valuer[V]
	Parse(input string) error
}

type Valuer[V any] interface {
	Value() V
}

type abstractWrapper[V any] struct {
	Wrapped[V]
}

func (w abstractWrapper[V]) Value() any {
	return w.Wrapped.Value()
}

type String string

func (v *String) Parse(input string) error {
	*v = String(input)
	return nil
}

func (v *String) Value() string {
	return string(*v)
}

type StringsAsSeparateFlags []string

func (v *StringsAsSeparateFlags) Parse(input string) error {
	*v = append(*v, input)
	return nil
}

func (v *StringsAsSeparateFlags) Value() []string {
	return []string(*v)
}

type Bool bool

func (v *Bool) Parse(input string) error {
	if input == "" {
		*v = true
		return nil
	}
	b, err := strconv.ParseBool(input)
	if err != nil {
		return err
	}
	*v = Bool(b)
	return nil
}

func (v *Bool) Value() bool {
	return bool(*v)
}

type LogLevel loggertypes.Level

func (v *LogLevel) Parse(input string) error {
	if input == "verbose" {
		*v = LogLevel(loggertypes.LevelDebug)
		return nil
	}
	return (*loggertypes.Level)(v).Set(input)
}

func (v *LogLevel) Value() loggertypes.Level {
	return loggertypes.Level(*v)
}

type Duration time.Duration

func (v *Duration) Parse(input string) error {
	d, err := time.ParseDuration(input)
	if err != nil {
		return err
	}
	*v = Duration(d)
	return nil
}

func (v *Duration) Value() time.Duration {
	return time.Duration(*v)
}
