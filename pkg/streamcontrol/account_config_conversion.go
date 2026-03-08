package streamcontrol

import (
	"context"
	"fmt"

	"github.com/goccy/go-yaml"
)

func GetAccountConfig[
	AC AccountConfigGeneric[SP],
	SP StreamProfile,
](
	ctx context.Context,
	platCfgCfg any,
) AC {
	if platCfgCfg == nil {
		var zeroValue AC
		return zeroValue
	}
	switch platCfgCfg := platCfgCfg.(type) {
	case AC:
		return platCfgCfg
	case *AC:
		return *platCfgCfg
	case RawMessage:
		var v AC
		err := yaml.Unmarshal(platCfgCfg, &v)
		if err != nil {
			panic(err)
		}
		return v
	case *RawMessage:
		var v AC
		err := yaml.Unmarshal(*platCfgCfg, &v)
		if err != nil {
			panic(err)
		}
		return v
	default:
		var zeroValue AC
		panic(fmt.Errorf("unable to get the config: expected type '%T' or RawMessage, but received type '%T'", zeroValue, platCfgCfg))
	}
}

func ToAbstractAccounts[
	AC AccountConfigGeneric[SP],
	SP StreamProfile,
](
	accounts map[AccountID]AC,
) map[AccountID]RawMessage {
	m := make(map[AccountID]RawMessage, len(accounts))
	for k, v := range accounts {
		if k == "" {
			continue
		}
		m[k] = ToAbstractAccountConfig(context.Background(), &v)
	}
	return m
}
