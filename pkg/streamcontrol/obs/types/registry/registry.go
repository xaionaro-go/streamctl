package registry

import (
	"reflect"
	"sort"

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
	return reflect.ValueOf(v).Type().Elem()
}

func ToTypeName[T any](sample T) string {
	return strcase.ToSnake(typeOf(sample).Name())
}

func (r *Registry[T]) RegisterType(sample T) {
	r.Types[ToTypeName(sample)] = typeOf(sample)
}

func (r *Registry[T]) NewByTypeName(typeName string) T {
	return reflect.New(r.Types[typeName]).Interface().(T)
}

func (r *Registry[T]) ListTypeNames() []string {
	result := make([]string, 0, len(r.Types))
	for k := range r.Types {
		result = append(result, k)
	}
	sort.Strings(result)
	return result
}
