package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/davecgh/go-spew/spew"
	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/facebookincubator/go-belt/tool/logger/implementation/zap"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/twitch"
)

func assertNoError(err error) {
	if err == nil {
		return
	}
	log.Panic(err)
}

func main() {
	l := zap.Default().WithLevel(logger.LevelTrace)
	ctx := context.Background()
	ctx = logger.CtxWithLogger(ctx, l)
	ctx = observability.OnInsecureDebug(ctx)
	logger.Default = func() logger.Logger {
		return l
	}
	defer belt.Flush(ctx)
	oldUsage := flag.Usage
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "syntax: chatlistener [options] <channel_id>\n")
		oldUsage()
	}
	channelID := flag.String("channel-id", "", "")

	clientID := flag.String("client-id", "", "")
	clientSecret := flag.String("client-secret", "", "")
	flag.Parse()
	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(1)
	}
	user := flag.Arg(0)

	cfg := twitch.Config{
		Enable: new(bool),
		Config: twitch.PlatformSpecificConfig{
			Channel:  *channelID,
			ClientID: *clientID,
			GetOAuthListenPorts: func() []uint16 {
				return []uint16{8092}
			},
		},
	}
	cfg.Config.ClientSecret.Set(*clientSecret)
	c, err := twitch.New(ctx, cfg, func(c twitch.Config) error {
		return nil
	})
	if err != nil {
		panic(err)
	}

	userInfo, err := c.GetUser(user)
	if err != nil {
		panic(err)
	}

	spew.Dump(userInfo)
}
