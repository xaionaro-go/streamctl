package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/spf13/cobra"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	obs "github.com/xaionaro-go/streamctl/pkg/streamcontrol/obs/types"
	twitch "github.com/xaionaro-go/streamctl/pkg/streamcontrol/twitch/types"
	youtube "github.com/xaionaro-go/streamctl/pkg/streamcontrol/youtube/types"
	"github.com/xaionaro-go/streamctl/pkg/streamd/client"
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
		},
		PersistentPostRun: func(cmd *cobra.Command, args []string) {
			ctx := cmd.Context()
			logger.Debug(ctx, "end")
		},
	}

	StreamSetup = &cobra.Command{
		Use:  "stream-setup",
		Args: cobra.ExactArgs(0),
		Run:  streamSetup,
	}

	StreamStatus = &cobra.Command{
		Use:  "stream-status",
		Args: cobra.ExactArgs(0),
		Run:  streamStatus,
	}

	LoggerLevel = logger.LevelWarning
)

func init() {
	Root.AddCommand(StreamSetup)
	Root.AddCommand(StreamStatus)

	Root.PersistentFlags().Var(&LoggerLevel, "log-level", "")
	Root.PersistentFlags().String("remote-addr", "localhost:3594", "the path to the config file")
	StreamSetup.PersistentFlags().String("title", "", "stream title")
	StreamSetup.PersistentFlags().String("description", "", "stream description")
	StreamSetup.PersistentFlags().String("profile", "", "profile")
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
	streamD := client.New(remoteAddr)
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
		err := streamD.StartStream(ctx, youtube.ID, title, description, cfg.Backends[youtube.ID].StreamProfiles[profileName])
		assertNoError(ctx, err)
	}

	if isEnabled[twitch.ID] {
		err := streamD.StartStream(ctx, twitch.ID, title, description, cfg.Backends[twitch.ID].StreamProfiles[profileName])
		assertNoError(ctx, err)
	}
}

func streamStatus(cmd *cobra.Command, args []string) {
	ctx := cmd.Context()

	remoteAddr, err := cmd.Flags().GetString("remote-addr")
	assertNoError(ctx, err)
	streamD := client.New(remoteAddr)

	for _, platID := range []streamcontrol.PlatformName{
		obs.ID, twitch.ID, youtube.ID,
	} {
		isEnabled, err := streamD.IsBackendEnabled(ctx, platID)
		assertNoError(ctx, err)

		if !isEnabled {
			continue
		}

		status, err := streamD.GetStreamStatus(ctx, platID)
		assertNoError(ctx, err)

		statusJSON, err := json.Marshal(status)
		assertNoError(ctx, err)

		fmt.Printf("%10s: %s\n", platID, statusJSON)
	}
}
