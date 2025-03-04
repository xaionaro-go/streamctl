//go:build linux && !android
// +build linux,!android

package autoupdater

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/autoupdater"
)

const (
	Available = true

	assetName = "streampanel-linux-amd64"
)

func (u *AutoUpdater) Update(
	ctx context.Context,
	updateInfo *autoupdater.Update,
	artifact io.Reader,
	progressBar autoupdater.ProgressBar,
) error {
	progressBar.SetProgress(0)
	logger.Debugf(ctx, "updating the application on Linux")

	b, err := io.ReadAll(artifact)
	if err != nil {
		return fmt.Errorf("unable to read the artifact: %w", err)
	}
	me := os.Args[0]
	progressBar.SetProgress(0.2)

	if u.BeforeUpgrade != nil {
		logger.Debugf(ctx, "BeforeUpgrade")
		u.BeforeUpgrade()
	}

	tmpPath := me + "-new"
	if err = os.WriteFile(tmpPath, b, 0755); err != nil {
		return fmt.Errorf("unable to write to file '%s': %w", tmpPath, err)
	}
	progressBar.SetProgress(0.8)

	backupFile := me + "-old"
	if err := os.Rename(me, backupFile); err != nil {
		return fmt.Errorf("unable to rename '%s' to '%s': %w", me, backupFile, err)
	}

	if err := os.Rename(tmpPath, me); err != nil {
		return fmt.Errorf("unable to rename '%s' to '%s': %w", tmpPath, me, err)
	}

	if err := os.Remove(backupFile); err != nil {
		logger.Errorf(ctx, "unable to delete the old file '%s': %v", backupFile, err)
	}
	progressBar.SetProgress(0.85)

	if u.AfterUpgrade != nil {
		logger.Debugf(ctx, "CloseAppFunc")
		u.AfterUpgrade()
	}

	progressBar.SetProgress(1)
	return nil
}
