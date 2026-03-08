package streamcontrol

import "context"

type AbstractPlatformConfig = PlatformConfig[RawMessage, RawMessage]

func ToAbstractPlatformConfig[AC AccountConfigGeneric[SP], SP StreamProfile](
	ctx context.Context,
	platCfg *PlatformConfig[AC, SP],
) *AbstractPlatformConfig {
	if platCfg == nil {
		return nil
	}
	accounts := make(map[AccountID]RawMessage)
	if platCfg.Accounts != nil {
		for k, v := range platCfg.Accounts {
			if k == "" {
				continue
			}
			accounts[k] = ToAbstractAccountConfig(ctx, &v)
		}
	}
	return &AbstractPlatformConfig{
		Accounts: accounts,
		Custom:   platCfg.Custom,
	}
}

func ConvertPlatformConfig[AC AccountConfigGeneric[SP], SP StreamProfile](
	ctx context.Context,
	platCfg *AbstractPlatformConfig,
) *PlatformConfig[AC, SP] {
	if platCfg == nil {
		platCfg = &AbstractPlatformConfig{}
	}
	var accounts map[AccountID]AC
	if platCfg.Accounts != nil {
		accounts = make(map[AccountID]AC)
		for k, v := range platCfg.Accounts {
			if k == "" {
				continue
			}
			accounts[k] = GetAccountConfig[AC](ctx, v)
		}
	}
	return &PlatformConfig[AC, SP]{
		Accounts: accounts,
		Custom:   platCfg.Custom,
	}
}
