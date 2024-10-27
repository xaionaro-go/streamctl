package streampanel

import (
	"context"
)

type ProgressBar interface {
	SetProgress(v float64)
}

type Update interface {
	Apply(ctx context.Context, pb ProgressBar) error
	Cancel() error
	ReleaseName() string
}

type AutoUpdater interface {
	CheckForUpdates(context.Context) (Update, error)
}
