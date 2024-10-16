package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/asticode/go-astiav"
	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/facebookincubator/go-belt/tool/logger/implementation/logrus"
	"github.com/spf13/pflag"
	"github.com/xaionaro-go/streamctl/pkg/recoder/libav/recoder"
)

func main() {
	pflag.Usage = func() {
		fmt.Fprintf(os.Stderr, "syntax: %s <URL-from> <URL-to>\n", os.Args[0])
	}

	loggerLevel := logger.LevelWarning
	pflag.Var(&loggerLevel, "log-level", "Log level")
	pflag.Parse()
	if len(pflag.Args()) != 2 {
		pflag.Usage()
		os.Exit(1)
	}

	l := logrus.Default().WithLevel(loggerLevel)
	ctx := logger.CtxWithLogger(context.Background(), l)
	logger.Default = func() logger.Logger {
		return l
	}
	defer belt.Flush(ctx)

	fromURL := pflag.Arg(0)
	toURL := pflag.Arg(1)

	astiav.SetLogLevel(recoder.LogLevelToAstiav(l.Level()))
	astiav.SetLogCallback(func(c astiav.Classer, level astiav.LogLevel, fmt, msg string) {
		var cs string
		if c != nil {
			if cl := c.Class(); cl != nil {
				cs = " - class: " + cl.String()
			}
		}
		l.Logf(
			recoder.LogLevelFromAstiav(level),
			"%s%s",
			strings.TrimSpace(msg), cs,
		)
	})

	l.Debugf("opening '%s' as the input...", fromURL)
	input, err := recoder.NewInputFromURL(ctx, fromURL, "", recoder.InputConfig{})
	if err != nil {
		l.Fatal(err)
	}

	l.Debugf("opening '%s' as the output...", toURL)
	output, err := recoder.NewOutputFromURL(ctx, toURL, "", recoder.OutputConfig{})
	if err != nil {
		l.Fatal(err)
	}

	l.Debugf("starting the recoding...")
	err = recoder.New(recoder.RecoderConfig{}).Recode(ctx, input, output)
	if err != nil {
		l.Fatal(err)
	}
}
