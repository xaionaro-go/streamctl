package astiavlogger

import (
	"strings"
	"sync"

	"github.com/asticode/go-astiav"
	logger "github.com/facebookincubator/go-belt/tool/logger/types"
	"github.com/xaionaro-go/recoder/libav/recoder"
)

func Callback(l logger.Logger) astiav.LogCallback {
	astiavLogger, setClassFunc := WrapLogger(l)
	var astiavLoggerLocker sync.Mutex
	return func(c astiav.Classer, level astiav.LogLevel, format, msg string) {
		astiavLoggerLocker.Lock()
		defer astiavLoggerLocker.Unlock()
		setClassFunc(c)
		astiavLogger.Logf(
			recoder.LogLevelFromAstiav(level),
			"%s", strings.TrimSpace(msg),
		)
		setClassFunc(nil)
	}
}
