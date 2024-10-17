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

func typeOf(v any) reflect.Type {
	return reflect.Indirect(reflect.ValueOf(v)).Type()
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
	r.Types[ToTypeName(sample)] = typeOf(sample)
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
