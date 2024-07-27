package main

import (
	"context"
	"encoding/gob"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/mainprocess"
)

type ProcessName = mainprocess.ProcessName

const (
	ProcessNameMain    = mainprocess.ProcessNameMain
	ProcessNameStreamd = ProcessName("streamd")
	ProcessNameUI      = ProcessName("ui")
)

var forkLocker sync.Mutex
var forkMap = map[ProcessName]*exec.Cmd{}

func getFork(procName ProcessName) *exec.Cmd {
	forkLocker.Lock()
	defer forkLocker.Unlock()
	return forkMap[procName]
}

func setFork(procName ProcessName, f *exec.Cmd) {
	forkLocker.Lock()
	defer forkLocker.Unlock()
	forkMap[procName] = f
}

func init() {
	gob.Register(MessageQuit{})
	gob.Register(StreamDDied{})
	gob.Register(GetFlags{})
	gob.Register(GetFlagsResult{})
	gob.Register(GetStreamdAddress{})
	gob.Register(GetStreamdAddressResult{})
	gob.Register(UpdateStreamDConfig{})
}

const (
	EnvPassword = "STREAMPANEL_PASSWORD"
)

func runSubprocess(
	ctx context.Context,
	subprocessFlag string,
) {
	parts := strings.SplitN(subprocessFlag, ":", 2)
	if len(parts) != 2 {
		logger.Fatalf(ctx, "expected 2 parts in --subprocess: name and address, separated via a colon")
	}
	procName := ProcessName(parts[0])
	addr := parts[1]
	switch procName {
	case ProcessNameStreamd:
		forkStreamd(ctx, addr, os.Getenv(EnvPassword))
	case ProcessNameUI:
		forkUI(ctx, addr, os.Getenv(EnvPassword))
	}
}

func runSplitProcesses(
	ctx context.Context,
	flags Flags,
) {
	ctx = belt.WithField(ctx, "process", "main")
	signalsChan := signalHandler(ctx)

	procList := []ProcessName{
		ProcessNameUI,
	}
	if flags.RemoteAddr == "" {
		procList = append(procList, ProcessNameStreamd)
	}

	var m *mainprocess.Manager
	var err error
	m, err = mainprocess.NewManager(
		func(
			ctx context.Context,
			procName ProcessName,
			addr string,
			password string,
			isRestart bool,
		) error {

			f := getFork(procName)
			if f != nil {
				time.Sleep(time.Millisecond * 100)
				f.Process.Kill()
				logger.Debugf(ctx, "waiting for process '%s' to die", procName)
				f.Wait()
			}

			logger.Debugf(ctx, "running process '%s'", procName)
			err := runFork(ctx, flags, procName, addr, password)
			if err != nil {
				panic(err)
			}
			if isRestart && flags.RemoteAddr == "" {
				switch procName {
				case ProcessNameStreamd:
					err := m.SendMessage(ctx, ProcessNameUI, StreamDDied{})
					if err != nil {
						logger.Errorf(ctx, "failed to send a StreamDDied message to '%s': %v", ProcessNameUI, err)
					}
				}
			}
			return nil
		},
		procList...,
	)
	if err != nil {
		logger.Fatalf(ctx, "failed to start process manager: %v", err)
	}
	defer m.Close()
	go m.Serve(ctx, func(ctx context.Context, source ProcessName, content any) error {
		switch content.(type) {
		case GetFlags:
			msg := GetFlagsResult{
				Flags: flags,
			}
			err := m.SendMessagePreReady(ctx, source, msg)
			if err != nil {
				logger.Errorf(ctx, "failed to send message %#+v to '%s': %v", msg, source, err)
			}
		case MessageQuit:
			signalsChan <- os.Interrupt
		}
		return nil
	})
	go func() {
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second):
		}

		err := m.VerifyEverybodyConnected(ctx)
		if err != nil {
			logger.Fatalf(ctx, "%s", err)
		}
	}()

	<-ctx.Done()
}

const debugDontFork = false

func runFork(
	ctx context.Context,
	flags Flags,
	procName ProcessName,
	addr, password string,
) error {
	logger.Debugf(ctx, "running fork: '%s' '%s' '%s'", procName, addr, password)
	defer logger.Debugf(ctx, "/running fork: '%s' '%s' '%s'", procName, addr, password)
	if debugDontFork {
		return fakeFork(ctx, procName, addr, password)
	}

	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("unable to get the path of the executable: %w", err)
	}

	os.Setenv(EnvPassword, password)
	args := []string{execPath, "--sentry-dsn=" + flags.SentryDSN, "--log-level=" + flags.LoggerLevel.String(), "--subprocess=" + string(procName) + ":" + addr}
	logger.Infof(ctx, "running '%s %s'", args[0], strings.Join(args[1:], " "))
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	cmd.Stdin = os.Stdin
	setFork(procName, cmd)
	err = cmd.Start()
	if err != nil {
		return fmt.Errorf("unable to start '%s %s': %w", args[0], strings.Join(args[1:], " "), err)
	}
	go func() {
		err := cmd.Wait()
		if err != nil {
			logger.Errorf(ctx, "error running '%s %s': %v", args[0], strings.Join(args[1:], " "), err)
		}
	}()
	return nil
}

func fakeFork(ctx context.Context, procName ProcessName, addr, password string) error {
	switch procName {
	case ProcessNameStreamd:
		go forkUI(ctx, addr, password)
		return nil
	case ProcessNameUI:
		go forkStreamd(ctx, addr, password)
		return nil
	}
	return fmt.Errorf("unexpected process name: %s", procName)
}

func setReadyFor(
	ctx context.Context,
	mainProcess *mainprocess.Client,
	msgTypes ...any,
) {
	logger.Debugf(ctx, "setReadyFor: %#+v", msgTypes)
	defer logger.Debugf(ctx, "/setReadyFor: %#+v", msgTypes)

	err := mainProcess.SendMessage(ctx, ProcessNameMain, mainprocess.MessageReady{
		ReadyForMessages: msgTypes,
	})
	if err != nil {
		logger.Fatal(ctx, err)
	}

	err = mainProcess.ReadOne(
		ctx,
		func(ctx context.Context, source ProcessName, content any) error {
			_, ok := content.(mainprocess.MessageReadyConfirmed)
			if !ok {
				return fmt.Errorf("got unexpected type '%T' instead of %T", content, mainprocess.MessageReadyConfirmed{})
			}
			return nil
		},
	)
	assertNoError(err)
}
