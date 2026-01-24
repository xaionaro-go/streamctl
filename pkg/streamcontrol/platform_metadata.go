package streamcontrol

import (
	"context"
	"reflect"
)

type platformMetadata struct {
	AccountConfig          reflect.Type
	StreamProfile          reflect.Type
	StreamStatusCustomData reflect.Type
	Config                 reflect.Type
	MaxTitleLength         int
	InitConfig             func(Config)
	NewAccountController   func(
		context.Context,
		AccountID,
		Config,
		func(Config) error,
	) (AbstractAccount, error)
}
