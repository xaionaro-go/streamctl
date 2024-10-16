package observability

import (
	"bytes"
	"context"
	"fmt"
	"runtime"

	"github.com/DataDog/gostackparse"
	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/pkg/field"
	xruntime "github.com/facebookincubator/go-belt/pkg/runtime"
	"github.com/facebookincubator/go-belt/tool/experimental/errmon"
	errmontypes "github.com/facebookincubator/go-belt/tool/experimental/errmon/types"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/facebookincubator/go-belt/tool/logger/adapter"
	loggertypes "github.com/facebookincubator/go-belt/tool/logger/types"
)

func getGoroutines() ([]errmontypes.Goroutine, int) {
	// TODO: consider pprof.Lookup("goroutine") instead of runtime.Stack

	// getting all goroutines
	stackBufferSize := 65536 * runtime.NumGoroutine()
	if stackBufferSize > 10*1024*1024 {
		stackBufferSize = 10 * 1024 * 1024
	}
	stackBuffer := make([]byte, stackBufferSize)
	n := runtime.Stack(stackBuffer, true)
	goroutines, errs := gostackparse.Parse(bytes.NewReader(stackBuffer[:n]))
	if len(errs) > 0 { //nolint:staticcheck
		// TODO: do something
	}

	// convert goroutines for the output
	goroutinesConverted := make([]errmontypes.Goroutine, 0, len(goroutines))
	for _, goroutine := range goroutines {
		goroutinesConverted = append(goroutinesConverted, *goroutine)
	}

	// getting current goroutine ID
	n = runtime.Stack(stackBuffer, false)
	currentGoroutines, errs := gostackparse.Parse(bytes.NewReader(stackBuffer[:n]))
	if len(errs) > 0 { //nolint:staticcheck
		// TODO: do something
	}
	var currentGoroutineID int
	switch len(currentGoroutines) {
	case 0:
		// TODO: do something
	case 1:
		currentGoroutineID = currentGoroutines[0].ID
	default:
		// TODO: do something
	}

	return goroutinesConverted, currentGoroutineID
}

type ErrorMonitorLoggerHook struct {
	ErrorMonitor errmontypes.ErrorMonitor
	SendChan     chan ErrorMonitorMessage
}

func NewErrorMonitorLoggerHook(
	errorMonitor errmon.ErrorMonitor,
) *ErrorMonitorLoggerHook {
	result := &ErrorMonitorLoggerHook{
		ErrorMonitor: errorMonitor,
		SendChan:     make(chan ErrorMonitorMessage, 10),
	}
	GoSafe(context.TODO(), result.senderLoop)
	return result
}

var _ loggertypes.PreHook = (*ErrorMonitorLoggerHook)(nil)

func (h *ErrorMonitorLoggerHook) ProcessInput(
	traceIDs belt.TraceIDs,
	level loggertypes.Level,
	args ...any,
) loggertypes.PreHookResult {
	if level > loggertypes.LevelWarning {
		return loggertypes.PreHookResult{
			Skip: false,
		}
	}

	emitter := &mockEmitter{}
	l := adapter.LoggerFromEmitter(emitter).WithLevel(logger.LevelWarning)
	l.Log(level, args...)

	h.sendReport(emitter.LastEntry)

	return loggertypes.PreHookResult{
		Skip: false,
	}
}

func (h *ErrorMonitorLoggerHook) ProcessInputf(
	traceIDs belt.TraceIDs,
	level loggertypes.Level,
	format string,
	args ...any,
) loggertypes.PreHookResult {
	if level > loggertypes.LevelWarning {
		return loggertypes.PreHookResult{
			Skip: false,
		}
	}

	emitter := &mockEmitter{}
	l := adapter.LoggerFromEmitter(emitter).WithLevel(logger.LevelWarning)
	l.Logf(level, format, args...)

	h.sendReport(emitter.LastEntry)

	return loggertypes.PreHookResult{
		Skip: false,
	}
}

func (h *ErrorMonitorLoggerHook) ProcessInputFields(
	traceIDs belt.TraceIDs,
	level loggertypes.Level,
	message string,
	fields field.AbstractFields,
) loggertypes.PreHookResult {
	if level > loggertypes.LevelWarning {
		return loggertypes.PreHookResult{
			Skip: false,
		}
	}

	emitter := &mockEmitter{}
	l := adapter.LoggerFromEmitter(emitter).WithLevel(logger.LevelWarning)
	l.LogFields(level, message, fields)
	h.sendReport(emitter.LastEntry)

	return loggertypes.PreHookResult{
		Skip: false,
	}
}

func copyEntry(entry *loggertypes.Entry) *loggertypes.Entry {
	entryDup := *entry

	if entry.Fields != nil {
		fields := make(field.Fields, 0, entry.Fields.Len())
		entry.Fields.ForEachField(func(f *field.Field) bool {
			fields = append(fields, *f)
			return true
		})
		entryDup.Fields = fields
	}

	return &entryDup
}

type ErrorMonitorMessage struct {
	Entry              *loggertypes.Entry
	Goroutines         []errmontypes.Goroutine
	CurrentGoroutineID int
	StackTrace         xruntime.PCs
}

func (h *ErrorMonitorLoggerHook) sendReport(
	entry *loggertypes.Entry,
) {
	if entry == nil {
		logger.Default().Errorf("an attempt to send through sentry a nil entry")
		return
	}
	logger.Default().Tracef("sending through sentry entry: %#+v", *entry)
	entryDup := copyEntry(entry)
	goroutines, currentGoroutineID := getGoroutines()
	stackTrace := xruntime.CallerStackTrace(nil)
	select {
	case h.SendChan <- ErrorMonitorMessage{
		Entry:              entryDup,
		Goroutines:         goroutines,
		CurrentGoroutineID: currentGoroutineID,
		StackTrace:         stackTrace,
	}:
	default:
		logger.Default().Errorf("unable to send an error to Sentry, the channel is busy")
		return
	}
}

func (h *ErrorMonitorLoggerHook) senderLoop() {
	for {
		message := <-h.SendChan
		h.ErrorMonitor.Emitter().Emit(&errmontypes.Event{
			Entry:       *message.Entry,
			ID:          "",
			ExternalIDs: []any{},
			Exception: errmontypes.Exception{
				IsPanic:    message.Entry.Level <= loggertypes.LevelPanic,
				Error:      fmt.Errorf("[%s] %s", message.Entry.Level, message.Entry.Message),
				StackTrace: message.StackTrace,
			},
			CurrentGoroutineID: message.CurrentGoroutineID,
			Goroutines:         message.Goroutines,
		})
	}
}
