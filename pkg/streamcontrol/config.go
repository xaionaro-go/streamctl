package streamcontrol

import (
	"context"
	"fmt"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/goccy/go-yaml"
)

type PlatformConfig[T any, S StreamProfile] struct {
	Config         T
	StreamProfiles map[string]S
}

type AbstractPlatformConfig = PlatformConfig[any, StreamProfile]

type Config map[string]*AbstractPlatformConfig

func (cfg *Config) UnmarshalYAML(b []byte) error {
	t := map[string]*AbstractPlatformConfig{}
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

func GetPlatformConfig[T any, S any](ctx context.Context, cfg Config, id string) *PlatformConfig[T, S] {
	platCfg, ok := cfg[id]
	if !ok {
		logger.Debugf(ctx, "config '%s' was not found in cfg: %#+v", id, cfg)
		return nil
	}
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

func GetStreamProfiles[S any](streamProfiles map[string]StreamProfile) map[string]S {
	s := make(map[string]S, len(streamProfiles))
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

func InitConfig[T any, S any](cfg Config, id string, platCfg PlatformConfig[T, S]) {
	if _, ok := cfg[id]; ok {
		panic(fmt.Errorf("id '%s' is already registered", id))
	}
	cfg[id] = &PlatformConfig[any, StreamProfile]{
		Config: &platCfg.Config,
	}
}

func ToAbstractStreamProfiles[S StreamProfile](in map[string]S) map[string]StreamProfile {
	m := make(map[string]StreamProfile, len(in))
	for k, v := range in {
		m[k] = v
	}
	return m
}
