package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"io"
	"os"
	"strings"
	"time"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/sasha-s/go-deadlock"
)

type logWriter struct {
	Logger       logger.Logger
	Buffer       bytes.Buffer
	BufferLocker deadlock.Mutex
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

func (l *logWriter) write(b []byte) (int, error) {
	l.BufferLocker.Lock()
	defer l.BufferLocker.Unlock()
	return l.Buffer.Write(b)
}

func (l *logWriter) Write(b []byte) (int, error) {
	isALogRusLine := false
	s := string(b)
	if len(s) > 14 {
		switch {
		case strings.HasPrefix(s, string(hexMustDecode("1b5b33"))):
			isALogRusLine = true
		case strings.HasPrefix(s, `time="`):
			isALogRusLine = true
		}
	}
	if isALogRusLine {
		return os.Stderr.Write(b)
	}
	return l.write(b)
}
