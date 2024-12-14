package encoder

import (
	"io"
)

type Output interface {
	io.Closer
}
