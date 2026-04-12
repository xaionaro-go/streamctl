package streamcontrol

import (
	"fmt"
	"reflect"

	"github.com/goccy/go-yaml"
	"github.com/hashicorp/go-multierror"
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
	err := ConvertPlatformConfigInplace(cfg, platID)
	if err != nil {
		return false
	}
	platCfg := cfg[platID]
	if platCfg == nil {
		return false
	}
	return platCfg.IsInitialized()
}

func ConvertPlatformConfigInplace(
	cfg Config,
	platID PlatformName,
) error {
	platCfg := cfg[platID]
	if platCfg == nil {
		return nil
	}

	var err error
	platCfg.Config, err = GetAbstractPlatformSpecificConfig(cfg, platID)
	if err != nil {
		return fmt.Errorf("unable to convert the platform specific part of the config: %w", err)
	}

	err = ConvertAbstractStreamProfiles(platCfg.StreamProfiles, platID)
	if err != nil {
		return fmt.Errorf("unable to convert the stream profiles of the config: %w", err)
	}

	return nil
}

func GetAbstractPlatformSpecificConfig(
	cfg Config,
	platID PlatformName,
) (PlatformSpecificConfig, error) {
	meta := registry[platID]
	platCfgCfgTyped := reflect.New(meta.PlatformSpecificConfig).Interface().(PlatformSpecificConfig)

	platCfg := cfg[platID]
	if platCfg == nil {
		return nil, nil
	}

	var b []byte
	switch platCfgCfg := platCfg.Config.(type) {
	case nil:
		return nil, nil
	case RawMessage:
		b = platCfgCfg
	case *RawMessage:
		b = *platCfgCfg
	case PlatformSpecificConfig:
		return platCfgCfg, nil
	default:
		return nil, fmt.Errorf("unable to get the config: expected type '%T' or RawMessage, but received type '%T'", platCfgCfgTyped, platCfgCfg)
	}

	err := yaml.Unmarshal(b, platCfgCfgTyped)
	if err != nil {
		panic(err)
	}

	return platCfgCfgTyped, nil
}

func ConvertAbstractStreamProfiles(
	s StreamProfiles[AbstractStreamProfile],
	platID PlatformName,
) error {
	var errs *multierror.Error
	for k, v := range s {
		var convErr error
		s[k], convErr = ConvertStreamProfile(v, platID)
		errs = multierror.Append(errs, convErr)
	}
	return errs.ErrorOrNil()
}

func ConvertStreamProfile(
	p StreamProfile,
	platID PlatformName,
) (StreamProfile, error) {
	meta := registry[platID]
	streamProfileTyped := reflect.New(meta.StreamProfile).Interface().(StreamProfile)

	var b []byte
	switch p := p.(type) {
	case nil:
		return nil, nil
	case RawMessage:
		b = p
	case *RawMessage:
		b = *p
	case AbstractStreamProfile:
		return p, nil
	default:
		return nil, fmt.Errorf("unable to get the config: expected type '%T' or RawMessage, but received type '%T'", streamProfileTyped, p)
	}

	err := yaml.Unmarshal(b, streamProfileTyped)
	if err != nil {
		panic(err)
	}

	return streamProfileTyped, nil
}
