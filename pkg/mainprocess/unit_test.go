package mainprocess

import (
	"context"
	"encoding/gob"
	"fmt"
	"testing"

	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/facebookincubator/go-belt/tool/logger/implementation/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test(t *testing.T) {
	l := logrus.Default().WithLevel(logger.LevelTrace)
	logger.Default = func() logger.Logger {
		return l
	}
	ctx := logger.CtxWithLogger(context.Background(), l)
	defer belt.Flush(ctx)
	ctx, cancelFunc := context.WithCancel(ctx)
	defer cancelFunc()

	type messageContent struct {
		Integer int
		String  string
	}
	gob.Register(messageContent{})

	handleCallHappened := map[string]chan struct{}{
		"main":   make(chan struct{}),
		"child0": make(chan struct{}),
		"child1": make(chan struct{}),
	}
	callCount := map[string]int{}

	handleCall := func(procName string, content any) {
		logger.Tracef(ctx, "handleCall('%s', %#+v)", procName, content)
		count := callCount[procName]
		count++
		callCount[procName] = count
		msg := content.(messageContent)
		assert.Equal(t, count, msg.Integer, procName)
		assert.Equal(t, fmt.Sprint(count), msg.String, procName)
		var oldCh chan struct{}
		oldCh, handleCallHappened[procName] = handleCallHappened[procName], make(chan struct{})
		close(oldCh)
	}

	m, err := NewManager(
		nil,
		"child0", "child1",
	)
	require.NoError(t, err)
	defer m.Close()
	go m.Serve(
		belt.WithField(ctx, "process", "main"),
		func(ctx context.Context, source ProcessName, content any) error {
			handleCall("main", content)
			return nil
		},
	)

	c0, err := NewClient("child0", m.Addr().String(), m.Password())
	require.NoError(t, err)
	defer c0.Close()
	go c0.Serve(belt.WithField(ctx, "process", "child0"), func(ctx context.Context, source ProcessName, content any) error {
		handleCall("child0", content)
		return nil
	})

	c1, err := NewClient("child1", m.Addr().String(), m.Password())
	require.NoError(t, err)
	defer c1.Close()
	go c1.Serve(belt.WithField(ctx, "process", "child1"), func(ctx context.Context, source ProcessName, content any) error {
		handleCall("child1", content)
		return nil
	})

	_, err = NewClient("child2", m.Addr().String(), m.Password())
	require.Error(t, err)

	waitCh0 := handleCallHappened["main"]
	waitCh1 := handleCallHappened["child1"]
	err = c0.SendMessage(ctx, "", messageContent{Integer: 1, String: "1"})
	require.NoError(t, err)
	<-waitCh0
	<-waitCh1

	waitCh0 = handleCallHappened["main"]
	err = c1.SendMessage(ctx, "main", messageContent{Integer: 2, String: "2"})
	require.NoError(t, err)
	<-waitCh0

	waitCh0 = handleCallHappened["child0"]
	err = c1.SendMessage(ctx, "child0", messageContent{Integer: 1, String: "1"})
	require.NoError(t, err)
	<-waitCh0

	require.Equal(t, 2, callCount["main"])
	require.Equal(t, 1, callCount["child0"])
	require.Equal(t, 1, callCount["child1"])
}
