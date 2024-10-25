package xpath

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"

	"github.com/hashicorp/go-multierror"
)

func GetExecPath(
	execPathUnprocessed string,
	tryDirs ...string,
) (string, error) {
	var result *multierror.Error
	for _, relPath := range append([]string{""}, tryDirs...) {
		r, err := getExecPath(execPathUnprocessed, relPath)
		if err == nil {
			return r, nil
		}
		result = multierror.Append(result, fmt.Errorf("unable to locate exec path of '%s' in '%s': %w", execPathUnprocessed, relPath, err))

	}
	return "", result
}

func getExecPath(
	execPathUnprocessed string,
	dir string,
) (string, error) {
	if dir != "" {
		execPathUnprocessed = path.Join(dir, execPathUnprocessed)
	}
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
