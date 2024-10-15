package recoder

import (
	"io"
)

type Output interface {
	io.Closer
}
