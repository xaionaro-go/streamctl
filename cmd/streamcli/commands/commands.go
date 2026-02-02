package commands

import (
	"bytes"
	"context"
	"crypto"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	_ "net/http/pprof"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/spf13/cobra"
	"github.com/xaionaro-go/observability"
	videodecoder "github.com/xaionaro-go/player/pkg/player/decoder/libav"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"

	"github.com/xaionaro-go/streamctl/pkg/streamd/client"
	"github.com/xaionaro-go/streamctl/pkg/streampanel/consts"
	"github.com/xaionaro-go/xcontext"
	"github.com/xaionaro-go/xsync"
)

var (
	// Access these variables only from a main package:

	Root = &cobra.Command{
		Use: os.Args[0],
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			ctx := cmd.Context()
			l := logger.FromCtx(ctx).WithLevel(LoggerLevel)
			ctx = logger.CtxWithLogger(ctx, l)
			cmd.SetContext(ctx)
			logger.Debugf(ctx, "log-level: %v", LoggerLevel)

			netPprofAddr, err := cmd.Flags().GetString("go-net-pprof-addr")
			if err != nil {
				l.Error("unable to get the value of the flag 'go-net-pprof-addr': %v", err)
			}
			if netPprofAddr != "" {
				observability.Go(ctx, func(ctx context.Context) {
					if netPprofAddr == "" {
						netPprofAddr = "localhost:0"
					}
					l.Infof("starting to listen for net/pprof requests at '%s'", netPprofAddr)
					l.Error(http.ListenAndServe(netPprofAddr, nil))
				})
			}
		},
		PersistentPostRun: func(cmd *cobra.Command, args []string) {
			ctx := cmd.Context()
			logger.Debug(ctx, "end")
		},
	}

	Stream = &cobra.Command{
		Use: "stream",
	}

	StreamSetup = &cobra.Command{
		Use:  "setup",
		Args: cobra.ExactArgs(0),
		Run:  streamSetup,
	}

	StreamStatus = &cobra.Command{
		Use:  "status",
		Args: cobra.ExactArgs(0),
		Run:  streamStatus,
	}

	Variables = &cobra.Command{
		Use: "variables",
	}

	VariablesGet = &cobra.Command{
		Use:  "get",
		Args: cobra.ExactArgs(1),
		Run:  variablesGet,
	}

	VariablesGetHash = &cobra.Command{
		Use:  "get_hash",
		Args: cobra.ExactArgs(1),
		Run:  variablesGetHash,
	}

	VariablesSet = &cobra.Command{
		Use:  "set",
		Args: cobra.ExactArgs(1),
		Run:  variablesSet,
	}

	VariablesSetScreenshotFromURL = &cobra.Command{
		Use:  "set_image_from_url",
		Args: cobra.MinimumNArgs(3),
		Run:  variablesSetImageFromURL,
	}

	Config = &cobra.Command{
		Use: "config",
	}

	ConfigGet = &cobra.Command{
		Use:  "get",
		Args: cobra.ExactArgs(0),
		Run:  configGet,
	}

	PlatformEvents = &cobra.Command{
		Use: "chat",
	}

	PlatformEventsListen = &cobra.Command{
		Use:  "listen",
		Args: cobra.ExactArgs(0),
		Run:  chatListen,
	}

	PlatformEventsInject = &cobra.Command{
		Use:  "inject",
		Args: cobra.ExactArgs(3),
		Run:  platformEventsInject,
	}

	LoggerLevel = logger.LevelWarning
)

