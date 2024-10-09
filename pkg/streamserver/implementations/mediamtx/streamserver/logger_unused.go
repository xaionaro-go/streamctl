package streamserver

import (
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/mediamtx/pkg/conf"
	mediamtxlogger "github.com/xaionaro-go/mediamtx/pkg/logger"
)

func fromConfLoggerLevel(level conf.LogLevel) logger.Level {
	return fromMediamtxLoggerLevel(mediamtxlogger.Level(level))
}
