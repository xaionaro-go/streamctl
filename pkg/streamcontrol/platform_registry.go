package streamcontrol

import (
	"context"
	"fmt"
	"reflect"

	"github.com/goccy/go-yaml"
	"github.com/hashicorp/go-multierror"
)

var registry = map[PlatformID]platformMetadata{}

func RegisterPlatform[
	AC AccountConfigGeneric[SP],
	SP StreamProfile,
	SSCD any,
](
	platformID PlatformID,
	maxTitleLength int,
	accountFactory func(
		ctx context.Context,
		cfg AC,
		saveCfgFn func(AC) error,
	) (AccountGeneric[SP], error),
) {
	var (
		platCfgSample AC
		profileSample SP
		sscdSample    SSCD
		configSample  PlatformConfig[AC, SP]
	)

	if _, ok := registry[platformID]; ok {
		panic(fmt.Errorf("platform '%s' is already registered", platformID))
	}

	registry[platformID] = platformMetadata{
		AccountConfig:          reflect.TypeOf(platCfgSample),
		StreamProfile:          reflect.TypeOf(profileSample),
		StreamStatusCustomData: reflect.TypeOf(sscdSample),
		Config:                 reflect.TypeOf(configSample),
		MaxTitleLength:         maxTitleLength,
		InitConfig: func(cfg Config) {
			InitConfig(cfg, platformID, PlatformConfig[AC, SP]{})
		},
		NewAccountController: func(
			ctx context.Context,
			accountID AccountID,
			cfg Config,
			saveCfgFn func(Config) error,
		) (AbstractAccount, error) {
			platCfg := GetPlatformConfig[AC, SP](ctx, cfg, platformID)
			if platCfg == nil {
				return nil, nil
			}
			accountCfg, ok := platCfg.Accounts[accountID]
			if !ok {
				return nil, nil
			}
			account, err := accountFactory(ctx, accountCfg, func(accountCfg AC) error {
				if cfg[platformID] == nil {
					cfg[platformID] = &AbstractPlatformConfig{}
				}
				if cfg[platformID].Accounts == nil {
					cfg[platformID].Accounts = make(map[AccountID]RawMessage)
				}
				cfg[platformID].Accounts[accountID] = ToAbstractAccountConfig(ctx, &accountCfg)
				return saveCfgFn(cfg)
			})
			if err != nil {
				return nil, err
			}
			return ToAbstractAccount(account), nil
		},
	}
}

func GetMaxTitleLength(id PlatformID) int {
	return registry[id].MaxTitleLength
}

func GetPlatformIDs() []PlatformID {
	var result []PlatformID
	for id := range registry {
		result = append(result, id)
	}
	return result
}

func InitializeConfig(cfg Config, id PlatformID) {
	registry[id].InitConfig(cfg)
}

func GetEmptyStreamProfile(platID PlatformID) StreamProfile {
	meta := registry[platID]
	return reflect.New(meta.StreamProfile).Interface().(StreamProfile)
}

func GetStreamStatusCustomDataType(platID PlatformID) reflect.Type {
	return registry[platID].StreamStatusCustomData
}

func NewStreamProfile(platID PlatformID) StreamProfile {
	meta := registry[platID]
	return reflect.New(meta.StreamProfile).Interface().(StreamProfile)
}

func GetEmptyPlatformSpecificConfig(platID PlatformID) any {
	meta := registry[platID]
	return reflect.New(meta.AccountConfig).Interface()
}

func IsInitialized(
	cfg Config,
	platID PlatformID,
) bool {
	platCfg := cfg[platID]
	if platCfg == nil {
		return false
	}
	return platCfg.IsInitialized()
}

func ConvertAccountConfig(
	p AccountConfigGeneric[RawMessage],
	platID PlatformID,
) (AccountConfigCommon, error) {
	meta := registry[platID]
	platCfgCfgTyped := reflect.New(meta.AccountConfig).Interface().(AccountConfigCommon)

	var b []byte
	switch platCfgCfg := p.(type) {
	case nil:
		return nil, nil
	case RawMessage:
		b = platCfgCfg
	case *RawMessage:
		b = *platCfgCfg
	case AccountConfigGeneric[RawMessage]:
		if reflect.TypeOf(p) == meta.AccountConfig || reflect.TypeOf(p) == reflect.PtrTo(meta.AccountConfig) {
			return p, nil
		}
		return p, nil
	default:
		return nil, fmt.Errorf("unable to get the config: expected type '%T' or RawMessage, but received type '%T'", platCfgCfgTyped, p)
	}

	err := yaml.Unmarshal(b, platCfgCfgTyped)
	if err != nil {
		return nil, fmt.Errorf("unable to unmarshal: %w", err)
	}

	return platCfgCfgTyped, nil
}

func ConvertStreamProfiles(
	s StreamProfiles[RawMessage],
	platID PlatformID,
) error {
	var errs *multierror.Error
	for k, v := range s {
		res, err := ConvertStreamProfile(v, platID)
		if err != nil {
			errs = multierror.Append(errs, err)
			continue
		}
		s[k] = ToRawMessage(res)
	}
	return errs.ErrorOrNil()
}

func ConvertStreamProfile(
	p RawMessage,
	platID PlatformID,
) (StreamProfile, error) {
	meta := registry[platID]
	streamProfileTyped := reflect.New(meta.StreamProfile).Interface().(StreamProfile)

	if len(p) == 0 {
		return nil, nil
	}

	err := yaml.Unmarshal(p, streamProfileTyped)
	if err != nil {
		panic(err)
	}

	return streamProfileTyped, nil
}
