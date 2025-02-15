package ximage

import (
	"image"
	"image/color"
	"math"
)

type PointFloat64 struct {
	X float64
	Y float64
}

type RectangleFloat64 struct {
	Min PointFloat64
	Max PointFloat64
}

func (t *RectangleFloat64) Size() PointFloat64 {
	return PointFloat64{
		X: math.Abs(t.Max.X - t.Min.X),
		Y: math.Abs(t.Max.Y - t.Min.Y),
	}
}

func (t *RectangleFloat64) At(x, y int) color.Color {
	return t.AtFloat64(float64(x), float64(y))
}

func (t *RectangleFloat64) AtFloat64(x, y float64) color.Color {
	if x < t.Min.X || x > t.Max.X {
		return nil
	}
	if y < t.Min.Y || y > t.Max.Y {
		return nil
	}
	return color.Gray{Y: 255}
}

type Transform struct {
	image.Image
	ImageBounds     image.Rectangle
	ImageSize       image.Point
	ColorModelValue color.Model
	To              RectangleFloat64
}

var _ image.Image = (*Transform)(nil)

func NewTransform(
	img image.Image,
	colorModel color.Model,
	transform RectangleFloat64,
) *Transform {
	if img == nil {
		return nil
	}
	return &Transform{
		Image:           img,
		ImageBounds:     img.Bounds(),
		ImageSize:       img.Bounds().Size(),
		ColorModelValue: colorModel,
		To:              transform,
	}
}

func (t *Transform) ColorModel() color.Model {
	return t.ColorModelValue
}

func (t *Transform) Bounds() image.Rectangle {
	return image.Rectangle{
		Min: image.Point{
			X: int(t.To.Min.X),
			Y: int(t.To.Min.Y),
		},
		Max: image.Point{
			X: int(t.To.Max.X),
			Y: int(t.To.Max.Y),
		},
	}
}

func (t *Transform) At(x, y int) color.Color {
	return t.AtFloat64(float64(x), float64(y))
}

func (t *Transform) Coords(x, y float64) (int, int, bool) {
	if x < t.To.Min.X || x > t.To.Max.X {
		return 0, 0, false
	}
	if y < t.To.Min.Y || y > t.To.Max.Y {
		return 0, 0, false
	}
	x = (x - t.To.Min.X) / (t.To.Max.X - t.To.Min.X)
	y = (y - t.To.Min.Y) / (t.To.Max.Y - t.To.Min.Y)
	srcX := t.ImageBounds.Min.X + int(x*float64(t.ImageSize.X))
	srcY := t.ImageBounds.Min.Y + int(y*float64(t.ImageSize.Y))
	return srcX, srcY, true
}

func (t *Transform) AtFloat64(x, y float64) color.Color {
	srcX, srcY, ok := t.Coords(x, y)
	if !ok {
		return nil
	}
	return t.Image.At(srcX, srcY)
}
