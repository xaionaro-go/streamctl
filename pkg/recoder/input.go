package recoder

import (
	"io"
)

type Input interface {
	io.Closer
}
