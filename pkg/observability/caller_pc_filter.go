package observability

import (
	"runtime"
	"strings"
)

func CallerPCFilter(
	originalPCFilter func(uintptr) bool,
) func(uintptr) bool {
	return func(pc uintptr) bool {
		if !originalPCFilter(pc) {
			return false
		}
		fn := runtime.FuncForPC(pc)
		funcName := fn.Name()
		switch {
		case strings.Contains(funcName, "pkg/xsync"):
			return false
		}
		file, _ := fn.FileLine(pc)
		switch {
		case strings.Contains(file, "context.go"):
			return false
		case strings.Contains(file, "log_writer.go"):
			return false
		case strings.Contains(file, "logger.go"):
			return false
		case strings.Contains(file, "runtime.go"):
			return false
		}
		return true
	}
}
