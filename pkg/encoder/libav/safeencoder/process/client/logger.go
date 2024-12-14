package client

import (
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/encoder/libav/safeencoder/grpc/go/encoder_grpc"
)

func logLevelGo2Protobuf(logLevel logger.Level) encoder_grpc.LoggingLevel {
	switch logLevel {
	case logger.LevelFatal:
		return encoder_grpc.LoggingLevel_LoggingLevelFatal
	case logger.LevelPanic:
		return encoder_grpc.LoggingLevel_LoggingLevelPanic
	case logger.LevelError:
		return encoder_grpc.LoggingLevel_LoggingLevelError
	case logger.LevelWarning:
		return encoder_grpc.LoggingLevel_LoggingLevelWarn
	case logger.LevelInfo:
		return encoder_grpc.LoggingLevel_LoggingLevelInfo
	case logger.LevelDebug:
		return encoder_grpc.LoggingLevel_LoggingLevelDebug
	case logger.LevelTrace:
		return encoder_grpc.LoggingLevel_LoggingLevelTrace
	default:
		return encoder_grpc.LoggingLevel_LoggingLevelWarn
	}
}
