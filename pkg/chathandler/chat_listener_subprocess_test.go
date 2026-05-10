package chathandler

import (
	"testing"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
)

func TestBuildChatListenerArgs(t *testing.T) {
	const platName = streamcontrol.PlatformName("youtube")
	const streamdAddr = "127.0.0.1:3594"

	got := BuildChatListenerArgs(platName, streamcontrol.ChatListenerPrimary, streamdAddr, logger.LevelTrace, "")

	require.Equal(t, []string{
		"--internal-chat-listener",
		"--internal-chat-listener-platform", "youtube",
		"--internal-chat-listener-type", "primary",
		"--internal-chat-listener-streamd-addr", "127.0.0.1:3594",
		"--internal-chat-listener-log-level", "trace",
	}, got)
}

// TestBuildChatListenerArgs_WithLogstashAddr locks in the contract that a
// non-empty logstashAddr is emitted as the trailing flag/value pair in argv.
// The child gates its CtxWithLogstash hookup on this flag's presence, so a
// regression that drops or misorders the pair would silently disable
// logstash forwarding for the chat listener subprocess.
func TestBuildChatListenerArgs_WithLogstashAddr(t *testing.T) {
	const platName = streamcontrol.PlatformName("youtube")
	const streamdAddr = "127.0.0.1:3594"
	const logstashAddr = "tcp://logstash.example:5044"

	got := BuildChatListenerArgs(platName, streamcontrol.ChatListenerPrimary, streamdAddr, logger.LevelTrace, logstashAddr)

	require.Equal(t, []string{
		"--internal-chat-listener",
		"--internal-chat-listener-platform", "youtube",
		"--internal-chat-listener-type", "primary",
		"--internal-chat-listener-streamd-addr", "127.0.0.1:3594",
		"--internal-chat-listener-log-level", "trace",
		"--internal-chat-listener-logstash-addr", logstashAddr,
	}, got)

	// Belt-and-suspenders: assert the flag/value pair sits at the very
	// end of argv (the position the supervisor relies on so the
	// optional flag never collides with positional consumers).
	require.GreaterOrEqual(t, len(got), 2)
	assert.Equal(t, "--"+FlagChatListenerLogstashAddr, got[len(got)-2])
	assert.Equal(t, logstashAddr, got[len(got)-1])
}

// TestBuildChatListenerArgs_LevelPropagation locks in the contract that the
// log level emitted in argv matches the level argument byte-for-byte. A
// regression here would silently drop the parent's verbosity (the bug this
// helper exists to prevent).
func TestBuildChatListenerArgs_LevelPropagation(t *testing.T) {
	for _, level := range []logger.Level{
		logger.LevelTrace,
		logger.LevelDebug,
		logger.LevelInfo,
		logger.LevelWarning,
		logger.LevelError,
		logger.LevelPanic,
		logger.LevelFatal,
	} {
		t.Run(level.String(), func(t *testing.T) {
			args := BuildChatListenerArgs("youtube", streamcontrol.ChatListenerPrimary, "x:1", level, "")

			// Locate the log-level flag and assert its argument matches.
			flag := "--" + FlagChatListenerLogLevel
			var found bool
			for i := 0; i < len(args)-1; i++ {
				if args[i] == flag {
					assert.Equal(t, level.String(), args[i+1],
						"emitted level argument must match input level")
					found = true
					break
				}
			}
			require.True(t, found, "log-level flag must be present in argv")
		})
	}
}
