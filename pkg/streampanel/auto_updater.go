package streampanel

import (
	"context"
)

type Update interface {
	Apply(ctx context.Context) error
	Cancel() error
	ReleaseName() string
}

type AutoUpdater interface {
	CheckForUpdates(context.Context) (Update, error)
}
