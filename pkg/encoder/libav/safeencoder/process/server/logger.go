package server

import (
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/encoder/libav/safeencoder/grpc/go/encoder_grpc"
)

func logLevelProtobuf2Go(logLevel encoder_grpc.LoggingLevel) logger.Level {
	switch logLevel {
	case encoder_grpc.LoggingLevel_LoggingLevelNone:
		return logger.LevelFatal
	case encoder_grpc.LoggingLevel_LoggingLevelFatal:
		return logger.LevelFatal
	case encoder_grpc.LoggingLevel_LoggingLevelPanic:
		return logger.LevelPanic
	case encoder_grpc.LoggingLevel_LoggingLevelError:
		return logger.LevelError
	case encoder_grpc.LoggingLevel_LoggingLevelWarn:
		return logger.LevelWarning
	case encoder_grpc.LoggingLevel_LoggingLevelInfo:
		return logger.LevelInfo
	case encoder_grpc.LoggingLevel_LoggingLevelDebug:
		return logger.LevelDebug
	case encoder_grpc.LoggingLevel_LoggingLevelTrace:
		return logger.LevelTrace
	default:
		return logger.LevelUndefined
	}
}
