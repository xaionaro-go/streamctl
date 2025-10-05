//go:build linux
// +build linux

package screenshoter

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"os"
	"os/exec"
	"path"
	"strings"
	"time"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/avpipeline"
	"github.com/xaionaro-go/avpipeline/codec"
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/kernel"
	"github.com/xaionaro-go/avpipeline/kernel/boilerplate"
	"github.com/xaionaro-go/avpipeline/node"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/streamctl/pkg/screenshot"
	"github.com/xaionaro-go/secret"
	"golang.org/x/sys/unix"
)

type ScreenshoterWayland struct {
	Display string
}

func (s *ScreenshoterWayland) Loop(
	ctx context.Context,
	interval time.Duration,
	config screenshot.Config,
	callback func(context.Context, image.Image),
) error {
	ctx, cancelFn := context.WithCancel(ctx)
	defer cancelFn()

	logger.Debugf(ctx, "creating a temporary FIFO file for wf-recorder")
	d, err := os.MkdirTemp("", "streamctl-screenshoter-wayland-")
	if err != nil {
		return fmt.Errorf("unable to create a temporary directory: %w", err)
	}
	defer os.RemoveAll(d)
	socketPath := path.Join(d, s.Display+".y4m")
	err = unix.Mkfifo(socketPath, 0600)
	if err != nil {
		return fmt.Errorf("unable to create a FIFO file %q: %w", socketPath, err)
	}
	defer os.Remove(socketPath)

	logger.Debugf(ctx, "starting wf-recorder")
	args := []string{
		"wf-recorder",
		"-o", "DP-8",
		"-y",
		"-r", fmt.Sprintf("%f", 1.0/interval.Seconds()),
		"-c", "rawvideo",
		"-x", "yuv420p",
		"-m", "yuv4mpegpipe",
		"-f", socketPath,
	}
	cmd := exec.Command(args[0], args[1:]...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Start()
	if err != nil {
		return fmt.Errorf("unable to start wf-recorder: %w", err)
	}
	observability.Go(ctx, func(ctx context.Context) {
		err := cmd.Wait()
		cancelFn()
		logger.Debugf(ctx, "%s exited: %v; stdout: %q; stderr: %q", strings.Join(args, " "), err, stdout.String(), stderr.String())
		os.WriteFile(socketPath, []byte{}, 0600) // wake up the reader
	})
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()

	logger.Debugf(ctx, "opening the FIFO file for reading")
	inputKernel, err := kernel.NewInputFromURL(ctx, socketPath, secret.New(""), kernel.InputConfig{
		AsyncOpen: true,
	})
	if err != nil {
		return fmt.Errorf("unable to open FIFO file %q for reading: %w", socketPath, err)
	}
	defer inputKernel.Close(ctx)

	inputNode := node.NewFromKernel(ctx, inputKernel)
	defer inputNode.GetProcessor().Close(ctx)

	decoderKernel := kernel.NewDecoder(ctx, codec.NewNaiveDecoderFactory(ctx, nil))
	defer decoderKernel.Close(ctx)

	decoderNode := node.NewFromKernel(ctx, decoderKernel)
	defer decoderNode.GetProcessor().Close(ctx)
	inputNode.AddPushPacketsTo(decoderNode)

	var img image.Image
	receiverNode := node.NewFromKernel(
		ctx,
		boilerplate.NewFuncsToKernel(
			ctx,
			nil,
			nil,
			func(
				ctx context.Context,
				input frame.Input,
				_ chan<- packet.Output,
				_ chan<- frame.Output,
			) error {
				var err error
				if img == nil {
					img, err = input.Data().GuessImageFormat()
					if err != nil {
						return fmt.Errorf("unable to guess image format: %w", err)
					}
				}
				err = input.Data().ToImage(img)
				if err != nil {
					return fmt.Errorf("unable to convert frame to image: %w", err)
				}
				callback(ctx, img)
				return nil
			},
			nil,
		),
	)
	defer receiverNode.GetProcessor().Close(ctx)
	decoderNode.AddPushFramesTo(receiverNode)

	logger.Debugf(ctx, "starting the decoding loop")
	errCh := make(chan node.Error, 100)
	observability.Go(ctx, func(ctx context.Context) {
		avpipeline.Serve(ctx, avpipeline.ServeConfig{}, errCh, inputNode)
	})

	logger.Debugf(ctx, "waiting for an error or context cancellation")
	select {
	case err := <-errCh:
		return fmt.Errorf("error during processing: %w", err)
	case <-ctx.Done():
		return ctx.Err()
	}
}
