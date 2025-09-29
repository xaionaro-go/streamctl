package main

import (
	"context"
	"flag"
	"fmt"
	"image"
	"image/png"
	"os"
	"time"

	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/facebookincubator/go-belt/tool/logger/implementation/logrus"
	"github.com/xaionaro-go/streamctl/pkg/screenshot"
	"github.com/xaionaro-go/streamctl/pkg/screenshoter"
)

func main() {
	xMin := flag.Int("x-min", 0, "")
	xMax := flag.Int("x-max", 100, "")
	yMin := flag.Int("y-min", 0, "")
	yMax := flag.Int("y-max", 100, "")
	fps := flag.Float64("fps", 30, "")
	outputFile := flag.String("output-file", "", "")
	flag.Parse()

	l := logrus.Default().WithLevel(logger.LevelDebug)
	ctx := context.Background()
	ctx = logger.CtxWithLogger(ctx, l)
	logger.Default = func() logger.Logger {
		return l
	}
	defer belt.Flush(ctx)

	h := screenshoter.New()

	startedAt := time.Now()
	frameCount := 0
	logger.Debugf(ctx, "starting the loop with %f FPS", *fps)
	err := h.Loop(
		ctx,
		time.Duration(float64(time.Second) / *fps),
		screenshot.Config{
			Bounds: image.Rectangle{
				Min: image.Point{
					X: *xMin,
					Y: *yMin,
				},
				Max: image.Point{
					X: *xMax,
					Y: *yMax,
				},
			},
		},
		func(_ context.Context, img image.Image) {
			frameCount++
			fps := float64(frameCount) / time.Since(startedAt).Seconds()
			fmt.Printf("received a picture; overall FPS: %f\n", fps)
			if *outputFile == "" {
				return
			}

			func() {
				f, err := os.OpenFile(*outputFile+"~", os.O_CREATE|os.O_WRONLY, 0644)
				if err != nil {
					logger.Error(ctx, "unable to open '%s'", *outputFile)
					return
				}
				defer f.Close()

				err = png.Encode(f, img)
				if err != nil {
					logger.Errorf(ctx, "unable to encode the image to PNG: %w", err)
					return
				}

				err = os.Rename(*outputFile+"~", *outputFile)
				if err != nil {
					logger.Errorf(ctx, "unable to rename '%s~' to '%s': %w", *outputFile, *outputFile, err)
					return
				}
			}()
		},
	)
	assertNoError(err)
}
