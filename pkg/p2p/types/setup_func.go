package types

import (
	"google.golang.org/grpc"
)

type FuncSetupServer func(*grpc.Server) error
type FuncSetupClient func(*grpc.ClientConn) error
