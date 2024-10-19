package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/twitch"
)

func assertNoError(err error) {
	if err == nil {
		return
	}
	log.Panic(err)
}

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "syntax: chatlistener <channel_id>\n")
	}
	flag.Parse()
	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(1)
	}
	channelID := flag.Arg(0)

	h, err := twitch.NewChatHandler(channelID)
	assertNoError(err)

	fmt.Println("started")
	for ev := range h.MessagesChan() {
		fmt.Printf("%#+v\n", ev)
	}
}
