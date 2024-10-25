package xpath

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
)

func GetExecPath(execPathUnprocessed string) (string, error) {
	execPath, err := exec.LookPath(execPathUnprocessed)
	switch {
	case err == nil:
		return execPath, nil
	case errors.Is(err, exec.ErrDot):
		wd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("unable to get current working directory: %w", err)
		}
		return exec.LookPath(path.Join(wd, execPathUnprocessed))
	default:
		return "", err
	}
}
