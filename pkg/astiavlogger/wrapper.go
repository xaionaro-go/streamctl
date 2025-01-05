package astiavlogger

import (
	"github.com/asticode/go-astiav"
	"github.com/facebookincubator/go-belt/tool/logger/implementation/logrus"
	logger "github.com/facebookincubator/go-belt/tool/logger/types"
)

func WrapLogger(logger logger.Logger) (logger.Logger, func(astiav.Classer)) {
	switch logger.Emitter().(type) {
	case *logrus.Emitter:
		return wrapperLogrusLogger(logger)
	}
	return logger, func(c astiav.Classer) {}
}
