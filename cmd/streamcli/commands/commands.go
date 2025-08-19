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
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/spf13/cobra"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	kick "github.com/xaionaro-go/streamctl/pkg/streamcontrol/kick/types"
	obs "github.com/xaionaro-go/streamctl/pkg/streamcontrol/obs/types"
	twitch "github.com/xaionaro-go/streamctl/pkg/streamcontrol/twitch/types"
	youtube "github.com/xaionaro-go/streamctl/pkg/streamcontrol/youtube/types"
	"github.com/xaionaro-go/streamctl/pkg/streamd/client"
	"github.com/xaionaro-go/streamctl/pkg/streampanel/consts"
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

	Config = &cobra.Command{
		Use: "config",
	}

	ConfigGet = &cobra.Command{
		Use:  "get",
		Args: cobra.ExactArgs(0),
		Run:  configGet,
	}

	Chat = &cobra.Command{
		Use: "chat",
	}

	ChatListen = &cobra.Command{
		Use:  "listen",
		Args: cobra.ExactArgs(0),
		Run:  chatListen,
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

	Root.AddCommand(Config)
	Config.AddCommand(ConfigGet)

	Root.AddCommand(Chat)
	Chat.AddCommand(ChatListen)

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
	ctx := cmd.Context()

	remoteAddr, err := cmd.Flags().GetString("remote-addr")
	assertNoError(ctx, err)
	streamD, err := client.New(ctx, remoteAddr)
	assertNoError(ctx, err)
	title, err := cmd.Flags().GetString("title")
	assertNoError(ctx, err)
	description, err := cmd.Flags().GetString("description")
	assertNoError(ctx, err)
	_profileName, err := cmd.Flags().GetString("profile")
	assertNoError(ctx, err)
	profileName := streamcontrol.ProfileName(_profileName)
	logger.Debugf(
		ctx,
		"title == '%s'; description == '%s'; profile == '%s'",
		title, description, profileName,
	)

	isEnabled := map[streamcontrol.PlatformName]bool{}
	for _, platID := range []streamcontrol.PlatformName{
		twitch.ID, youtube.ID,
	} {
		_isEnabled, err := streamD.IsBackendEnabled(ctx, platID)
		assertNoError(ctx, err)
		isEnabled[platID] = _isEnabled
	}

	cfg, err := streamD.GetConfig(ctx)
	assertNoError(ctx, err)

	if isEnabled[youtube.ID] {
		err := streamD.StartStream(
			ctx,
			youtube.ID,
			title,
			description,
			cfg.Backends[youtube.ID].StreamProfiles[profileName],
		)
		assertNoError(ctx, err)
	}

	if isEnabled[twitch.ID] {
		err := streamD.StartStream(
			ctx,
			twitch.ID,
			title,
			description,
			cfg.Backends[twitch.ID].StreamProfiles[profileName],
		)
		assertNoError(ctx, err)
	}
}

func streamStatus(cmd *cobra.Command, args []string) {
	ctx := cmd.Context()

	isJSON, err := cmd.Flags().GetBool("json")
	assertNoError(ctx, err)

	remoteAddr, err := cmd.Flags().GetString("remote-addr")
	assertNoError(ctx, err)

	streamD, err := client.New(ctx, remoteAddr)
	assertNoError(ctx, err)

	result := map[streamcontrol.PlatformName]*streamcontrol.StreamStatus{}
	for _, platID := range []streamcontrol.PlatformName{
		obs.ID, twitch.ID, youtube.ID, kick.ID,
	} {
		isEnabled, err := streamD.IsBackendEnabled(ctx, platID)
		assertNoError(ctx, err)

		if !isEnabled {
			continue
		}

		status, err := streamD.GetStreamStatus(ctx, platID)
		assertNoError(ctx, err)

		result[platID] = status
	}

	if isJSON {
		b, err := json.Marshal(result)
		assertNoError(ctx, err)
		fmt.Printf("%s\n", b)
	} else {
		for _, platID := range []streamcontrol.PlatformName{
			obs.ID, twitch.ID, youtube.ID,
		} {
			statusJSON, err := json.Marshal(result[platID])
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
