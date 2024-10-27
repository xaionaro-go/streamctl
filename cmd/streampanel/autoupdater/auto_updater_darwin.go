//go:build darwin

package autoupdater

import (
	"context"
	"fmt"
	"io"

	"github.com/xaionaro-go/streamctl/pkg/autoupdater"
)

const (
	Available = false

	assetName = ""
)

func (u *AutoUpdater) Update(
	ctx context.Context,
	updateInfo *autoupdater.Update,
	artifact io.Reader,
	progressBar autoupdater.ProgressBar,
) error {
	return fmt.Errorf("auto update is not supported for this platform")
}
