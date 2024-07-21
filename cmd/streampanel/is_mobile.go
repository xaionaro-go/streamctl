package main

import (
	"runtime"
)

func isMobile() bool {
	switch runtime.GOOS {
	case "android":
		return true
	}
	return false
}
