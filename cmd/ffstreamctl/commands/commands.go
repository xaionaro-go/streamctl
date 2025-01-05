package commands

import (
	"context"
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"os"
	"strconv"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/spf13/cobra"
	"github.com/xaionaro-go/streamctl/pkg/ffstream"
	"github.com/xaionaro-go/streamctl/pkg/ffstreamserver/client"
	"github.com/xaionaro-go/streamctl/pkg/observability"
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
				observability.Go(ctx, func() {
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

	Stats = &cobra.Command{
		Use: "stats",
	}

	StatsEncoder = &cobra.Command{
		Use:  "encoder",
		Args: cobra.ExactArgs(0),
		Run:  statsEncoder,
	}

	StatsOutputSRT = &cobra.Command{
		Use:  "output_srt",
		Args: cobra.ExactArgs(0),
		Run:  statsOutputSRT,
	}

	Flag = &cobra.Command{
		Use: "flag",
	}

	FlagInt = &cobra.Command{
		Use: "int",
	}

	FlagIntGet = &cobra.Command{
		Use:  "get",
		Args: cobra.ExactArgs(1),
		Run:  flagIntGet,
	}

	FlagIntSet = &cobra.Command{
		Use:  "set",
		Args: cobra.ExactArgs(2),
		Run:  flagIntSet,
	}

	EncoderConfig = &cobra.Command{
		Use: "encoder_config",
	}

	EncoderConfigGet = &cobra.Command{
		Use:  "get",
		Args: cobra.ExactArgs(0),
		Run:  encoderConfigGet,
	}

	EncoderConfigSet = &cobra.Command{
		Use:  "set",
		Args: cobra.ExactArgs(0),
		Run:  encoderConfigSet,
	}

	LoggerLevel = logger.LevelWarning
)

func init() {
	Root.AddCommand(Stats)
	Stats.AddCommand(StatsEncoder)
	Stats.AddCommand(StatsOutputSRT)

	Root.AddCommand(Flag)
	Flag.AddCommand(FlagInt)
	FlagInt.AddCommand(FlagIntGet)
	FlagInt.AddCommand(FlagIntSet)

	Root.AddCommand(EncoderConfig)
	EncoderConfig.AddCommand(EncoderConfigGet)
	EncoderConfig.AddCommand(EncoderConfigSet)

	Root.PersistentFlags().Var(&LoggerLevel, "log-level", "")
	Root.PersistentFlags().String("remote-addr", "localhost:3594", "the path to the config file")
	Root.PersistentFlags().String("go-net-pprof-addr", "", "address to listen to for net/pprof requests")

	StatsEncoder.PersistentFlags().String("title", "", "stream title")
	StatsEncoder.PersistentFlags().String("description", "", "stream description")
	StatsEncoder.PersistentFlags().String("profile", "", "profile")
	StatsOutputSRT.PersistentFlags().Bool("json", false, "use JSON output format")
}
func assertNoError(ctx context.Context, err error) {
	if err != nil {
		logger.Panic(ctx, err)
	}
}

func statsEncoder(cmd *cobra.Command, args []string) {
	ctx := cmd.Context()

	remoteAddr, err := cmd.Flags().GetString("remote-addr")
	assertNoError(ctx, err)

	client := client.New(remoteAddr)

	stats, err := client.GetEncoderStats(ctx)
	assertNoError(ctx, err)

	jsonOutput(ctx, cmd.OutOrStdout(), stats)
}

func statsOutputSRT(cmd *cobra.Command, args []string) {
	ctx := cmd.Context()

	remoteAddr, err := cmd.Flags().GetString("remote-addr")
	assertNoError(ctx, err)

	client := client.New(remoteAddr)

	stats, err := client.GetOutputSRTStats(ctx)
	assertNoError(ctx, err)

	jsonOutput(ctx, cmd.OutOrStdout(), stats)
}

func flagIntGet(cmd *cobra.Command, args []string) {
	ctx := cmd.Context()

	flagID, err := srtFlagNameToID(args[0])
	assertNoError(ctx, err)

	remoteAddr, err := cmd.Flags().GetString("remote-addr")
	assertNoError(ctx, err)

	client := client.New(remoteAddr)

	value, err := client.GetFlagInt(ctx, flagID)
	assertNoError(ctx, err)

	fmt.Fprintf(cmd.OutOrStdout(), "%d", value)
}

func flagIntSet(cmd *cobra.Command, args []string) {
	ctx := cmd.Context()

	flagID, err := srtFlagNameToID(args[0])
	assertNoError(ctx, err)

	value, err := strconv.ParseInt(args[1], 10, 64)
	assertNoError(ctx, err)

	remoteAddr, err := cmd.Flags().GetString("remote-addr")
	assertNoError(ctx, err)

	client := client.New(remoteAddr)

	err = client.SetFlagInt(ctx, flagID, value)
	assertNoError(ctx, err)
}

func encoderConfigGet(cmd *cobra.Command, args []string) {
	ctx := cmd.Context()

	remoteAddr, err := cmd.Flags().GetString("remote-addr")
	assertNoError(ctx, err)

	client := client.New(remoteAddr)

	cfg, err := client.GetEncoderConfig(ctx)
	assertNoError(ctx, err)

	jsonOutput(ctx, cmd.OutOrStdout(), cfg)
}

func encoderConfigSet(cmd *cobra.Command, args []string) {
	ctx := cmd.Context()

	cfg := jsonInput[ffstream.EncoderConfig](ctx, cmd.InOrStdin())

	remoteAddr, err := cmd.Flags().GetString("remote-addr")
	assertNoError(ctx, err)
	client := client.New(remoteAddr)

	err = client.SetEncoderConfig(ctx, cfg)
	assertNoError(ctx, err)
}
