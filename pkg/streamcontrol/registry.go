package streamcontrol

import (
	"fmt"
	"reflect"

	"github.com/goccy/go-yaml"
)

type platformMeta struct {
	PlatformSpecificConfig reflect.Type
	StreamProfile          reflect.Type
	Config                 reflect.Type
}

var registry = map[PlatformName]platformMeta{}

func RegisterPlatform[PSC PlatformSpecificConfig, SP StreamProfile](id PlatformName) {
	var (
		platCfgSample PSC
		profileSample SP
		configSample  PlatformConfig[PSC, SP]
	)

	if _, ok := registry[id]; ok {
		panic(fmt.Errorf("platform '%s' is already registered", id))
	}

	registry[id] = platformMeta{
		PlatformSpecificConfig: reflect.TypeOf(platCfgSample),
		StreamProfile:          reflect.TypeOf(profileSample),
		Config:                 reflect.TypeOf(configSample),
	}
}

func IsInitialized(
	cfg Config,
	platID PlatformName,
) bool {
	meta := registry[platID]
	platCfgCfgTyped := reflect.New(meta.PlatformSpecificConfig).Interface().(PlatformSpecificConfig)

	platCfg := cfg[platID]
	var b []byte
	switch platCfgCfg := platCfg.Config.(type) {
	case RawMessage:
		b = platCfgCfg
	case *RawMessage:
		b = *platCfgCfg
	case PlatformSpecificConfig:
		return platCfgCfg.IsInitialized()
	case nil:
		return false
	default:
		panic(fmt.Errorf("unable to get the config: expected type '%T' or RawMessage, but received type '%T'", platCfgCfgTyped, platCfgCfg))
	}

	err := yaml.Unmarshal(b, platCfgCfgTyped)
	if err != nil {
		panic(err)
	}

	return platCfgCfgTyped.IsInitialized()
}
