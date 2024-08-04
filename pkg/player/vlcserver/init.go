//go:build with_libvlc
// +build with_libvlc

package vlcserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"

	"github.com/facebookincubator/go-belt"
	"github.com/xaionaro-go/streamctl/pkg/player/vlcserver/server"
)

const (
	EnvKeyIsVLCServer = "IS_STREAMPANEL_VLCSERVER"
)

func init() {
	if os.Getenv(EnvKeyIsVLCServer) != "" {
		runVLCServer()
		belt.Flush(context.TODO())
		os.Exit(0)
	}
}

func runVLCServer() {
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
	err = srv.Serve(listener)
	panic(err)
}
