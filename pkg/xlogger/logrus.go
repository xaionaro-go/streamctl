package xlogger

import (
	"context"

	"github.com/facebookincubator/go-belt/tool/logger"
	xlogrus "github.com/facebookincubator/go-belt/tool/logger/implementation/logrus"
	"github.com/sirupsen/logrus"
)

type LogrusFieldLogger struct {
	logger.Logger
}

func LogrusFieldLoggerFromCtx(ctx context.Context) logrus.FieldLogger {
	return LogrusFieldLogger{Logger: logger.FromCtx(ctx)}
}

func (l LogrusFieldLogger) WithField(key string, value any) *logrus.Entry {
	return l.getLogrusEntry().WithField(key, value)
}

func (l LogrusFieldLogger) WithFields(fields logrus.Fields) *logrus.Entry {
	return l.getLogrusEntry().WithFields(fields)
}

func (l LogrusFieldLogger) getLogrusEntry() *logrus.Entry {
	return l.Logger.Emitter().(*xlogrus.Emitter).LogrusEntry
}

func (l LogrusFieldLogger) WithError(err error) *logrus.Entry {
	return l.getLogrusEntry().WithError(err)
}

func (l LogrusFieldLogger) Debugln(args ...any) {
	l.Debug(args...)
}
func (l LogrusFieldLogger) Printf(format string, args ...any) {
	l.Debugf(format, args...)
}
func (l LogrusFieldLogger) Print(args ...any) {
	l.Debug(args...)
}
func (l LogrusFieldLogger) Println(args ...any) {
	l.Debug(args...)
}
func (l LogrusFieldLogger) Infoln(args ...any) {
	l.Info(args...)
}
func (l LogrusFieldLogger) Warning(args ...any) {
	l.Warn(args...)
}
func (l LogrusFieldLogger) Warnln(args ...any) {
	l.Warn(args...)
}
func (l LogrusFieldLogger) Warningln(args ...any) {
	l.Warn(args...)
}
func (l LogrusFieldLogger) Warningf(format string, args ...any) {
	l.Warnf(format, args...)
}
func (l LogrusFieldLogger) Errorln(args ...any) {
	l.Error(args...)
}
func (l LogrusFieldLogger) Panicln(args ...any) {
	l.Panic(args...)
}
func (l LogrusFieldLogger) Fatalln(args ...any) {
	l.Fatal(args...)
}
