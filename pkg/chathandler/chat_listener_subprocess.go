package chathandler

import (
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
)

// Flag constants for chat listener subprocess arguments.
// Used by StartExternalChatHandler (pkg/streamd) to build command args
// and by pkg/chathandler/process to parse them.
const (
	FlagChatListenerMode        = "internal-chat-listener"
	FlagChatListenerPlatform    = "internal-chat-listener-platform"
	FlagChatListenerType        = "internal-chat-listener-type"
	FlagChatListenerStreamdAddr = "internal-chat-listener-streamd-addr"
	// FlagChatListenerLogLevel propagates the parent's current log level
	// (observability.LogLevelFilter.GetLevel()) to the subprocess so debug
	// output matches between streamd and its chat listener children.
	// Optional; subprocess defaults to LevelInfo when absent or invalid.
	FlagChatListenerLogLevel = "internal-chat-listener-log-level"
	// FlagChatListenerLogstashAddr forwards the parent streamd's
	// --logstash-addr to the chat listener subprocess so its logs ship
	// to the same logstash sink as the parent's. Optional; absent or
	// empty disables logstash forwarding for this child. The address
	// appears in argv (visible in `ps`) — never embed credentials.
	FlagChatListenerLogstashAddr = "internal-chat-listener-logstash-addr"
)

// BuildChatListenerArgs assembles the argv passed to the streamd executable
// when re-spawning it as an internal chat listener subprocess. Centralizing
// this lets the producer (StartExternalChatHandler) and a unit test agree on
// the exact format without duplicating string constants.
//
// logstashAddr forwards the parent's --logstash-addr to the child so its
// logs ship to the same sink. Empty values are OMITTED from argv (no
// `--... ""` pair) so the child's CtxWithLogstash hookup stays gated on
// presence, not on emptiness.
func BuildChatListenerArgs(
	platName streamcontrol.PlatformName,
	listenerType streamcontrol.ChatListenerType,
	streamdAddr string,
	level logger.Level,
	logstashAddr string,
) []string {
	args := []string{
		"--" + FlagChatListenerMode,
		"--" + FlagChatListenerPlatform, string(platName),
		"--" + FlagChatListenerType, listenerType.String(),
		"--" + FlagChatListenerStreamdAddr, streamdAddr,
		"--" + FlagChatListenerLogLevel, level.String(),
	}
	if logstashAddr != "" {
		args = append(args, "--"+FlagChatListenerLogstashAddr, logstashAddr)
	}
	return args
}
