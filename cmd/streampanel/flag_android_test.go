//go:build android || android_test
// +build android android_test

package main

import (
	"context"
	"os"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/facebookincubator/go-belt/tool/logger"
	xlogrus "github.com/facebookincubator/go-belt/tool/logger/implementation/logrus"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/streamctl/pkg/xpath"
)

func TestGetFlagsAndroidFromFiles(t *testing.T) {
	ctx := context.Background()
	ll := xlogrus.DefaultLogrusLogger()
	l := xlogrus.New(ll).WithLevel(logger.LevelTrace)
	logger.Default = func() logger.Logger {
		return l
	}
	ctx = logger.CtxWithLogger(ctx, l)

	flagsFilePath, err := xpath.Expand("~/flags.yaml")
	if err != nil {
		t.Fatal(err)
		return
	}
	logger.Infof(ctx, "file path: '%s'", flagsFilePath)
	flagsFilePath = strings.Trim(flagsFilePath, "/")

	fs := fstest.MapFS{
		flagsFilePath: &fstest.MapFile{
			Data: []byte(`LoggerLevel: trace
LogstashAddr: "tcp://192.168.141.10:5363"
LogFile: "~/trace.log"`),
		},
	}
	androidFS = fs
	_, err = androidFS.Open(flagsFilePath)
	require.False(t, os.IsNotExist(err), "the fake FS does not work properly")

	var flags Flags
	getFlagsAndroidFromFiles(&flags)
	require.Equal(t, Flags{
		LoggerLevel:  loggerLevel(logger.LevelTrace),
		LogstashAddr: "tcp://192.168.141.10:5363",
		LogFile:      "~/trace.log",
	}, flags)
}
