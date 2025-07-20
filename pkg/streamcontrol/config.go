package streamcontrol

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"runtime/debug"

	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/experimental/errmon"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/goccy/go-yaml"
	"github.com/hashicorp/go-multierror"
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
	valueOfHierarchyItem := func(idx int) reflect.Value {
		item := hierarchy[idx]
		v := reflect.ValueOf(&item).Elem()
		if v.Kind() == reflect.Interface {
			v = v.Elem()
		}
		if v.Kind() == reflect.Pointer {
			v = v.Elem()
		}
		return v
	}
	v := valueOfHierarchyItem(len(hierarchy) - 1)
	if v.Kind() == reflect.Interface {
		v = v.Elem()
	}
	if v.Kind() == reflect.Pointer {
		v = v.Elem()
	}
	for i := 0; i < v.NumField(); i++ {
		fv := v.Field(i)
		if !fv.CanSet() {
			continue
		}
		for h := len(hierarchy) - 1; h >= 0; h-- {
			nv := valueOfHierarchyItem(h).Field(i)
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

type PlatformSpecificConfig interface {
	IsInitialized() bool
}

type PlatformConfig[T PlatformSpecificConfig, S StreamProfile] struct {
	Enable         *bool
	Config         T
	StreamProfiles StreamProfiles[S]
	Custom         map[string]any
}

type ProfileName string

func (cfg *PlatformConfig[T, S]) IsInitialized() bool {
	if cfg == nil {
		return false
	}
	return cfg.Config.IsInitialized()
}

func (cfg PlatformConfig[T, S]) GetStreamProfile(name ProfileName) (S, bool) {
	return cfg.StreamProfiles.Get(name)
}

func (cfg *PlatformConfig[T, S]) SetCustomString(key string, value any) bool {
	if cfg == nil {
		return false
	}
	if cfg.Custom == nil {
		cfg.Custom = map[string]any{}
	}
	cfg.Custom[key] = value
	return true
}

func (cfg *PlatformConfig[T, S]) GetCustomString(key string) (string, bool) {
	if cfg == nil {
		return "", false
	}
	if cfg.Custom == nil {
		return "", false
	}
	v, ok := cfg.Custom[key]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	if !ok {
		return "", false
	}
	return s, true
}

type AbstractPlatformConfig = PlatformConfig[PlatformSpecificConfig, AbstractStreamProfile]

type RawMessage json.RawMessage

var _ yaml.BytesUnmarshaler = (*RawMessage)(nil)
var _ yaml.BytesMarshaler = (*RawMessage)(nil)

func (RawMessage) GetParent() (ProfileName, bool) {
	panic(
		"the value is not parsed; don't use the platform config directly, and use function GetPlatformConfig instead",
	)
}
func (RawMessage) GetOrder() int {
	panic(
		"the value is not parsed; don't use the platform config directly, and use function GetPlatformConfig instead",
	)
}
func (RawMessage) IsInitialized() bool {
	panic(
		"the value is not parsed; don't use the platform config directly, and use function GetPlatformConfig instead",
	)
}

func (m *RawMessage) UnmarshalJSON(b []byte) error {
	return (*json.RawMessage)(m).UnmarshalJSON(b)
}

func (m *RawMessage) UnmarshalYAML(b []byte) error {
	if m == nil {
		return fmt.Errorf("a nil receiver")
	}
	*m = append((*m)[0:0], b...)
	return nil
}

func (m RawMessage) MarshalJSON() ([]byte, error) {
	v := map[string]any{}
	err := yaml.Unmarshal(m, &v)
	if err != nil {
		return nil, fmt.Errorf("unable to unmarshal YAML: %w", err)
	}
	return json.Marshal(v)
}

func (m RawMessage) MarshalYAML() ([]byte, error) {
	if m == nil {
		return []byte("null"), nil
	}
	return m, nil
}

type unparsedPlatformConfig = PlatformConfig[RawMessage, RawMessage]

type PlatformName string

type Config map[PlatformName]*AbstractPlatformConfig

var _ yaml.BytesUnmarshaler = (*Config)(nil)

func ptr[T any](in T) *T {
	return &in
}

func (cfg Config) Convert() error {
	var result *multierror.Error
	for k := range cfg {
		result = multierror.Append(result, ConvertPlatformConfigInplace(cfg, k))
	}
	return result.ErrorOrNil()
}

func (cfg *Config) UnmarshalYAML(b []byte) (_err error) {
	if cfg == nil {
		return fmt.Errorf("cfg is nil")
	}
	ctx := context.TODO()
	defer func() {
		r := recover()
		if r != nil {
			_err = fmt.Errorf("got a panic: %v", r)
			ctx = belt.WithField(ctx, "stack_trace", string(debug.Stack()))
			errmon.ObserveRecoverCtx(ctx, r)
		}
	}()

	t := map[PlatformName]*unparsedPlatformConfig{}
	err := yaml.Unmarshal(b, &t)
	if err != nil {
		return fmt.Errorf(
			"unable to unmarshal YAML of the root of the config: %w; config: <%s>",
			err,
			b,
		)
	}

	if *cfg == nil {
		*cfg = make(Config)
	}

	for k, v := range t {
		if v == nil {
			continue
		}
		vOrig := (*cfg)[k]
		if vOrig == nil {
			(*cfg)[k] = &PlatformConfig[PlatformSpecificConfig, AbstractStreamProfile]{
				Config:         &RawMessage{},
				StreamProfiles: make(StreamProfiles[AbstractStreamProfile]),
				Custom:         map[string]any{},
			}
			vOrig = (*cfg)[k]
		} else {
			if (*cfg)[k].StreamProfiles == nil {
				(*cfg)[k].StreamProfiles = make(StreamProfiles[AbstractStreamProfile])
			}
		}

		(*cfg)[k].Enable = v.Enable
		(*cfg)[k].Custom = v.Custom
		if (*cfg)[k].Enable == nil {
			(*cfg)[k].Enable = ptr(true)
		}

		cfgCfg := vOrig.Config

		err = yaml.Unmarshal(v.Config, cfgCfg)
		if err != nil {
			return fmt.Errorf("unable to unmarshal YAML of platform-config %s: %w", b, err)
		}
		(*cfg)[k].Config = cfgCfg
		for platName := range (*cfg)[k].StreamProfiles {
			delete((*cfg)[k].StreamProfiles, platName)
		}
		for platName, v := range v.StreamProfiles {
			dst := (*cfg)[k].StreamProfiles
			dst[platName] = v
		}
	}

	for k := range *cfg {
		_, ok := t[k]
		if !ok {
			delete(*cfg, k)
		}
	}

	return nil
}

func GetPlatformConfig[T PlatformSpecificConfig, S StreamProfile](
	ctx context.Context,
	cfg Config,
	id PlatformName,
) *PlatformConfig[T, S] {
	platCfg, ok := cfg[id]
	if !ok {
		logger.Debugf(ctx, "config '%s' was not found in cfg: %#+v", id, cfg)
		return nil
	}

	return ConvertPlatformConfig[T, S](ctx, platCfg)
}

func ToAbstractPlatformConfig[T PlatformSpecificConfig, S StreamProfile](
	ctx context.Context,
	platCfg *PlatformConfig[T, S],
) *AbstractPlatformConfig {
	return &AbstractPlatformConfig{
		Enable:         platCfg.Enable,
		Config:         platCfg.Config,
		StreamProfiles: ToAbstractStreamProfiles[S](platCfg.StreamProfiles),
		Custom:         platCfg.Custom,
	}
}

func ConvertPlatformConfig[T PlatformSpecificConfig, S StreamProfile](
	ctx context.Context,
	platCfg *AbstractPlatformConfig,
) *PlatformConfig[T, S] {
	if platCfg == nil {
		platCfg = &AbstractPlatformConfig{}
	}
	return &PlatformConfig[T, S]{
		Enable:         platCfg.Enable,
		Config:         GetPlatformSpecificConfig[T](ctx, platCfg.Config),
		StreamProfiles: GetStreamProfiles[S](platCfg.StreamProfiles),
		Custom:         platCfg.Custom,
	}
}

func GetPlatformSpecificConfig[T PlatformSpecificConfig](
	ctx context.Context,
	platCfgCfg any,
) T {
	if platCfgCfg == nil {
		var zeroValue T
		return zeroValue
	}
	switch platCfgCfg := platCfgCfg.(type) {
	case T:
		return platCfgCfg
	case *T:
		return *platCfgCfg
	case RawMessage:
		var v T
		err := yaml.Unmarshal(platCfgCfg, &v)
		if err != nil {
			panic(err)
		}
		return v
	case *RawMessage:
		var v T
		err := yaml.Unmarshal(*platCfgCfg, &v)
		if err != nil {
			panic(err)
		}
		return v
	default:
		var zeroValue T
		panic(fmt.Errorf("unable to get the config: expected type '%T' or RawMessage, but received type '%T'", zeroValue, platCfgCfg))
	}
}

func GetStreamProfiles[S StreamProfile](
	streamProfiles map[ProfileName]AbstractStreamProfile,
) StreamProfiles[S] {
	s := make(map[ProfileName]S, len(streamProfiles))
	for k, p := range streamProfiles {
		switch p := p.(type) {
		case S:
			s[k] = p
		case RawMessage:
			var v S
			if err := json.Unmarshal(p, &v); err != nil {
				panic(err)
			}
			s[k] = v
		default:
			panic(fmt.Errorf("do not know how to convert type %T", p))
		}
	}
	return s
}

func InitConfig[T PlatformSpecificConfig, S StreamProfile](cfg Config, id PlatformName, platCfg PlatformConfig[T, S]) {
	if _, ok := cfg[id]; ok {
		panic(fmt.Errorf("id '%s' is already registered", id))
	}
	cfg[id] = &PlatformConfig[PlatformSpecificConfig, AbstractStreamProfile]{
		Config: platCfg.Config,
		Custom: map[string]any{},
	}
}

func ToAbstractStreamProfiles[S StreamProfile](
	in map[ProfileName]S,
) map[ProfileName]AbstractStreamProfile {
	m := make(map[ProfileName]AbstractStreamProfile, len(in))
	for k, v := range in {
		m[k] = v
	}
	return m
}
