package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/facebookincubator/go-belt/tool/logger/implementation/zap"
	"github.com/nicklaw5/helix/v2"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/secret"
	"github.com/xaionaro-go/streamctl/pkg/oauthhandler"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/twitch"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/twitch/auth"
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
	clientID := flag.String("client-id", "", "client ID for a WebSockets subscription (if not provided IRC will be used, instead)")
	clientSecret := flag.String("client-secret", "", "client secret for a WebSockets subscription (if not provided IRC will be used, instead)")
	flag.Parse()
	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(1)
	}
	channelID := flag.Arg(0)

	var (
		h   twitch.ChatHandler
		err error
	)
	if *clientID != "" && *clientSecret != "" {
		h, err = newChatHandlerWebsockets(ctx, *clientID, *clientSecret, channelID)
	} else {
		h, err = twitch.NewChatHandlerIRC(ctx, channelID)
	}
	assertNoError(err)

	fmt.Println("started")
	for ev := range h.MessagesChan() {
		fmt.Printf("%#+v\n", ev)
	}
}

const oauthListenerPort = 8091

func newChatHandlerWebsockets(
	ctx context.Context,
	clientID string,
	clientSecret string,
	channelID string,
) (*twitch.ChatHandlerSub, error) {
	options := &helix.Options{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURI:  auth.RedirectURI(oauthListenerPort),
	}
	client, err := helix.NewClientWithContext(ctx, options)
	if err != nil {
		return nil, fmt.Errorf("unable to create a helix client object: %w", err)
	}
	var clientCode secret.String
	err = auth.NewClientCode(
		ctx,
		clientID,
		oauthhandler.OAuth2HandlerViaBrowser,
		func() []uint16 {
			return []uint16{oauthListenerPort}
		}, func(code string) {
			clientCode.Set(code)
		},
	)
	if err != nil {
		return nil, fmt.Errorf("unable to get a client code: %w", err)
	}
	accessToken, refreshToken, err := auth.NewTokenByUser(ctx, client, clientCode)
	client.SetUserAccessToken(accessToken.Get())
	client.SetRefreshToken(refreshToken.Get())
	userID, err := twitch.GetUserID(ctx, client, channelID)
	if err != nil {
		return nil, fmt.Errorf("unable to get the user ID for login '%s': %w", channelID, err)
	}
	if observability.IsOnInsecureDebug(ctx) {
		logger.Tracef(ctx, "user access token: %v", accessToken.Get())
	}
	h, err := twitch.NewChatHandlerSub(ctx, client, userID, nil)
	if err != nil {
		return nil, fmt.Errorf("unable to get a chat handler: %w", err)
	}
	return h, nil
}
