package observability

import (
	"context"

	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/pkg/field"
	logger "github.com/facebookincubator/go-belt/tool/logger/types"
	"github.com/xaionaro-go/streamctl/pkg/xsync"
)

var LogLevelFilter LogLevelFilterT

type LogLevelFilterT struct {
	Locker xsync.Mutex
	Level  logger.Level
}

var _ logger.PreHook = (*LogLevelFilterT)(nil)

func (h *LogLevelFilterT) GetLevel() logger.Level {
	ctx := xsync.WithNoLogging(context.TODO(), true)
	return xsync.DoR1(ctx, &h.Locker, func() logger.Level {
		return h.Level
	})
}

func (h *LogLevelFilterT) SetLevel(
	level logger.Level,
) {
	ctx := xsync.WithNoLogging(context.TODO(), true)
	h.Locker.Do(ctx, func() {
		h.Level = level
	})
}

func (h *LogLevelFilterT) ProcessInput(
	traceIDs belt.TraceIDs,
	level logger.Level,
	args ...any,
) logger.PreHookResult {
	ctx := xsync.WithNoLogging(context.TODO(), true)
	return xsync.DoR1(ctx, &h.Locker, func() logger.PreHookResult {
		if level > h.Level {
			return logger.PreHookResult{Skip: true}
		}
		return logger.PreHookResult{}
	})
}
func (h *LogLevelFilterT) ProcessInputf(
	traceIDs belt.TraceIDs,
	level logger.Level,
	format string,
	args ...any,
) logger.PreHookResult {
	ctx := xsync.WithNoLogging(context.TODO(), true)
	return xsync.DoR1(ctx, &h.Locker, func() logger.PreHookResult {
		if level > h.Level {
			return logger.PreHookResult{Skip: true}
		}
		return logger.PreHookResult{}
	})
}
func (h *LogLevelFilterT) ProcessInputFields(
	traceIDs belt.TraceIDs,
	level logger.Level,
	message string,
	fields field.AbstractFields,
) logger.PreHookResult {
	ctx := xsync.WithNoLogging(context.TODO(), true)
	return xsync.DoR1(ctx, &h.Locker, func() logger.PreHookResult {
		if level > h.Level {
			return logger.PreHookResult{Skip: true}
		}
		return logger.PreHookResult{}
	})
}
