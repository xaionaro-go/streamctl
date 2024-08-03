package main

import (
	"context"
	"fmt"
	"runtime"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/spf13/pflag"
	"github.com/xaionaro-go/streamctl/pkg/mainprocess"
	"github.com/xaionaro-go/streamctl/pkg/streampanel/consts"
)

type Flags struct {
	LoggerLevel         logger.Level
	ListenAddr          string
	RemoteAddr          string
	ConfigPath          string
	NetPprofAddrMain    string
	NetPprofAddrUI      string
	NetPprofAddrStreamD string
	CPUProfile          string
	HeapProfile         string
	LogstashAddr        string
	SentryDSN           string
	Page                string
	LogFile             string
	Subprocess          string
	SplitProcess        bool
}

var platformGetFlagsFuncs []func(*Flags)

func parseFlags() Flags {
	var loggerLevel logger.Level
	var defaultLogFile string
	if ForceDebug {
		loggerLevel = logger.LevelTrace
		switch runtime.GOOS {
		case "android":
			defaultLogFile = "~/trace.log"
		}
	} else {
		loggerLevel = logger.LevelWarning
		defaultLogFile = ""
	}
	pflag.Var(&loggerLevel, "log-level", "Log level")
	listenAddr := pflag.String("listen-addr", "", "the address to listen for incoming connections to")
	remoteAddr := pflag.String("remote-addr", "", "the address (for example 127.0.0.1:3594) of streamd to connect to, instead of running the stream controllers locally")
	configPath := pflag.String("config-path", "~/.streampanel.yaml", "the path to the config file")
	netPprofAddrMain := pflag.String("go-net-pprof-addr-main", "", "address to listen to for net/pprof requests by the main process")
	netPprofAddrUI := pflag.String("go-net-pprof-addr-ui", "", "address to listen to for net/pprof requests by the UI process")
	netPprofAddrStreamD := pflag.String("go-net-pprof-addr-streamd", "", "address to listen to for net/pprof requests by the streamd process")
	cpuProfile := pflag.String("go-profile-cpu", "", "file to write cpu profile to")
	heapProfile := pflag.String("go-profile-heap", "", "file to write memory profile to")
	logstashAddr := pflag.String("logstash-addr", "", "the address of logstash to send logs to (for example: 'tcp://192.168.0.2:5044')")
	sentryDSN := pflag.String("sentry-dsn", "", "DSN of a Sentry instance to send error reports")
	page := pflag.String("page", string(consts.PageControl), "DSN of a Sentry instance to send error reports")
	logFile := pflag.String("log-file", defaultLogFile, "log file to write logs into")
	subprocess := pflag.String("subprocess", "", "[internal use flag] run a specific sub-process (format: processName:addressToConnect)")
	splitProcess := pflag.Bool("split-process", !isMobile(), "split the process into multiple processes for better stability")
	pflag.Parse()

	flags := Flags{
		LoggerLevel:         loggerLevel,
		ListenAddr:          *listenAddr,
		RemoteAddr:          *remoteAddr,
		ConfigPath:          *configPath,
		NetPprofAddrMain:    *netPprofAddrMain,
		NetPprofAddrUI:      *netPprofAddrUI,
		NetPprofAddrStreamD: *netPprofAddrStreamD,
		CPUProfile:          *cpuProfile,
		HeapProfile:         *heapProfile,
		LogstashAddr:        *logstashAddr,
		SentryDSN:           *sentryDSN,
		Page:                *page,
		LogFile:             *logFile,
		Subprocess:          *subprocess,
		SplitProcess:        *splitProcess,
	}

	for _, platformGetFlagsFunc := range platformGetFlagsFuncs {
		platformGetFlagsFunc(&flags)
	}

	return flags
}

type GetFlags struct{}
type GetFlagsResult struct {
	Flags Flags
}

func getFlags(
	ctx context.Context,
	mainProcess *mainprocess.Client,
) Flags {
	err := mainProcess.SendMessage(ctx, ProcessNameMain, GetFlags{})
	assertNoError(err)

	var flags Flags
	err = mainProcess.ReadOne(
		ctx,
		func(ctx context.Context, source mainprocess.ProcessName, content any) error {
			result, ok := content.(GetFlagsResult)
			if !ok {
				return fmt.Errorf("got unexpected type '%T' instead of %T", content, GetFlagsResult{})
			}
			flags = result.Flags
			return nil
		},
	)
	assertNoError(err)

	return flags
}
