package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"

	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/logger"
	xlogrus "github.com/facebookincubator/go-belt/tool/logger/implementation/logrus"
	"github.com/facebookincubator/go-belt/tool/logger/types"
	"github.com/spf13/pflag"
	"github.com/xaionaro-go/streamctl/pkg/p2p"
)

func main() {
	pflag.Parse()

	ctx := logger.CtxWithLogger(context.Background(), xlogrus.Default().WithLevel(logger.LevelTrace))
	ctx, cancelFn := context.WithCancel(ctx)
	logger.Default = func() types.Logger {
		return logger.FromCtx(ctx)
	}
	defer belt.Flush(ctx)
	defer cancelFn()

	_, peer0PrivKey, err := ed25519.GenerateKey(rand.Reader)
	assertNoError(err)

	p2p, err := p2p.NewP2P(
		ctx,
		peer0PrivKey,
		"test",
		"xaionaro-void-test",
		[]byte("test"),
		"",
	)
	assertNoError(err)

	err = p2p.Start(ctx)
	assertNoError(err)

	<-context.Background().Done()
}

func assertNoError(err error) {
	if err != nil {
		panic(err)
	}
}
