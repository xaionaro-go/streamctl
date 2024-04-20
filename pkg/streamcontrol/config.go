package streamcontrol

import (
	"context"
	"fmt"
	"reflect"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/goccy/go-yaml"
)

type StreamProfiles[S StreamProfile] map[ProfileName]S

func (profiles StreamProfiles[S]) Get(name ProfileName) (S, bool) {
	profile, ok := profiles[name]
	if !ok {
		return profile, false
	}

	var hierarchy []S
	hierarchy = append(hierarchy, profile)

	for {
		parentName, ok := profile.GetParent()
		if !ok {
			break
		}

		parentProfile, ok := profiles[parentName]
		if !ok {
			break
		}

		hierarchy = append(hierarchy, parentProfile)
	}

	result := hierarchy[len(hierarchy)-1]
	v := reflect.ValueOf(&result).Elem()
	for i := 0; i < v.NumField(); i++ {
		fv := v.Field(i)
		if !fv.CanSet() {
			continue
		}
		for h := len(hierarchy) - 1; h >= 0; h-- {
			nv := reflect.ValueOf(hierarchy[h]).Field(i)
			if isNil(nv) {
				continue
			}
			fv.Set(nv)
		}
	}
	return result, true
}

func isNil(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Ptr, reflect.Slice, reflect.Map, reflect.Func, reflect.Interface:
		return v.IsNil()
	default:
		return false
	}
}

type PlatformConfig[T any, S StreamProfile] struct {
	Config         T
	StreamProfiles StreamProfiles[S]
}

type ProfileName string

func (cfg PlatformConfig[T, S]) GetStreamProfile(name ProfileName) (S, bool) {
	return cfg.StreamProfiles.Get(name)
}

type AbstractPlatformConfig = PlatformConfig[any, AbstractStreamProfile]

type PlatformName string

type Config map[PlatformName]*AbstractPlatformConfig

func (cfg *Config) UnmarshalYAML(b []byte) error {
	t := map[PlatformName]*AbstractPlatformConfig{}
	err := yaml.Unmarshal(b, &t)
	if err != nil {
		return fmt.Errorf("unable to unmarshal YAML of the root of the config: %w", err)
	}

	for k, v := range t {
		b, err := yaml.Marshal(v.Config)
		if err != nil {
			return fmt.Errorf("unable to re-marshal YAML of config %#+v: %w", v, err)
		}

		vOrig, ok := (*cfg)[k]
		if !ok {
			continue
		}

		cfgCfg := vOrig.Config

		err = yaml.Unmarshal(b, cfgCfg)
		if err != nil {
			return fmt.Errorf("unable to unmarshal YAML of platform-config %s: %w", b, err)
		}
		(*cfg)[k].Config = cfgCfg
		(*cfg)[k].StreamProfiles = v.StreamProfiles
	}

	for k := range *cfg {
		_, ok := t[k]
		if !ok {
			delete(*cfg, k)
		}
	}

	return nil
}

func GetPlatformConfig[T any, S StreamProfile](
	ctx context.Context,
	cfg Config,
	id PlatformName,
) *PlatformConfig[T, S] {
	platCfg, ok := cfg[id]
	if !ok {
		logger.Debugf(ctx, "config '%s' was not found in cfg: %#+v", id, cfg)
		return nil
	}

	return ConvertPlatformConfig[T, S](ctx, platCfg, id)
}

func ConvertPlatformConfig[T any, S StreamProfile](
	ctx context.Context,
	platCfg *AbstractPlatformConfig,
	id PlatformName,
) *PlatformConfig[T, S] {
	platCfgCfg, ok := platCfg.Config.(*T)
	if !ok {
		var zeroValue T
		logger.Errorf(ctx, "unable to get the config: expected type '%T', but received type '%T'", zeroValue, platCfg.Config)
		return nil
	}

	return &PlatformConfig[T, S]{
		Config:         *platCfgCfg,
		StreamProfiles: GetStreamProfiles[S](platCfg.StreamProfiles),
	}
}

func GetStreamProfiles[S StreamProfile](streamProfiles map[ProfileName]AbstractStreamProfile) StreamProfiles[S] {
	s := make(map[ProfileName]S, len(streamProfiles))
	for k, pI := range streamProfiles {
		p, ok := pI.(S)
		if !ok {
			var zeroS S
			panic(fmt.Errorf("expected type %T, but received type %T", zeroS, pI))
		}
		s[k] = p
	}
	return s
}

func InitConfig[T any, S StreamProfile](cfg Config, id PlatformName, platCfg PlatformConfig[T, S]) {
	if _, ok := cfg[id]; ok {
		panic(fmt.Errorf("id '%s' is already registered", id))
	}
	cfg[id] = &PlatformConfig[any, AbstractStreamProfile]{
		Config: &platCfg.Config,
	}
}

func ToAbstractStreamProfiles[S StreamProfile](in map[ProfileName]S) map[ProfileName]AbstractStreamProfile {
	m := make(map[ProfileName]AbstractStreamProfile, len(in))
	for k, v := range in {
		m[k] = v
	}
	return m
}
