package observability

import (
	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/pkg/field"
	logger "github.com/facebookincubator/go-belt/tool/logger/types"
	"github.com/sasha-s/go-deadlock"
)

var LogLevelFilter LogLevelFilterT

type LogLevelFilterT struct {
	Locker deadlock.Mutex
	Level  logger.Level
}

var _ logger.PreHook = (*LogLevelFilterT)(nil)

func (h *LogLevelFilterT) GetLevel() logger.Level {
	h.Locker.Lock()
	defer h.Locker.Unlock()
	return h.Level
}

func (h *LogLevelFilterT) SetLevel(
	level logger.Level,
) {
	h.Locker.Lock()
	defer h.Locker.Unlock()
	h.Level = level
}

func (h *LogLevelFilterT) ProcessInput(
	traceIDs belt.TraceIDs,
	level logger.Level,
	args ...any,
) logger.PreHookResult {
	h.Locker.Lock()
	defer h.Locker.Unlock()
	if level > h.Level {
		return logger.PreHookResult{Skip: true}
	}
	return logger.PreHookResult{}
}
func (h *LogLevelFilterT) ProcessInputf(
	traceIDs belt.TraceIDs,
	level logger.Level,
	format string,
	args ...any,
) logger.PreHookResult {
	h.Locker.Lock()
	defer h.Locker.Unlock()
	if level > h.Level {
		return logger.PreHookResult{Skip: true}
	}
	return logger.PreHookResult{}
}
func (h *LogLevelFilterT) ProcessInputFields(
	traceIDs belt.TraceIDs,
	level logger.Level,
	message string,
	fields field.AbstractFields,
) logger.PreHookResult {
	h.Locker.Lock()
	defer h.Locker.Unlock()
	if level > h.Level {
		return logger.PreHookResult{Skip: true}
	}
	return logger.PreHookResult{}
}