func init() {
	Root.AddCommand(Stream)
	Stream.AddCommand(StreamSetup)
	Stream.AddCommand(StreamStatus)

	Root.AddCommand(Variables)
	Variables.AddCommand(VariablesGet)
	Variables.AddCommand(VariablesGetHash)
	Variables.AddCommand(VariablesSet)
	Variables.AddCommand(VariablesSetScreenshotFromURL)

	Root.AddCommand(Config)
	Config.AddCommand(ConfigGet)

	Root.AddCommand(PlatformEvents)
	PlatformEvents.AddCommand(PlatformEventsListen)
	PlatformEvents.AddCommand(PlatformEventsInject)
	PlatformEventsInject.Flags().Bool("is-live", true, "mark event as live")
	PlatformEventsInject.Flags().Bool("is-persistent", false, "mark event as persistent")

	Root.PersistentFlags().Var(&LoggerLevel, "log-level", "")
	Root.PersistentFlags().String("remote-addr", "localhost:3594", "the path to the config file")
	Root.PersistentFlags().String("go-net-pprof-addr", "", "address to listen to for net/pprof requests")

	StreamSetup.PersistentFlags().String("title", "", "stream title")
	StreamSetup.PersistentFlags().String("description", "", "stream description")
	StreamSetup.PersistentFlags().String("profile", "", "profile")
	StreamStatus.PersistentFlags().Bool("json", false, "use JSON output format")
}
func assertNoError(ctx context.Context, err error) {
	if err != nil {
		logger.Panic(ctx, err)
	}
}

func streamSetup(cmd *cobra.Command, args []string) {
	panic("not implemented")
}

func streamStatus(cmd *cobra.Command, args []string) {
	ctx := cmd.Context()

	isJSON, err := cmd.Flags().GetBool("json")
	assertNoError(ctx, err)

	remoteAddr, err := cmd.Flags().GetString("remote-addr")
	assertNoError(ctx, err)

	streamD, err := client.New(ctx, remoteAddr)
	assertNoError(ctx, err)

	result := map[streamcontrol.PlatformID]*streamcontrol.StreamStatus{}
	for _, platID := range streamcontrol.GetPlatformIDs() {
		isEnabled, err := streamD.IsBackendEnabled(ctx, platID)
		assertNoError(ctx, err)

		if !isEnabled {
			continue
		}

		status, err := streamD.GetStreamStatus(ctx, streamcontrol.NewStreamIDFullyQualified(platID, streamcontrol.DefaultAccountID, streamcontrol.DefaultStreamID))
		assertNoError(ctx, err)

		result[platID] = status
	}

	if isJSON {
		b, err := json.Marshal(result)
		assertNoError(ctx, err)
		fmt.Printf("%s\n", b)
	} else {
		for _, platID := range streamcontrol.GetPlatformIDs() {
			status, ok := result[platID]
			if !ok {
				continue
			}
			statusJSON, err := json.Marshal(status)
			assertNoError(ctx, err)

			fmt.Printf("%10s: %s\n", platID, statusJSON)
		}
	}
}

func variablesGet(cmd *cobra.Command, args []string) {
	variableKey := args[0]
	ctx := cmd.Context()

	remoteAddr, err := cmd.Flags().GetString("remote-addr")
	assertNoError(ctx, err)
	streamD, err := client.New(ctx, remoteAddr)
	assertNoError(ctx, err)

	b, err := streamD.GetVariable(ctx, consts.VarKey(variableKey))
	assertNoError(ctx, err)

	_, err = io.Copy(os.Stdout, bytes.NewReader(b))
	assertNoError(ctx, err)
}

func variablesGetHash(cmd *cobra.Command, args []string) {
	variableKey := args[0]
	ctx := cmd.Context()

	remoteAddr, err := cmd.Flags().GetString("remote-addr")
	assertNoError(ctx, err)
	streamD, err := client.New(ctx, remoteAddr)
	assertNoError(ctx, err)

	b, err := streamD.GetVariableHash(ctx, consts.VarKey(variableKey), crypto.SHA1)
	assertNoError(ctx, err)

	fmt.Printf("%X\n", b)
}

