// Package subproclog wires the logger for streamd's internal worker
// subprocesses (chat-listener, translator). Centralised so every spawn
// type uses the same logrus-backed setup and the same logstash-hook
// rules — avoiding drift between subprocess kinds.
package subproclog

import (
	"context"
	"fmt"
	"os"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/facebookincubator/go-belt/tool/logger/implementation/logrus"
	"github.com/xaionaro-go/observability"
)

// Setup installs a logrus-backed logger gated by observability.LogLevelFilter
// in ctx, and (when logstashAddr is non-empty) attaches a logstash hook
// tagged with appName so child logs reach the same sink as the parent's.
//
// Logrus is required because observability.CtxWithLogstash type-asserts the
// in-context logger's emitter to *logrus.Emitter and logs an error and
// returns the context unmodified for any other emitter type — a zap-backed
// logger would defeat the optional logstash hookup entirely.
//
// rawLogLevel is typically the value of the parent-forwarded log-level flag;
// empty rawLogLevel falls back to LevelInfo. Empty logstashAddr disables
// logstash forwarding for this child. appName is the identifier reported to
// logstash (e.g. "streamd-chat-listener", "streamd-translator").
func Setup(
	ctx context.Context,
	rawLogLevel string,
	logstashAddr string,
	appName string,
) context.Context {
	level := logger.LevelInfo
	if rawLogLevel != "" {
		if err := level.Set(rawLogLevel); err != nil {
			fmt.Fprintf(os.Stderr,
				"%s: invalid log-level %q: %v; using %s\n",
				appName, rawLogLevel, err, level)
		}
	}

	l := logrus.Default().WithLevel(logger.LevelTrace)
	observability.LogLevelFilter.SetLevel(level)
	l = l.WithPreHooks(&observability.LogLevelFilter)
	logger.Default = func() logger.Logger { return l }
	ctx = logger.CtxWithLogger(ctx, l)

	if logstashAddr != "" {
		ctx = observability.CtxWithLogstash(ctx, logstashAddr, appName)
	}
	return ctx
}
