package mainprocess

import (
	"context"
	"encoding/gob"
	"fmt"
	"testing"
	"time"

	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/facebookincubator/go-belt/tool/logger/implementation/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testMessage struct {
	Data string
}

func init() {
	gob.Register(testMessage{})
}

func TestMainProcessSecurity(t *testing.T) {
	m, err := NewManager(nil, "child")
	require.NoError(t, err)
	defer m.Close()

	require.NotEmpty(t, m.Password())
	require.Len(t, m.Password(), 16)
	require.Contains(t, m.Addr().String(), "127.0.0.1")
}

func TestMainProcessCoordination(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	received := make(chan string, 1)
	m, err := NewManager(nil, "child")
	require.NoError(t, err)
	defer m.Close()

	go m.Serve(ctx, func(ctx context.Context, source ProcessName, content any) error {
		if msg, ok := content.(testMessage); ok {
			received <- msg.Data
		}
		return nil
	})

	c, err := NewClient("child", m.Addr().String(), m.Password())
	require.NoError(t, err)
	defer c.Close()

	go c.Serve(ctx, func(ctx context.Context, source ProcessName, content any) error {
		return nil
	})

	err = c.SendMessage(ctx, ProcessNameMain, testMessage{Data: "hello"})
	require.NoError(t, err)

	select {
	case data := <-received:
		require.Equal(t, "hello", data)
	case <-ctx.Done():
		t.Fatal("timed out waiting for message")
	}
}

func TestMainProcessSystemConnectivity(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	m, err := NewManager(nil, "child1", "child2")
	require.NoError(t, err)
	defer m.Close()

	go m.Serve(ctx, func(ctx context.Context, source ProcessName, content any) error {
		return nil
	})

	// Launch child 1
	c1, err := NewClient("child1", m.Addr().String(), m.Password())
	require.NoError(t, err)
	go c1.Serve(ctx, func(ctx context.Context, source ProcessName, content any) error { return nil })
	c1.SendMessage(ctx, ProcessNameMain, MessageReady{})

	// Launch child 2
	c2, err := NewClient("child2", m.Addr().String(), m.Password())
	require.NoError(t, err)
	go c2.Serve(ctx, func(ctx context.Context, source ProcessName, content any) error { return nil })
	c2.SendMessage(ctx, ProcessNameMain, MessageReady{})

	err = m.VerifyEverybodyConnected(ctx)
	require.NoError(t, err)
}
func TestMainProcess(t *testing.T) {
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
		switch content := content.(type) {
		case messageContent:
			count := callCount[procName]
			count++
			callCount[procName] = count
			assert.Equal(t, count, content.Integer, procName)
			assert.Equal(t, fmt.Sprint(count), content.String, procName)
			var oldCh chan struct{}
			oldCh, handleCallHappened[procName] = handleCallHappened[procName], make(chan struct{})
			close(oldCh)
		case MessageReadyConfirmed:
		default:
			t.Errorf("unexpected message type: %T", content)
		}
	}

	m, err := NewManager(
		nil,
		"child0", "child1",
	)
	require.NoError(t, err)
	defer m.Close()
	go m.Serve(
		belt.WithField(ctx, "process", ProcessNameMain),
		func(ctx context.Context, source ProcessName, content any) error {
			handleCall("main", content)
			return nil
		},
	)

	c0, err := NewClient("child0", m.Addr().String(), m.Password())
	require.NoError(t, err)
	defer c0.Close()
	go c0.Serve(
		belt.WithField(ctx, "process", "child0"),
		func(ctx context.Context, source ProcessName, content any) error {
			handleCall("child0", content)
			return nil
		},
	)
	c0.SendMessage(ctx, "main", MessageReady{
		ReadyForMessages: []any{messageContent{}},
	})

	c1, err := NewClient("child1", m.Addr().String(), m.Password())
	require.NoError(t, err)
	defer c1.Close()
	go c1.Serve(
		belt.WithField(ctx, "process", "child1"),
		func(ctx context.Context, source ProcessName, content any) error {
			handleCall("child1", content)
			return nil
		},
	)
	c1.SendMessage(ctx, "main", MessageReady{
		ReadyForMessages: []any{messageContent{}},
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
