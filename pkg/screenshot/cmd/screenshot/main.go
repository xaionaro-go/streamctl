package main

import (
	"flag"
	"image"
	"image/png"
	"os"

	"github.com/xaionaro-go/streamctl/pkg/screenshot"
)

func main() {
	xMin := flag.Int("x-min", 0, "")
	xMax := flag.Int("x-max", 100, "")
	yMin := flag.Int("y-min", 0, "")
	yMax := flag.Int("y-max", 100, "")
	flag.Parse()

	img, err := screenshot.Screenshot(screenshot.Config{
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
	})
	assertNoError(err)

	err = png.Encode(os.Stdout, img)
	assertNoError(err)
}
