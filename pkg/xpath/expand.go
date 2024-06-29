package xpath

import (
	"fmt"
	"os"
	"path"
	"runtime"
	"strings"
)

func Expand(rawPath string) (string, error) {
	switch {
	case strings.HasPrefix(rawPath, "~/"):
		var homeDir string
		switch runtime.GOOS {
		case "android":
			homeDir = "/data/user/0/center.dx.streampanel/files"
		default:
			var err error
			homeDir, err = os.UserHomeDir()
			if err != nil {
				return "", fmt.Errorf("unable to get user home dir: %w", err)
			}
		}
		return path.Join(homeDir, rawPath[2:]), nil
	}
	return rawPath, nil
}
