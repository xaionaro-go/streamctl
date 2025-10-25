package chathandlerobsolete

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"runtime"

	child_process_manager "github.com/AgustinSRG/go-child-process-manager"
	"github.com/facebookincubator/go-belt/tool/experimental/errmon"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/kick/chathandlerobsolete/client"
	"github.com/xaionaro-go/xpath"
	"github.com/xaionaro-go/xsync"
)

const (
	debugRunInTheSameProcess = false
)

type ChatHandlerOBSOLETE struct {
	*client.GRPCClient
	Cmd    *exec.Cmd
	Locker xsync.Mutex
}

func New(
	ctx context.Context,
	channelSlug string,
) (*ChatHandlerOBSOLETE, error) {
	if debugRunInTheSameProcess {
		return newInTheSameProcess(ctx, channelSlug)
	}
	return new(ctx, channelSlug)
}

func new(
	ctx context.Context,
	channelSlug string,
) (*ChatHandlerOBSOLETE, error) {
	execPath, err := xpath.GetExecPath(os.Args[0])
	if err != nil {
		return nil, fmt.Errorf("unable to get self-path: %w", err)
	}
	cmd := exec.Command(execPath)
	cmd.Stderr = os.Stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("unable to initialize an stdout pipe: %w", err)
	}
	cmd.Env = append(os.Environ(), EnvKeyIsServer+"=1")
	cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", EnvKeyLoggingLevel, logger.FromCtx(ctx).Level().String()))
	err = child_process_manager.ConfigureCommand(cmd)
	errmon.ObserveErrorCtx(ctx, err)
	err = cmd.Start()
	if err != nil {
		return nil, fmt.Errorf("unable to start a subprocess to isolate kick chat handler: %w", err)
	}
	err = child_process_manager.AddChildProcess(cmd.Process)
	if err != nil {
		if runtime.GOOS == "windows" {
			// this is actually an error, but I have no idea how to fix it, so demoting to a debug message
			logger.Debugf(ctx, "unable to register the command to be auto-killed: %v", err)
		} else {
			logger.Errorf(ctx, "unable to register the command to be auto-killed: %v", err)
		}
	}

	decoder := json.NewDecoder(stdout)
	var d ReturnedData
	err = decoder.Decode(&d)
	logger.Debugf(ctx, "got data: %#+v", d)
	if err != nil {
		return nil, fmt.Errorf("unable to un-JSON-ize the process output: %w", err)
	}

	client, err := client.New(ctx, d.ListenAddr, channelSlug)
	if err != nil {
		return nil, fmt.Errorf("unable to initialize a gRPC client: %w", err)
	}

	return &ChatHandlerOBSOLETE{
		GRPCClient: client,
		Cmd:        cmd,
	}, nil
}

func newInTheSameProcess(
	ctx context.Context,
	channelSlug string,
) (_ret *ChatHandlerOBSOLETE, _err error) {
	ctx, cancelFn := context.WithCancel(ctx)
	defer func() {
		if _err != nil {
			cancelFn()
		}
	}()
	addrCh := make(chan net.Addr, 1)
	errCh := make(chan error, 1)
	observability.Go(ctx, func(ctx context.Context) {
		select {
		case <-ctx.Done():
			return
		case errCh <- runServer(ctx, func(reportedAddr net.Addr) error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case addrCh <- reportedAddr:
			}
			return nil
		}):
		}
	})
	select {
	case addr := <-addrCh:
		client, err := client.New(ctx, addr.String(), channelSlug)
		if err != nil {
			return nil, fmt.Errorf("unable to initialize a gRPC client: %w", err)
		}
		return &ChatHandlerOBSOLETE{
			GRPCClient: client,
		}, nil
	case err := <-errCh:
		return nil, err
	}
}

func (h *ChatHandlerOBSOLETE) Close(ctx context.Context) error {
	return xsync.DoR1(ctx, &h.Locker, func() error {
		if h.GRPCClient == nil {
			return fmt.Errorf("chat handler is already closed")
		}
		if err := h.GRPCClient.Close(ctx); err != nil {
			logger.Errorf(ctx, "unable to close gRPC client: %v", err)
		}
		h.GRPCClient = nil
		if h.Cmd == nil {
			return fmt.Errorf("not a subprocess-based chat handler")
		}
		if err := h.Cmd.Process.Kill(); err != nil {
			logger.Errorf(ctx, "unable to kill chat handler subprocess: %v", err)
		}
		h.Cmd = nil
		return nil
	})
}