func variablesSet(cmd *cobra.Command, args []string) {
	variableKey := args[0]
	ctx := cmd.Context()

	remoteAddr, err := cmd.Flags().GetString("remote-addr")
	assertNoError(ctx, err)
	streamD, err := client.New(ctx, remoteAddr)
	assertNoError(ctx, err)

	value, err := io.ReadAll(os.Stdin)
	assertNoError(ctx, err)

	err = streamD.SetVariable(ctx, consts.VarKey(variableKey), value)
	assertNoError(ctx, err)
}

func variablesSetImageFromURL(cmd *cobra.Command, args []string) {
	ctx := xcontext.DetachDone(cmd.Context())
	imageKey := consts.ImageID(args[0])
	fps, err := strconv.ParseFloat(args[1], 64)
	assertNoError(ctx, err)

	remoteAddr, err := cmd.Flags().GetString("remote-addr")
	assertNoError(ctx, err)

	streamD, err := client.New(ctx, remoteAddr)
	assertNoError(ctx, err)

	var playerMap xsync.Map[*videodecoder.Decoder, int]

	imageRenderer := newScreenshotSender(streamD, consts.VarKeyImage(imageKey), fps, &playerMap)
	defer imageRenderer.Close()

	var cancelFn context.CancelFunc
	ctx, cancelFn = context.WithCancel(ctx)
	defer cancelFn()

	var wg sync.WaitGroup
	for idx, url := range args[2:] {
		idx, url := idx, url
		wg.Add(1)
		observability.Go(ctx, func(ctx context.Context) {
			defer wg.Done()
			for {
				func() {
					p := videodecoder.New(ctx, imageRenderer, nil)
					defer p.Close(ctx)

					playerMap.Store(p, idx)

					err = p.OpenURL(ctx, url)
					if err != nil {
						logger.Errorf(ctx, "player.OpenURL(%q) returned: %v", url, err)
						time.Sleep(time.Second)
						return
					}

					ch, err := p.EndChan(ctx)
					assertNoError(ctx, err)

					<-ch
				}()
			}
		})
	}

	wg.Wait()
}

func configGet(cmd *cobra.Command, args []string) {
	ctx := cmd.Context()

	remoteAddr, err := cmd.Flags().GetString("remote-addr")
	assertNoError(ctx, err)
	streamD, err := client.New(ctx, remoteAddr)
	assertNoError(ctx, err)

	cfg, err := streamD.GetConfig(ctx)
	assertNoError(ctx, err)

	cfg.WriteTo(os.Stdout)
}

func chatListen(cmd *cobra.Command, args []string) {
	ctx := cmd.Context()

	remoteAddr, err := cmd.Flags().GetString("remote-addr")
	assertNoError(ctx, err)
	streamD, err := client.New(ctx, remoteAddr)
	assertNoError(ctx, err)

	fmt.Println("subscribing...")
	ch, err := streamD.SubscribeToChatMessages(ctx, time.Now().Add(-time.Hour*24*3), 1000)
	assertNoError(ctx, err)

	fmt.Println("started listening...")
	for ev := range ch {
		spew.Dump(ev)
	}
}

func platformEventsInject(cmd *cobra.Command, args []string) {
	ctx := cmd.Context()

	remoteAddr, err := cmd.Flags().GetString("remote-addr")
	assertNoError(ctx, err)
	streamD, err := client.New(ctx, remoteAddr)
	assertNoError(ctx, err)

	// args: <platform-id> <user> <message>
	platID := streamcontrol.PlatformID(args[0])
	userName := args[1]
	message := args[2]

	isLive, err := cmd.Flags().GetBool("is-live")
	assertNoError(ctx, err)
	isPersistent, err := cmd.Flags().GetBool("is-persistent")
	assertNoError(ctx, err)

	user := streamcontrol.User{
		ID:   streamcontrol.UserID(userName),
		Slug: userName,
		Name: userName,
	}

	err = streamD.InjectPlatformEvent(ctx, platID, isLive, isPersistent, user, message)
	assertNoError(ctx, err)
}
