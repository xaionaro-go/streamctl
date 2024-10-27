package main

import (
	"context"
	"encoding/gob"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	child_process_manager "github.com/AgustinSRG/go-child-process-manager"
	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/logwriter"
	"github.com/xaionaro-go/streamctl/pkg/mainprocess"
	"github.com/xaionaro-go/streamctl/pkg/observability"
	"github.com/xaionaro-go/streamctl/pkg/xsync"
)

type ProcessName = mainprocess.ProcessName

const (
	ProcessNameMain    = mainprocess.ProcessNameMain
	ProcessNameStreamd = ProcessName("streamd")
	ProcessNameUI      = ProcessName("ui")
)

var forkLocker xsync.Mutex
var forkMap = map[ProcessName]*exec.Cmd{}

func getFork(procName ProcessName) *exec.Cmd {
	ctx := context.TODO()
	return xsync.DoR1(ctx, &forkLocker, func() *exec.Cmd {
		return forkMap[procName]
	})
}

func setFork(procName ProcessName, f *exec.Cmd) {
	ctx := context.TODO()
	forkLocker.Do(ctx, func() {
		forkMap[procName] = f
	})
}

func init() {
	gob.Register(MessageApplicationQuit{})
	gob.Register(MessageApplicationPrepareForUpgrade{})
	gob.Register(MessageApplicationRestart{})
	gob.Register(MessageProcessDie{})
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
	preCtx context.Context,
	subprocessFlag string,
) {
	parts := strings.SplitN(subprocessFlag, ":", 2)
	if len(parts) != 2 {
		logger.Panicf(
			belt.WithField(preCtx, "process", ""),
			"expected 2 parts in --subprocess: name and address, separated via a colon",
		)
	}
	procName := ProcessName(parts[0])
	addr := parts[1]

	preCtx = belt.WithField(preCtx, "process", procName)
	logger.Debugf(preCtx, "process name is %s", procName)

	switch procName {
	case ProcessNameStreamd:
		forkStreamd(preCtx, addr, os.Getenv(EnvPassword))
	case ProcessNameUI:
		forkUI(preCtx, addr, os.Getenv(EnvPassword))
	}
}

func runSplitProcesses(
	ctx context.Context,
	cancelFunc context.CancelFunc,
	flags Flags,
) {
	ctx = belt.WithField(ctx, "process", ProcessNameMain)
	signalsChan := mainProcessSignalHandler(ctx, cancelFunc)

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
						logger.Errorf(
							ctx,
							"failed to send a StreamDDied message to '%s': %v",
							ProcessNameUI,
							err,
						)
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
	observability.Go(ctx, func() {
		m.Serve(ctx, func(ctx context.Context, source ProcessName, content any) error {
			switch content.(type) {
			case GetFlags:
				msg := GetFlagsResult{
					Flags: flags,
				}
				err := m.SendMessagePreReady(ctx, source, msg)
				if err != nil {
					logger.Errorf(ctx, "failed to send message %#+v to '%s': %v", msg, source, err)
				}
			case MessageApplicationQuit:
				signalsChan <- os.Interrupt
			case MessageApplicationPrepareForUpgrade:
				executablePathBeforeUpdate = getExecutablePath()
				logger.Debugf(ctx, "executablePathBeforeUpdate = %s", executablePathBeforeUpdate)
			case MessageApplicationRestart:
				killChildProcess(ctx, m, ProcessNameStreamd)
				killChildProcess(ctx, m, ProcessNameUI)
			}
			return nil
		})
	})
	observability.Go(ctx, func() {
		select {
		case <-ctx.Done():
			return
		case <-time.After(3 * time.Second):
		}

		err := m.VerifyEverybodyConnected(ctx)
		if err != nil {
			logger.Fatalf(ctx, "%s", err)
		}
	})

	<-ctx.Done()
}

const debugDontFork = false

func killChildProcess(
	ctx context.Context,
	m *mainprocess.Manager,
	processName ProcessName,
) {
	err := m.SendMessagePreReady(ctx, processName, MessageProcessDie{})
	if err != nil {
		logger.Errorf(ctx, "failed to send message MessageProcessDie to '%s': %v", processName, err)
		forceKill(ctx, processName)
	}
}

func forceKill(
	ctx context.Context,
	processName ProcessName,
) {
	proc := getFork(processName)
	err := proc.Process.Kill()
	if err != nil {
		logger.Errorf(ctx, "unable to kill the child process '%s': %w", string(processName), err)
	}
}

var executablePathBeforeUpdate string

func getExecutablePath() (_ret string) {
	ctx := context.TODO()

	defer func() { logger.Debugf(ctx, "getExecutablePath: %v", _ret) }()
	if executablePathBeforeUpdate != "" {
		return executablePathBeforeUpdate
	}

	execPath, err := os.Executable()
	if err != nil {
		logger.Errorf(ctx, "unable to get the path of the executable: %v", err)
		return os.Args[0]
	}

	return execPath
}

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

	execPath := getExecutablePath()

	ctx, cancelFn := context.WithCancel(ctx)
	os.Setenv(EnvPassword, password)
	args := []string{
		execPath,
		"--sentry-dsn=" + flags.SentryDSN,
		"--log-level=" + logger.Level(flags.LoggerLevel).String(),
		"--subprocess=" + string(procName) + ":" + addr,
		"--logstash-addr=" + flags.LogstashAddr,
	}
	logger.Infof(ctx, "running '%s %s'", args[0], strings.Join(args[1:], " "))
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stderr = logwriter.NewLogWriter(
		ctx,
		logger.FromCtx(ctx).
			WithField("log_writer_target", "split").
			WithField("output_type", "stderr"),
	)
	cmd.Stdout = logwriter.NewLogWriter(
		ctx,
		logger.FromCtx(ctx).WithField("log_writer_target", "split"),
	)
	cmd.Stdin = os.Stdin
	err := child_process_manager.ConfigureCommand(cmd)
	if err != nil {
		logger.Errorf(ctx, "unable to configure the command %v to be auto-killed", err)
	}
	setFork(procName, cmd)
	err = cmd.Start()
	if err != nil {
		cancelFn()
		return fmt.Errorf("unable to start '%s %s': %w", args[0], strings.Join(args[1:], " "), err)
	}
	err = child_process_manager.AddChildProcess(cmd.Process)
	if err != nil {
		logger.Errorf(ctx, "unable to register the command %v to be auto-killed", err)
	}
	observability.Go(ctx, func() {
		err := cmd.Wait()
		cancelFn()
		if err != nil {
			logger.Errorf(
				ctx,
				"error running '%s %s': %v",
				args[0],
				strings.Join(args[1:], " "),
				err,
			)
		}
	})
	return nil
}

func fakeFork(ctx context.Context, procName ProcessName, addr, password string) error {
	switch procName {
	case ProcessNameStreamd:
		observability.Go(ctx, func() { forkUI(ctx, addr, password) })
		return nil
	case ProcessNameUI:
		observability.Go(ctx, func() { forkStreamd(ctx, addr, password) })
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
				return fmt.Errorf(
					"got unexpected type '%T' instead of %T",
					content,
					mainprocess.MessageReadyConfirmed{},
				)
			}
			return nil
		},
	)
	assertNoError(err)
}
