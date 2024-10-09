package client

import (
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/implementations/libav/saferecoder/grpc/go/recoder_grpc"
)

func logLevelGo2Protobuf(logLevel logger.Level) recoder_grpc.LoggingLevel {
	switch logLevel {
	case logger.LevelFatal:
		return recoder_grpc.LoggingLevel_LoggingLevelFatal
	case logger.LevelPanic:
		return recoder_grpc.LoggingLevel_LoggingLevelPanic
	case logger.LevelError:
		return recoder_grpc.LoggingLevel_LoggingLevelError
	case logger.LevelWarning:
		return recoder_grpc.LoggingLevel_LoggingLevelWarn
	case logger.LevelInfo:
		return recoder_grpc.LoggingLevel_LoggingLevelInfo
	case logger.LevelDebug:
		return recoder_grpc.LoggingLevel_LoggingLevelDebug
	case logger.LevelTrace:
		return recoder_grpc.LoggingLevel_LoggingLevelTrace
	default:
		return recoder_grpc.LoggingLevel_LoggingLevelWarn
	}
}
