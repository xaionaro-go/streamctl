package registry

import (
	"fmt"
	"reflect"
	"sort"

	"github.com/xaionaro-go/streamctl/pkg/audio/types"
)

type PlayerPCMFactory interface {
	NewPlayerPCM() types.PlayerPCM
}

type factoryWithPriority struct {
	Priority int
	PlayerPCMFactory
}

var factoryRegistry = map[reflect.Type]factoryWithPriority{}

func RegisterFactory(
	priority int,
	playerPCMFactory PlayerPCMFactory,
) {
	t := reflect.ValueOf(playerPCMFactory).Type()
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if _, ok := factoryRegistry[t]; ok {
		panic(fmt.Errorf("there is already registered a factory of PlayerPCM of type %v", t))
	}
	factoryRegistry[t] = factoryWithPriority{
		Priority:         priority,
		PlayerPCMFactory: playerPCMFactory,
	}
}

func Factories() []PlayerPCMFactory {
	var factoriesWithPriorities []factoryWithPriority
	for _, factory := range factoryRegistry {
		factoriesWithPriorities = append(factoriesWithPriorities, factory)
	}
	sort.Slice(factoriesWithPriorities, func(i, j int) bool {
		return factoriesWithPriorities[i].Priority < factoriesWithPriorities[j].Priority
	})

	var factories []PlayerPCMFactory
	for _, factory := range factoriesWithPriorities {
		factories = append(factories, factory.PlayerPCMFactory)
	}

	return factories
}
