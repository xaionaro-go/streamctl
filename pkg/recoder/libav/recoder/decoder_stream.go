package recoder

import (
	"github.com/asticode/go-astiav"
)

type decoderStream interface {
	CodecContext() *astiav.CodecContext
	InputStream() *astiav.Stream
}
