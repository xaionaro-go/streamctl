package process

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"

	child_process_manager "github.com/AgustinSRG/go-child-process-manager"
	"github.com/facebookincubator/go-belt/tool/experimental/errmon"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/implementations/libav/saferecoder/process/client"
)

type Recoder struct {
	*client.Client
	Cmd *exec.Cmd
}

func Run(
	ctx context.Context,
) (*Recoder, error) {
	cmd := exec.Command(os.Args[0])
	cmd.Stderr = os.Stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("unable to initialize an stdout pipe: %w", err)
	}
	cmd.Env = append(os.Environ(), EnvKeyIsRecoder+"=1")
	err = child_process_manager.ConfigureCommand(cmd)
	errmon.ObserveErrorCtx(ctx, err)
	err = cmd.Start()
	if err != nil {
		return nil, fmt.Errorf("unable to start a forked process of a libav-recoder: %w", err)
	}
	err = child_process_manager.AddChildProcess(cmd.Process)
	errmon.ObserveErrorCtx(ctx, err)

	decoder := json.NewDecoder(stdout)
	var d ReturnedData
	err = decoder.Decode(&d)
	logger.Debugf(ctx, "got data: %#+v", d)
	if err != nil {
		return nil, fmt.Errorf("unable to un-JSON-ize the process output: %w", err)
	}

	c := client.New(d.ListenAddr)
	level := logger.FromCtx(ctx).Level()
	if err := c.SetLoggingLevel(ctx, level); err != nil {
		return nil, fmt.Errorf("unable to set the logging level to %s: %w", level, err)
	}

	return &Recoder{
		Client: c,
		Cmd:    cmd,
	}, nil
}

func (r *Recoder) Kill() error {
	return r.Cmd.Process.Kill()
}

func (r *Recoder) Wait(ctx context.Context) error {
	_, err := r.Cmd.Process.Wait()
	return err
}
