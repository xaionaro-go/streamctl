package server

import (
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/recoder/libav/saferecoder/grpc/go/recoder_grpc"
)

func logLevelProtobuf2Go(logLevel recoder_grpc.LoggingLevel) logger.Level {
	switch logLevel {
	case recoder_grpc.LoggingLevel_LoggingLevelNone:
		return logger.LevelFatal
	case recoder_grpc.LoggingLevel_LoggingLevelFatal:
		return logger.LevelFatal
	case recoder_grpc.LoggingLevel_LoggingLevelPanic:
		return logger.LevelPanic
	case recoder_grpc.LoggingLevel_LoggingLevelError:
		return logger.LevelError
	case recoder_grpc.LoggingLevel_LoggingLevelWarn:
		return logger.LevelWarning
	case recoder_grpc.LoggingLevel_LoggingLevelInfo:
		return logger.LevelInfo
	case recoder_grpc.LoggingLevel_LoggingLevelDebug:
		return logger.LevelDebug
	case recoder_grpc.LoggingLevel_LoggingLevelTrace:
		return logger.LevelTrace
	default:
		return logger.LevelUndefined
	}
}
