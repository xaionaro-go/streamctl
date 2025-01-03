package recoder

import (
	"io"
)

type Encoder interface {
	io.Closer
}
