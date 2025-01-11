package logwriter

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/xsync"
)

type logWriter struct {
	Logger       logger.Logger
	Buffer       bytes.Buffer
	BufferLocker xsync.Mutex
}

var _ io.Writer = (*logWriter)(nil)

func NewLogWriter(
	ctx context.Context,
	logger logger.Logger,
) *logWriter {
	l := &logWriter{
		Logger: logger,
	}
	go l.flusher(ctx)
	return l
}

func (l *logWriter) flusher(ctx context.Context) {
	logger.Tracef(ctx, "flusher()")
	defer logger.Tracef(ctx, "/flusher()")

	defer l.Flush()

	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		l.Flush()
	}
}

func (l *logWriter) Flush() {
	ctx := context.TODO()

	s := xsync.DoR1(ctx, &l.BufferLocker, func() string {
		s := l.Buffer.String()
		l.Buffer.Reset()
		return s
	})
	if len(s) == 0 {
		return
	}

	l.Logger.Logf(observability.LogLevelFilter.Level, "%s", s)
}

var logrusPrefix = []byte{0x1b, 0x5b}

func (l *logWriter) Write(b []byte) (int, error) {
	ctx := context.TODO()
	return xsync.DoR2(xsync.WithNoLogging(ctx, true), &l.BufferLocker, func() (int, error) {
		switch {
		case bytes.HasPrefix(b, logrusPrefix):
			return os.Stderr.Write(b)
		default:
			if len(bytes.Trim(b, " \n\t\r")) == 0 {
				return io.Discard.Write(b)
			}
			_, err := l.Buffer.Write(b)
			if err != nil {
				os.Stderr.WriteString(fmt.Sprintf("unable to write to the logger: %v", err))
			}
			return len(b), nil
		}
	})
}
