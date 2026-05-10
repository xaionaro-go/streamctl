// Package process registers the translator subprocess entrypoint.
// Blank-import this from any binary that spawns the translator (currently
// cmd/streamd). The init function intercepts --internal-translator before
// pflag.Parse runs so the subprocess executes before the main binary's
// flag parser rejects the unknown flags.
package process

import (
	"context"
	"os"
	"strings"

	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/subproclog"
	"github.com/xaionaro-go/streamctl/pkg/translator"
	"github.com/xaionaro-go/streamctl/pkg/translator/runner"
)

func init() {
	if !hasFlag(translator.FlagTranslatorMode) {
		return
	}

	ctx := subproclog.Setup(
		context.Background(),
		flagValue(translator.FlagTranslatorLogLevel),
		flagValue(translator.FlagTranslatorLogstashAddr),
		"streamd-translator",
	)
	defer belt.Flush(ctx)

	// Pass the original argv (minus the program name) so the runner can
	// read its own flags. The mode flag is harmless — flagValue ignores
	// non-matching tokens.
	err := runner.Run(ctx, os.Args[1:])
	if err != nil {
		logger.Errorf(ctx, "translator subprocess failed: %v", err)
		os.Exit(1)
	}
	os.Exit(0)
}

func hasFlag(name string) bool {
	target := "--" + name
	for _, arg := range os.Args[1:] {
		if arg == target || strings.HasPrefix(arg, target+"=") {
			return true
		}
	}
	return false
}

func flagValue(name string) string {
	target := "--" + name
	for i, arg := range os.Args[1:] {
		switch {
		case strings.HasPrefix(arg, target+"="):
			return arg[len(target)+1:]
		case arg == target && i+1 < len(os.Args[1:]):
			return os.Args[i+2]
		}
	}
	return ""
}
