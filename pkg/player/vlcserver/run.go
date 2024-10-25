//go:build with_libvlc
// +build with_libvlc

package vlcserver

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"time"

	child_process_manager "github.com/AgustinSRG/go-child-process-manager"
	"github.com/facebookincubator/go-belt/tool/experimental/errmon"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/player/vlcserver/client"
	"github.com/xaionaro-go/streamctl/pkg/xpath"
)

type VLC struct {
	Client *client.Client
	Cmd    *exec.Cmd
}

func Run(
	ctx context.Context,
	title string,
) (*VLC, error) {
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
	cmd.Env = append(os.Environ(), EnvKeyIsVLCServer+"=1")
	err = child_process_manager.ConfigureCommand(cmd)
	errmon.ObserveErrorCtx(ctx, err)
	err = cmd.Start()
	if err != nil {
		return nil, fmt.Errorf("unable to start a subprocess to isolate VLC: %w", err)
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

	return &VLC{
		Client: client.New(title, d.ListenAddr),
		Cmd:    cmd,
	}, nil
}

func (vlc *VLC) SetupForStreaming(
	ctx context.Context,
) error {
	return nil
}

func (vlc *VLC) ProcessTitle(
	ctx context.Context,
) (string, error) {
	return vlc.Client.ProcessTitle(ctx)
}

func (vlc *VLC) OpenURL(
	ctx context.Context,
	link string,
) error {
	return vlc.Client.OpenURL(ctx, link)
}

func (vlc *VLC) GetLink(
	ctx context.Context,
) (string, error) {
	return vlc.Client.GetLink(ctx)
}

func (vlc *VLC) EndChan(
	ctx context.Context,
) (<-chan struct{}, error) {
	return vlc.Client.EndChan(ctx)
}

func (vlc *VLC) IsEnded(
	ctx context.Context,
) (bool, error) {
	return vlc.Client.IsEnded(ctx)
}

func (vlc *VLC) GetPosition(
	ctx context.Context,
) (time.Duration, error) {
	return vlc.Client.GetPosition(ctx)
}

func (vlc *VLC) GetLength(
	ctx context.Context,
) (time.Duration, error) {
	return vlc.Client.GetLength(ctx)
}

func (vlc *VLC) GetSpeed(
	ctx context.Context,
) (float64, error) {
	return vlc.Client.GetSpeed(ctx)
}

func (vlc *VLC) SetSpeed(
	ctx context.Context,
	speed float64,
) error {
	return vlc.Client.SetSpeed(ctx, speed)
}

func (vlc *VLC) GetPause(
	ctx context.Context,
) (bool, error) {
	return vlc.Client.GetPause(ctx)
}

func (vlc *VLC) SetPause(
	ctx context.Context,
	pause bool,
) error {
	return vlc.Client.SetPause(ctx, pause)
}

func (vlc *VLC) Stop(
	ctx context.Context,
) error {
	return vlc.Client.Stop(ctx)
}

func (vlc *VLC) Close(
	ctx context.Context,
) error {
	defer vlc.Cmd.Process.Kill()
	return vlc.Client.Close(ctx)
}
