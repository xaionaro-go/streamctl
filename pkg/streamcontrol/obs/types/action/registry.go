package action

import (
	registrylib "github.com/xaionaro-go/streamctl/pkg/streamcontrol/obs/types/registry"
)

var registry = registrylib.New[Action]()

func NewByTypeName(typeName string) Action {
	return registry.NewByTypeName(typeName)
}

func ListTypeNames() []string {
	return registry.ListTypeNames()
}
