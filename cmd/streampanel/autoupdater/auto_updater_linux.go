package autoupdater

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"

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
) error {
	logger.Debugf(ctx, "updating the application on Linux")

	b, err := io.ReadAll(artifact)
	if err != nil {
		return fmt.Errorf("unable to read the artifact: %w", err)
	}
	me := os.Args[0]

	tmpPath := me + "-new"
	if err = os.WriteFile(tmpPath, b, 0755); err != nil {
		return fmt.Errorf("unable to write to file '%s': %w", tmpPath, err)
	}

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

	logger.Infof(ctx, "re-running the application")
	cmd := exec.Command(os.Args[0], os.Args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("unable to restart the application: %w", err)
	}

	if u.CloseAppFunc != nil {
		logger.Debugf(ctx, "CloseAppFunc")
		u.CloseAppFunc()
	}
	return nil
}
