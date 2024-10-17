package serializable

import (
	registrylib "github.com/xaionaro-go/streamctl/pkg/serializable/registry"
)

var localRegistry = registrylib.New[any]()

func RegisterType[T any]() {
	var sample T
	localRegistry.RegisterType(sample)
}

func NewByTypeName[T any](typeName string) (T, bool) {
	result := localRegistry.NewByTypeName(typeName)
	casted, ok := result.(T)
	return casted, ok
}

func ListTypeNames[T any]() []string {
	names := localRegistry.ListTypeNames()
	var result []string
	for _, name := range names {
		if _, ok := localRegistry.NewByTypeName(name).(T); ok {
			result = append(result, name)
		}
	}
	return result
}
