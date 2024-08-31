package main

import (
	"bytes"
	"context"
	"encoding/hex"
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

func hexMustDecode(s string) []byte {
	b, err := hex.DecodeString(s)
	assertNoError(err)
	return b
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
	s := func() string {
		l.BufferLocker.Lock()
		defer l.BufferLocker.Unlock()
		s := l.Buffer.String()
		l.Buffer.Reset()
		return s
	}()

	l.Logger.Logf(l.Logger.Level(), "%s", s)
}

func (l *logWriter) Write(b []byte) (int, error) {
	l.BufferLocker.Lock()
	defer l.BufferLocker.Unlock()
	return io.MultiWriter(&l.Buffer, os.Stderr).Write(b)
}
