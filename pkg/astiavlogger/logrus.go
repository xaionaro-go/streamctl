package astiavlogger

import (
	"fmt"
	"runtime"
	"strings"
	"unsafe"

	"github.com/asticode/go-astiav"
	"github.com/facebookincubator/go-belt/tool/logger/adapter"
	beltlogrus "github.com/facebookincubator/go-belt/tool/logger/implementation/logrus"
	logger "github.com/facebookincubator/go-belt/tool/logger/types"
	"github.com/iancoleman/strcase"
	"github.com/sirupsen/logrus"
	"github.com/xaionaro-go/unsafetools"
)

func wrapperLogrusLogger(logger logger.Logger) (logger.Logger, func(astiav.Classer)) {
	emitter := ptr(*logger.Emitter().(*beltlogrus.Emitter))
	logrusEntry := ptr(*emitter.LogrusEntry)
	emitter.LogrusEntry = logrusEntry
	logrusEntry.Logger = ptr(*logrusEntry.Logger)

	var class astiav.Classer
	callerPrettifier := func(f *runtime.Frame) (function string, file string) {
		var chain []string
		if class != nil {
			cl := class.Class()
			for ; cl != nil; cl = cl.Parent() {
				chain = append(chain, fmt.Sprintf(
					"[%s]%s:%s:%p",
					strcase.ToSnake(ClassCategoryToString(cl.Category())),
					cl.Name(),
					cl.ItemName(),
					*unsafetools.FieldByName(cl, "ptr").(*unsafe.Pointer),
				))
			}
		}
		fakeFunc := strings.Join(chain, "->")
		return fakeFunc, "av"
	}
	switch formatter := logrusEntry.Logger.Formatter.(type) {
	case *logrus.TextFormatter:
		formatter = ptr(*formatter)
		logrusEntry.Logger.Formatter = formatter
		formatter.CallerPrettyfier = callerPrettifier
	case *logrus.JSONFormatter:
		formatter = ptr(*formatter)
		logrusEntry.Logger.Formatter = formatter
		formatter.CallerPrettyfier = callerPrettifier
	}
	compactLogger := ptr(*logger.(adapter.GenericSugar).CompactLogger.(*beltlogrus.CompactLogger))
	*unsafetools.FieldByName(compactLogger, "emitter").(**beltlogrus.Emitter) = emitter
	logger = adapter.GenericSugar{
		CompactLogger: compactLogger,
	}

	return logger, func(newClass astiav.Classer) {
		class = newClass
	}
}
