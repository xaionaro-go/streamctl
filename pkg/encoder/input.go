package encoder

import (
	"io"
)

type Input interface {
	io.Closer
}
