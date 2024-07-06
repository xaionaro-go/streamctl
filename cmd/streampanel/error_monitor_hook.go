package main

import (
	"bytes"
	"fmt"
	"runtime"

	"github.com/DataDog/gostackparse"
	xruntime "github.com/facebookincubator/go-belt/pkg/runtime"
	errmontypes "github.com/facebookincubator/go-belt/tool/experimental/errmon/types"
	"github.com/facebookincubator/go-belt/tool/logger"
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
}

func (h *ErrorMonitorLoggerHook) ProcessLogEntry(entry *logger.Entry) bool {
	if entry.Level > logger.LevelWarning {
		return true
	}

	goroutines, currentGoroutineID := getGoroutines()
	h.ErrorMonitor.Emitter().Emit(&errmontypes.Event{
		Entry:       *entry,
		ID:          "",
		ExternalIDs: []any{},
		Exception: errmontypes.Exception{
			IsPanic:    entry.Level <= logger.LevelPanic,
			Error:      fmt.Errorf("[%s] %s", entry.Level, entry.Message),
			StackTrace: xruntime.CallerStackTrace(nil),
		},
		CurrentGoroutineID: currentGoroutineID,
		Goroutines:         goroutines,
	})
	return true
}

func (h *ErrorMonitorLoggerHook) Flush() {}
