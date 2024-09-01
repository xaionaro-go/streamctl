package logwriter

import (
	"bytes"
	"context"
	"io"
	"os"
	"time"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/xsync"
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
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			l.Flush()
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

	l.Logger.Logf(l.Logger.Level(), "%s", s)
}

func (l *logWriter) Write(b []byte) (int, error) {
	bSanitized := bytes.Trim(b, " \n\t\r")
	ctx := context.TODO()
	return xsync.DoR2(xsync.WithNoLogging(ctx, true), &l.BufferLocker, func() (int, error) {
		l.Buffer.Write(append(bSanitized, []byte("\n")...))
		return os.Stderr.Write(b)
	})
}
