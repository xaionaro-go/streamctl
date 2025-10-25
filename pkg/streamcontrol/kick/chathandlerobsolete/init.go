package chathandlerobsolete

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"

	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/facebookincubator/go-belt/tool/logger/implementation/logrus"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/kick/chathandlerobsolete/server"
)

const (
	EnvKeyIsServer     = "STREAMCTL_STREAMCONTROL_KICK_SERVER"
	EnvKeyLoggingLevel = "STREAMCTL_STREAMCONTROL_KICK_LOGGING_LEVEL"
)

type ReturnedData struct {
	ListenAddr string `json:"listen_addr"`
}

func init() {
	if os.Getenv(EnvKeyIsServer) == "" {
		return
	}
	loggingLevel := logger.LevelWarning
	loggingLevel.Set(os.Getenv(EnvKeyLoggingLevel))
	l := logrus.Default().WithLevel(loggingLevel)
	ctx := context.Background()
	ctx = logger.CtxWithLogger(ctx, l)
	logger.Default = func() logger.Logger {
		return l
	}
	defer belt.Flush(ctx)
	runServer(ctx, func(addr net.Addr) error {
		d := ReturnedData{
			ListenAddr: addr.String(),
		}
		b, err := json.Marshal(d)
		if err != nil {
			return fmt.Errorf("unable to send the address")
		}
		fmt.Fprintf(os.Stdout, "%s\n", b)
		os.Stdout.Close()
		return nil
	})
	belt.Flush(ctx)
	os.Exit(0)
}

func runServer(
	_ context.Context,
	addressReporter func(addr net.Addr) error,
) error {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("failed to listen: %w", err)
	}
	defer listener.Close()

	if addressReporter != nil {
		if err := addressReporter(listener.Addr()); err != nil {
			return fmt.Errorf("unable to report the address: %w", err)
		}
	}

	srv := server.NewGRPCServer()
	err = srv.Serve(listener)
	if err != nil {
		return fmt.Errorf("unable to serve: %w", err)
	}

	return nil
}
