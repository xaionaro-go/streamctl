package streamportserver

import (
	"context"
)

type GetPortServerser interface {
	GetPortServers(context.Context) ([]Config, error)
}
