package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"

	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/facebookincubator/go-belt/tool/logger/implementation/logrus"
	"github.com/spf13/pflag"
	"github.com/xaionaro-go/streamctl/pkg/observability"
	"github.com/xaionaro-go/streamctl/pkg/speech/speechtotext/whisper"
)

func syntaxExit(message string) {
	fmt.Fprintf(os.Stderr, "syntax error: %s\n", message)
	pflag.Usage()
	os.Exit(2)
}

func main() {
	loggerLevel := logger.LevelDebug
	pflag.Var(&loggerLevel, "log-level", "Log level")
	pflag.Parse()
	if pflag.NArg() != 1 {
		syntaxExit("expected one argument (address of the server)")
	}

	whisperSrvAddr := pflag.Arg(0)

	l := logrus.Default().WithLevel(loggerLevel)
	ctx := logger.CtxWithLogger(context.Background(), l)
	logger.Default = func() logger.Logger {
		return l
	}
	defer belt.Flush(ctx)

	conn, err := net.Dial("tcp", whisperSrvAddr)
	if err != nil {
		logger.Panicf(ctx, "unable to connect to whisper by address '%s': %v", whisperSrvAddr, err)
	}

	stt := whisper.New(ctx, conn, true)
	defer stt.Close()

	observability.Go(ctx, func() {
		defer logger.Infof(ctx, "stopped reader")
		logger.Infof(ctx, "started reader")
		for t := range stt.OutputChan() {
			var out bytes.Buffer
			enc := json.NewEncoder(&out)
			enc.SetIndent("", " ")
			err := enc.Encode(t)
			if err != nil {
				logger.Panic(ctx, err)
			}
			fmt.Println(out.String())
		}
	})

	defer logger.Infof(ctx, "stopped writer")
	logger.Infof(ctx, "started writer")
	buf := make([]byte, 1024*1024)
	for {
		n, err := os.Stdin.Read(buf)
		if n == 0 && err != nil {
			break
		}
		err = stt.WriteAudio(ctx, buf[:n])
		if err != nil {
			logger.Panic(ctx, err)
		}
	}
}
