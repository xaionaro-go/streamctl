package observability

import (
	"context"
	"strings"

	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/pkg/field"
	"github.com/facebookincubator/go-belt/pkg/runtime"
	loggertypes "github.com/facebookincubator/go-belt/tool/logger/types"
	"github.com/xaionaro-go/streamctl/pkg/xsync"
)

var LogLevelFilter LogLevelFilterT

type LogLevelFilterT struct {
	Locker xsync.Mutex
	Level  loggertypes.Level
}

var _ loggertypes.PreHook = (*LogLevelFilterT)(nil)

func (h *LogLevelFilterT) GetLevel() loggertypes.Level {
	ctx := xsync.WithNoLogging(context.TODO(), true)
	return xsync.DoR1(ctx, &h.Locker, func() loggertypes.Level {
		return h.Level
	})
}

func (h *LogLevelFilterT) SetLevel(
	level loggertypes.Level,
) {
	ctx := xsync.WithNoLogging(context.TODO(), true)
	h.Locker.Do(ctx, func() {
		h.Level = level
	})
}

func (h *LogLevelFilterT) ProcessInput(
	traceIDs belt.TraceIDs,
	level loggertypes.Level,
	args ...any,
) loggertypes.PreHookResult {
	ctx := xsync.WithNoLogging(context.TODO(), true)
	return xsync.DoR1(ctx, &h.Locker, func() loggertypes.PreHookResult {
		if !h.shouldLog(level) {
			return loggertypes.PreHookResult{Skip: true}
		}
		return loggertypes.PreHookResult{}
	})
}
func (h *LogLevelFilterT) ProcessInputf(
	traceIDs belt.TraceIDs,
	level loggertypes.Level,
	format string,
	args ...any,
) loggertypes.PreHookResult {
	ctx := xsync.WithNoLogging(context.TODO(), true)
	return xsync.DoR1(ctx, &h.Locker, func() loggertypes.PreHookResult {
		if !h.shouldLog(level) {
			return loggertypes.PreHookResult{Skip: true}
		}
		return loggertypes.PreHookResult{}
	})
}
func (h *LogLevelFilterT) ProcessInputFields(
	traceIDs belt.TraceIDs,
	level loggertypes.Level,
	message string,
	fields field.AbstractFields,
) loggertypes.PreHookResult {
	ctx := xsync.WithNoLogging(context.TODO(), true)
	return xsync.DoR1(ctx, &h.Locker, func() loggertypes.PreHookResult {
		if !h.shouldLog(level) {
			return loggertypes.PreHookResult{Skip: true}
		}
		return loggertypes.PreHookResult{}
	})
}

func (h *LogLevelFilterT) shouldLog(level loggertypes.Level) bool {
	if level > h.Level {
		return false
	}

	pcs := runtime.CallerStackTrace(runtime.DefaultCallerPCFilter)
	for _, pc := range pcs {
		file, _ := pc.FileLine()
		if strings.Contains(file, "xaionaro-go/kickcom") {
			if level == h.Level {
				return false
			}
		}
	}

	return true
}
