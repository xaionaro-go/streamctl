//go:build windows
// +build windows

package autoupdater

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/klauspost/compress/zip"
	"github.com/xaionaro-go/streamctl/pkg/autoupdater"
)

const (
	Available = true

	assetName    = "streampanel-windows-amd64.zip"
	dirInArchive = "streampanel-windows-amd64"
)

func (u *AutoUpdater) Update(
	ctx context.Context,
	updateInfo *autoupdater.Update,
	artifact io.Reader,
	progressBar autoupdater.ProgressBar,
) error {
	progressBar.SetProgress(0)
	logger.Debugf(ctx, "updating the application on Windows")

	zipBytes, err := io.ReadAll(artifact)
	if err != nil {
		return fmt.Errorf("unable to read the artifact: %w", err)
	}
	progressBar.SetProgress(0.2)

	zipReader, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	if err != nil {
		return fmt.Errorf("unable to open the artifact as a zip file: %w", err)
	}

	me := os.Args[0]
	myDir := path.Dir(me)

	numFiles := len(zipReader.File)
	for idx, f := range zipReader.File {
		progressBar.SetProgress(0.2 + (float64(idx)/float64(numFiles))*0.7)
		if !strings.HasPrefix(f.Name, dirInArchive) {
			return fmt.Errorf("a file with an unexpected path in the zip archive: '%s'", f.Name)
		}

		relPath, err := filepath.Rel(dirInArchive, f.Name)
		if err != nil {
			return fmt.Errorf("unable to get a relative path of '%s': %w", f.Name, err)
		}
		fileName := path.Base(relPath)
		filePath := path.Join(myDir, relPath)
		if f.FileInfo().IsDir() {
			err := os.MkdirAll(filePath, 0755)
			if err != nil {
				return fmt.Errorf("cannot create directory '%s': %w", filePath, err)
			}
			continue
		}

		tmpPath := filePath + "-new"
		fileContentReader, err := f.Open()
		if err != nil {
			return fmt.Errorf("unable to start decompressing file '%s': %w", filePath, err)
		}
		var fileContent []byte
		func() {
			defer fileContentReader.Close()
			fileContent, err = io.ReadAll(fileContentReader)
		}()
		if err != nil {
			return fmt.Errorf("unable to decompress file '%s': %w", filePath, err)
		}

		if err := os.WriteFile(tmpPath, fileContent, f.Mode()); err != nil {
			return fmt.Errorf("unable to write file '%s': %w", filePath, err)
		}

		backupPath := filePath + "-old"
		if err := os.Rename(filePath, backupPath); err != nil {
			return fmt.Errorf("unable to rename '%s' to '%s': %w", filePath, backupPath, err)
		}

		if err := os.Rename(tmpPath, filePath); err != nil {
			return fmt.Errorf("unable to rename '%s' to '%s': %w", tmpPath, filePath, err)
		}

		if err := os.Remove(backupPath); err != nil {
			if strings.HasSuffix(fileName, ".exe") {
				logger.Debugf(ctx, "unable to remove file '%s': %v", backupPath, err)
				continue
			}
			return fmt.Errorf("unable to remove file '%s': %w", backupPath, err)
		}
	}
	progressBar.SetProgress(0.9)

	logger.Infof(ctx, "re-running the application")
	cmd := exec.Command(os.Args[0], os.Args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("unable to restart the application: %w", err)
	}
	progressBar.SetProgress(1)

	if u.CloseAppFunc != nil {
		logger.Debugf(ctx, "CloseAppFunc")
		u.CloseAppFunc()
	}

	return nil
}
