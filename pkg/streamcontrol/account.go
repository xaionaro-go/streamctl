package streamcontrol

import (
	"context"
)

func NewAccount(
	ctx context.Context,
	platformID PlatformID,
	accountID AccountID,
	cfg Config,
	saveCfgFn func(Config) error,
) (AbstractAccount, error) {
	f := registry[platformID].NewAccountController
	if f == nil {
		return nil, nil
	}
	return f(ctx, accountID, cfg, saveCfgFn)
}
