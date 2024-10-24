package observability

import (
	"context"

	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/pkg/field"
	"github.com/facebookincubator/go-belt/tool/logger"
)

type ctxKeyOnInsecureDebugT struct{}

var ctxKeyOnInsecureDebug ctxKeyOnInsecureDebugT

func OnInsecureDebug(ctx context.Context) context.Context {
	return belt.WithField(ctx, "ctxKeyOnInsecureDebug", ctxKeyOnInsecureDebug)
}

func IsOnInsecureDebug(ctx context.Context) bool {

	_, ok := ctx.Value(ctxKeyOnInsecureDebug).(struct{})
	return ok
}

type RemoveInsecureDebugFilter struct{}

func NewRemoveInsecureDebugFilter() *RemoveInsecureDebugFilter {
	return &RemoveInsecureDebugFilter{}
}

var _ logger.Hook = (*RemoveInsecureDebugFilter)(nil)

func (f *RemoveInsecureDebugFilter) ProcessLogEntry(entry *logger.Entry) bool {
	return entry.Fields.ForEachField(func(f *field.Field) bool {
		return f.Value != ctxKeyOnInsecureDebug
	})
}

func (f *RemoveInsecureDebugFilter) Flush() {}
