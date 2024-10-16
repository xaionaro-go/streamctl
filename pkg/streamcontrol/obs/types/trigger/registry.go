package trigger

import (
	registrylib "github.com/xaionaro-go/streamctl/pkg/streamcontrol/obs/types/registry"
)

var registry = registrylib.New[Query]()

func NewByTypeName(typeName string) Query {
	return registry.NewByTypeName(typeName)
}

func ListQueryTypeNames() []string {
	return registry.ListTypeNames()
}
