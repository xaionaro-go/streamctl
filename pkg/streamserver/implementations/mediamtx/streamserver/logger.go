package streamserver

import (
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/mediamtx/pkg/conf"
	mediamtxlogger "github.com/xaionaro-go/mediamtx/pkg/logger"
)

type mediamtxLogger struct {
	logger.Logger
}

var _ mediamtxlogger.Writer = (*mediamtxLogger)(nil)

func newMediamtxLogger(l logger.Logger) mediamtxlogger.Writer {
	return mediamtxLogger{
		Logger: l,
	}
}

func (l mediamtxLogger) Log(
	level mediamtxlogger.Level,
	fmt string,
	args ...any,
) {
	l.Logf(
		fromMediamtxLoggerLevel(level),
		fmt,
		args...,
	)
}

func fromMediamtxLoggerLevel(level mediamtxlogger.Level) logger.Level {
	switch level {
	case mediamtxlogger.Debug:
		return logger.LevelDebug
	case mediamtxlogger.Info:
		return logger.LevelInfo
	case mediamtxlogger.Warn:
		return logger.LevelWarning
	case mediamtxlogger.Error:
		return logger.LevelError
	default:
		return logger.LevelInfo
	}
}

func toMediamtxLoggerLevel(level logger.Level) mediamtxlogger.Level {
	switch level {
	case logger.LevelTrace, logger.LevelDebug:
		return mediamtxlogger.Debug
	case logger.LevelInfo:
		return mediamtxlogger.Info
	case logger.LevelWarning:
		return mediamtxlogger.Warn
	case logger.LevelError, logger.LevelPanic, logger.LevelFatal:
		return mediamtxlogger.Error
	default:
		return mediamtxlogger.Info
	}
}

func fromConfLoggerLevel(level conf.LogLevel) logger.Level {
	return fromMediamtxLoggerLevel(mediamtxlogger.Level(level))
}

func toConfLoggerLevel(level logger.Level) conf.LogLevel {
	return conf.LogLevel(toMediamtxLoggerLevel(level))
}
