package observability

import (
	"time"

	"github.com/facebookincubator/go-belt/pkg/field"
	xlogrus "github.com/facebookincubator/go-belt/tool/logger/implementation/logrus"
	logger "github.com/facebookincubator/go-belt/tool/logger/types"
	"github.com/sasha-s/go-deadlock"
	"github.com/sirupsen/logrus"
)

type HookAdapter struct {
	Locker       deadlock.Mutex
	LogrusLogger *logrus.Logger
	LogrusHook   logrus.Hook
}

func NewHookAdapter(
	l *logrus.Logger,
	h logrus.Hook,
) logger.Hook {
	return &HookAdapter{
		LogrusLogger: l,
		LogrusHook:   h,
	}
}

func (h *HookAdapter) ProcessLogEntry(entry *logger.Entry) bool {
	fields := logrus.Fields{}
	entry.Fields.ForEachField(func(f *field.Field) bool {
		fields[f.Key] = f.Value
		return true
	})
	h.LogrusHook.Fire(&logrus.Entry{
		Logger:  h.LogrusLogger,
		Data:    fields,
		Time:    entry.Timestamp,
		Level:   xlogrus.LevelToLogrus(entry.Level),
		Caller:  entry.Caller.Frame(),
		Message: entry.Message,
		Buffer:  nil,
	})
	return true
}

type Flusher interface {
	Flush()
}

type FlusherErr interface {
	Flush() error
}

type FlusherDeadlined interface {
	Flush(timeout time.Duration)
}

type FlusherDeadlinedErr interface {
	Flush(timeout time.Duration) error
}

func (h *HookAdapter) Flush() {
	timeout := 5 * time.Second
	switch flusher := h.LogrusHook.(type) {
	case Flusher:
		flusher.Flush()
	case FlusherErr:
		flusher.Flush()
	case FlusherDeadlined:
		flusher.Flush(timeout)
	case FlusherDeadlinedErr:
		flusher.Flush(timeout)
	}
}
