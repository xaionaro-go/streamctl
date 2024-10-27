package autoupdater

import (
	"context"

	"github.com/xaionaro-go/streamctl/pkg/autoupdater"
	"github.com/xaionaro-go/streamctl/pkg/streampanel"
)

type Update struct {
	Update *autoupdater.Update
}

var _ streampanel.Update = (*Update)(nil)

func (u *Update) Apply(ctx context.Context) error {
	return u.Update.Apply(ctx)
}

func (u *Update) Cancel() error {
	return nil
}

func (u *Update) ReleaseName() string {
	return u.Update.Release.GetName()
}
