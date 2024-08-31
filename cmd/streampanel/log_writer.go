package main

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

	s := func() string {
		return xsync.DoR1(ctx, &l.BufferLocker, func() string {
			s := l.Buffer.String()
			l.Buffer.Reset()
			return s
		})
	}()

	l.Logger.Logf(l.Logger.Level(), "%s", s)
}

func (l *logWriter) Write(b []byte) (int, error) {
	if len(b) == 0 {
		return 0, nil
	}
	ctx := context.TODO()
	return xsync.DoR2(xsync.WithNoLogging(ctx, true), &l.BufferLocker, func() (int, error) {
		return io.MultiWriter(&l.Buffer, os.Stderr).Write(b)
	})
}
