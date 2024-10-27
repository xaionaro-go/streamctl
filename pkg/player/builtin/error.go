package builtin

import (
	"fmt"

	"github.com/asticode/go-astiav"
)

type ErrPixelFormatNotSupported struct {
	PixelFormat astiav.PixelFormat
}

func (e ErrPixelFormatNotSupported) Error() string {
	return fmt.Sprintf("support of pixel format %v is not implemented, yet", e.PixelFormat)
}
