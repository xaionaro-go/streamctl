package registry

import (
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/iancoleman/strcase"
)

type Registry[T any] struct {
	Types map[string]reflect.Type
}

func New[T any]() *Registry[T] {
	return &Registry[T]{
		Types: map[string]reflect.Type{},
	}
}

func typeOf(in any) reflect.Type {
	v := reflect.ValueOf(in)
	if v.Kind() == reflect.Interface {
		panic("should not support to happen") // or should we just do v = v.Elem() here?
	}
	t := v.Type()
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t
}

func ToTypeName[T any](sample T) string {
	name := typeOf(sample).Name()
	name = strings.ReplaceAll(name, "github.com/xaionaro-go/streamctl/pkg/streamd/config/", "")
	name = strings.ReplaceAll(name, "eventquery/", "")
	name = strings.ReplaceAll(name, "eventquery.", "")
	name = strings.ReplaceAll(name, "event/", "")
	name = strings.ReplaceAll(name, "event.", "")
	name = strings.ReplaceAll(name, "action/", "")
	name = strings.ReplaceAll(name, "action.", "")
	return strcase.ToSnake(name)
}

func (r *Registry[T]) RegisterType(sample T) {
	name := ToTypeName(sample)
	if _, ok := r.Types[name]; ok {
		panic(fmt.Errorf("type '%s' is already registered; cannot register %T", name, sample))
	}
	t := typeOf(sample)
	if t.Kind() == reflect.Interface {
		panic(fmt.Errorf("type '%s' (%T) is an interface; cannot register it", name, sample))
	}
	r.Types[name] = t
}

func (r *Registry[T]) NewByTypeName(typeName string) T {
	t := r.Types[typeName]
	if t == nil {
		panic(fmt.Errorf("type '%s' is not registered", typeName))
	}
	return reflect.New(t).Interface().(T)
}

func (r *Registry[T]) ListTypeNames() []string {
	result := make([]string, 0, len(r.Types))
	for k := range r.Types {
		result = append(result, k)
	}
	sort.Strings(result)
	return result
}
