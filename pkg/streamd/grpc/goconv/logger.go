package goconv

import (
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/streamd/grpc/go/streamd_grpc"
)

func LoggingLevelGRPC2Go(level streamd_grpc.LoggingLevel) logger.Level {
	switch level {
	case streamd_grpc.LoggingLevel_none:
		return logger.LevelUndefined
	case streamd_grpc.LoggingLevel_fatal:
		return logger.LevelFatal
	case streamd_grpc.LoggingLevel_panic:
		return logger.LevelPanic
	case streamd_grpc.LoggingLevel_error:
		return logger.LevelError
	case streamd_grpc.LoggingLevel_warning:
		return logger.LevelWarning
	case streamd_grpc.LoggingLevel_info:
		return logger.LevelInfo
	case streamd_grpc.LoggingLevel_debug:
		return logger.LevelDebug
	case streamd_grpc.LoggingLevel_trace:
		return logger.LevelTrace
	}
	return logger.LevelUndefined
}

func LoggingLevelGo2GRPC(level logger.Level) streamd_grpc.LoggingLevel {
	switch level {
	case logger.LevelUndefined:
		return streamd_grpc.LoggingLevel_none
	case logger.LevelFatal:
		return streamd_grpc.LoggingLevel_fatal
	case logger.LevelPanic:
		return streamd_grpc.LoggingLevel_panic
	case logger.LevelError:
		return streamd_grpc.LoggingLevel_error
	case logger.LevelWarning:
		return streamd_grpc.LoggingLevel_warning
	case logger.LevelInfo:
		return streamd_grpc.LoggingLevel_info
	case logger.LevelDebug:
		return streamd_grpc.LoggingLevel_debug
	case logger.LevelTrace:
		return streamd_grpc.LoggingLevel_trace
	}
	return streamd_grpc.LoggingLevel_warning
}
