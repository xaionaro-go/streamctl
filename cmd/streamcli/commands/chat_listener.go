package commands

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/spf13/cobra"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamd/client"
)

var (
	ChatListener = &cobra.Command{
		Use:   "listener",
		Short: "Manage chat listener types per platform",
	}

	ChatListenerList = &cobra.Command{
		Use:   "list <platform>",
		Short: "List all chat listener types and their status",
		Args:  cobra.ExactArgs(1),
		Run:   chatListenerList,
	}

	ChatListenerEnable = &cobra.Command{
		Use:   "enable <platform> <type>",
		Short: "Enable a chat listener type for a platform",
		Args:  cobra.ExactArgs(2),
		Run:   chatListenerEnable,
	}

	ChatListenerDisable = &cobra.Command{
		Use:   "disable <platform> <type>",
		Short: "Disable a chat listener type for a platform",
		Args:  cobra.ExactArgs(2),
		Run:   chatListenerDisable,
	}
)

func init() {
	ChatListener.AddCommand(ChatListenerList)
	ChatListener.AddCommand(ChatListenerEnable)
	ChatListener.AddCommand(ChatListenerDisable)
	Chat.AddCommand(ChatListener)
}

func isListenerTypeEnabled(
	enabledTypes []streamcontrol.ChatListenerType,
	t streamcontrol.ChatListenerType,
) bool {
	// nil means default (only primary enabled).
	if enabledTypes == nil {
		return t == streamcontrol.ChatListenerPrimary
	}
	for _, et := range enabledTypes {
		if et == t {
			return true
		}
	}
	return false
}

func newStreamDClient(cmd *cobra.Command) *client.Client {
	ctx := cmd.Context()
	remoteAddr, err := cmd.Flags().GetString("remote-addr")
	assertNoError(ctx, err)
	streamD, err := client.New(ctx, remoteAddr)
	assertNoError(ctx, err)
	return streamD
}

func chatListenerList(cmd *cobra.Command, args []string) {
	ctx := cmd.Context()
	streamD := newStreamDClient(cmd)
	platName := streamcontrol.PlatformName(args[0])

	cfg, err := streamD.GetConfig(ctx)
	assertNoError(ctx, err)

	platCfg := cfg.Backends[platName]
	if platCfg == nil {
		logger.Fatalf(ctx, "platform %q not found in config", platName)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 1, ' ', 0)
	for t := range streamcontrol.EndOfChatListenerType {
		status := "disabled"
		if isListenerTypeEnabled(platCfg.EnabledChatListenerTypes, t) {
			status = "enabled"
		}
		fmt.Fprintf(w, "%s\t%s\n", t, status)
	}
	w.Flush()
}

func chatListenerEnable(cmd *cobra.Command, args []string) {
	ctx := cmd.Context()
	streamD := newStreamDClient(cmd)
	platName := streamcontrol.PlatformName(args[0])

	cfg, err := streamD.GetConfig(ctx)
	assertNoError(ctx, err)

	platCfg := cfg.Backends[platName]
	if platCfg == nil {
		logger.Fatalf(ctx, "platform %q not found in config", platName)
	}

	t, err := streamcontrol.ChatListenerTypeFromString(args[1])
	assertNoError(ctx, err)

	if isListenerTypeEnabled(platCfg.EnabledChatListenerTypes, t) {
		logger.Infof(ctx, "listener type %q is already enabled for %q", t, platName)
		return
	}

	// Materialize nil (default) into an explicit list before adding.
	if platCfg.EnabledChatListenerTypes == nil {
		platCfg.EnabledChatListenerTypes = []streamcontrol.ChatListenerType{
			streamcontrol.ChatListenerPrimary,
		}
	}

	platCfg.EnabledChatListenerTypes = append(platCfg.EnabledChatListenerTypes, t)

	err = streamD.SetConfig(ctx, cfg)
	assertNoError(ctx, err)
	err = streamD.SaveConfig(ctx)
	assertNoError(ctx, err)
	fmt.Printf("enabled %s for %s\n", t, platName)
}

func chatListenerDisable(cmd *cobra.Command, args []string) {
	ctx := cmd.Context()
	streamD := newStreamDClient(cmd)
	platName := streamcontrol.PlatformName(args[0])

	cfg, err := streamD.GetConfig(ctx)
	assertNoError(ctx, err)

	platCfg := cfg.Backends[platName]
	if platCfg == nil {
		logger.Fatalf(ctx, "platform %q not found in config", platName)
	}

	t, err := streamcontrol.ChatListenerTypeFromString(args[1])
	assertNoError(ctx, err)

	if !isListenerTypeEnabled(platCfg.EnabledChatListenerTypes, t) {
		logger.Infof(ctx, "listener type %q is already disabled for %q", t, platName)
		return
	}

	// Materialize nil (default) into an explicit list before removing.
	if platCfg.EnabledChatListenerTypes == nil {
		platCfg.EnabledChatListenerTypes = []streamcontrol.ChatListenerType{
			streamcontrol.ChatListenerPrimary,
		}
	}

	filtered := platCfg.EnabledChatListenerTypes[:0]
	for _, et := range platCfg.EnabledChatListenerTypes {
		if et != t {
			filtered = append(filtered, et)
		}
	}
	platCfg.EnabledChatListenerTypes = filtered

	err = streamD.SetConfig(ctx, cfg)
	assertNoError(ctx, err)
	err = streamD.SaveConfig(ctx)
	assertNoError(ctx, err)
	fmt.Printf("disabled %s for %s\n", t, platName)
}
