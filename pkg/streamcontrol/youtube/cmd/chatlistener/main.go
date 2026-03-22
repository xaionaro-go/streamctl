package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/logger"
	xlogrus "github.com/facebookincubator/go-belt/tool/logger/implementation/logrus"
	"github.com/spf13/pflag"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/youtube"
	"golang.org/x/oauth2"
)

func assertNoError(err error) {
	if err == nil {
		return
	}
	log.Panic(err)
}

func main() {
	logLevel := logger.LevelInfo
	pflag.Var(&logLevel, "log-level", "")
	tokenFile := pflag.String("token-file", "", "path to OAuth2 token JSON file")
	clientID := pflag.String("client-id", "", "OAuth2 client ID")
	clientSecret := pflag.String("client-secret", "", "OAuth2 client secret")
	grpcHost := pflag.String("grpc-host", "", "gRPC server address (default: youtube.googleapis.com:443)")
	pflag.Parse()

	if pflag.NArg() != 2 {
		fmt.Fprintf(os.Stderr, "usage: chatlistener [flags] <video-id> <live-chat-id>\n")
		os.Exit(1)
	}

	videoID := pflag.Arg(0)
	liveChatID := pflag.Arg(1)

	ctx := logger.CtxWithLogger(context.Background(), xlogrus.Default().WithLevel(logLevel))
	logger.Default = func() logger.Logger {
		return logger.FromCtx(ctx)
	}
	defer belt.Flush(ctx)

	if *tokenFile == "" || *clientID == "" || *clientSecret == "" {
		fmt.Fprintf(os.Stderr, "--token-file, --client-id, and --client-secret are required\n")
		os.Exit(1)
	}

	tokenData, err := os.ReadFile(*tokenFile)
	assertNoError(err)

	var token oauth2.Token
	assertNoError(json.Unmarshal(tokenData, &token))

	cfg := &oauth2.Config{
		ClientID:     *clientID,
		ClientSecret: *clientSecret,
	}
	tokenSource := cfg.TokenSource(ctx, &token)

	h, err := youtube.NewChatListener(ctx, videoID, liveChatID, tokenSource, nil, *grpcHost)
	assertNoError(err)

	fmt.Println("started")
	for ev := range h.MessagesChan() {
		fmt.Printf("%#+v\n", ev)
	}
}
