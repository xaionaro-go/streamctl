package recoder

import (
	"github.com/asticode/go-astiav"

	"github.com/facebookincubator/go-belt/tool/logger"
)

func LogLevelToAstiav(level logger.Level) astiav.LogLevel {
	switch level {
	case logger.LevelUndefined:
		return astiav.LogLevelQuiet
	case logger.LevelPanic:
		return astiav.LogLevelPanic
	case logger.LevelFatal:
		return astiav.LogLevelFatal
	case logger.LevelError:
		return astiav.LogLevelError
	case logger.LevelWarning:
		return astiav.LogLevelWarning
	case logger.LevelInfo:
		return astiav.LogLevelInfo
	case logger.LevelDebug:
		return astiav.LogLevelVerbose
	case logger.LevelTrace:
		return astiav.LogLevelDebug
	}
	return astiav.LogLevelWarning
}

func LogLevelFromAstiav(level astiav.LogLevel) logger.Level {
	switch level {
	case astiav.LogLevelQuiet:
		return logger.LevelUndefined
	case astiav.LogLevelFatal:
		return logger.LevelFatal
	case astiav.LogLevelPanic:
		return logger.LevelPanic
	case astiav.LogLevelError:
		return logger.LevelError
	case astiav.LogLevelWarning:
		return logger.LevelWarning
	case astiav.LogLevelInfo:
		return logger.LevelInfo
	case astiav.LogLevelVerbose:
		return logger.LevelDebug
	case astiav.LogLevelDebug:
		return logger.LevelTrace
	}
	return logger.LevelWarning
}
