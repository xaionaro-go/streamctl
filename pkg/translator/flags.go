// Package translator hosts the translator subprocess infrastructure:
// flag constants, the gRPC service contract (under grpc/), the subprocess
// entrypoint (under process/), the long-running runner (under runner/), and
// the server implementation (under server/).
//
// Flag constants and BuildTranslatorArgs live here at the package root so
// the producer (streamd's translator_subprocess.go) and the parser
// (process.init) agree on the wire format without circular imports.
package translator

import "github.com/facebookincubator/go-belt/tool/logger"

// Flag constants for translator subprocess arguments. Mirrors the chathandler
// pattern (pkg/chathandler/chat_listener_subprocess.go): one mode flag plus
// keyed parameters consumed by the subprocess init function before pflag
// parses anything else.
const (
	FlagTranslatorMode        = "internal-translator"
	FlagTranslatorSocketPath  = "internal-translator-socket"
	FlagTranslatorStreamdAddr = "internal-translator-streamd-addr"
	// FlagTranslatorLogLevel propagates the parent's current log level
	// (observability.LogLevelFilter.GetLevel()) to the subprocess so debug
	// output matches between streamd and its translator child. Optional;
	// subprocess defaults to LevelInfo when absent or invalid.
	FlagTranslatorLogLevel = "internal-translator-log-level"
	// FlagTranslatorLogstashAddr forwards the parent streamd's
	// --logstash-addr to the translator subprocess so its logs ship
	// to the same logstash sink as the parent's. Optional; absent or
	// empty disables logstash forwarding for this child. The address
	// appears in argv (visible in `ps`) — never embed credentials.
	FlagTranslatorLogstashAddr = "internal-translator-logstash-addr"
)

// BuildTranslatorArgs assembles the argv passed to the streamd executable
// when re-spawning it as the internal translator subprocess. Centralising
// this lets the producer (streamd-side supervisor) and the consumer
// (pkg/translator/process) agree on the exact format without duplicating
// string constants.
//
// logstashAddr forwards the parent's --logstash-addr to the child so its
// logs ship to the same sink. Empty values are OMITTED from argv (no
// `--... ""` pair) so the child's CtxWithLogstash hookup stays gated on
// presence, not on emptiness.
func BuildTranslatorArgs(
	socketPath string,
	streamdAddr string,
	level logger.Level,
	logstashAddr string,
) []string {
	args := []string{
		"--" + FlagTranslatorMode,
		"--" + FlagTranslatorSocketPath, socketPath,
		"--" + FlagTranslatorStreamdAddr, streamdAddr,
		"--" + FlagTranslatorLogLevel, level.String(),
	}
	if logstashAddr != "" {
		args = append(args, "--"+FlagTranslatorLogstashAddr, logstashAddr)
	}
	return args
}
