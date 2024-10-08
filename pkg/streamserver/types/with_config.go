package types

import "context"

type WithConfiger interface {
	WithConfig(
		ctx context.Context,
		callback func(context.Context, *Config),
	)
}
