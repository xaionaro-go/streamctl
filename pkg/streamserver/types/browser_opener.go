package types

import (
	"context"
)

type BrowserOpener interface {
	OpenURL(ctx context.Context, url string) error
}
