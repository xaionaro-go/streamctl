//go:build with_libav
// +build with_libav

package process

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"

	"github.com/facebookincubator/go-belt"
	"github.com/xaionaro-go/streamctl/pkg/recoder/libav/saferecoder/process/server"
)

const (
	EnvKeyIsEncoder = "IS_STREAMPANEL_RECODER"
)

func init() {
	if os.Getenv(EnvKeyIsEncoder) != "" {
		runEncoder()
		belt.Flush(context.TODO())
		os.Exit(0)
	}
}

func runEncoder() {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(fmt.Errorf("failed to listen: %w", err))
	}
	defer listener.Close()

	d := ReturnedData{
		ListenAddr: listener.Addr().String(),
	}
	b, err := json.Marshal(d)
	if err != nil {
		panic(err)
	}

	fmt.Fprintf(os.Stdout, "%s\n", b)

	srv := server.NewServer()
	err = srv.Serve(context.TODO(), listener)
	panic(err)
}
