package main

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"time"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/spf13/pflag"
	"github.com/xaionaro-go/streamctl/pkg/mainprocess"
	"github.com/xaionaro-go/streamctl/pkg/streampanel/consts"
)

type loggerLevel logger.Level

func (l *loggerLevel) UnmarshalYAML(b []byte) error {
	var r logger.Level
	r.Set(strings.Trim(string(b), " "))
	if r == logger.LevelUndefined {
		return fmt.Errorf("unexpected logger level '%s'", b)
	}
	*l = loggerLevel(r)
	return nil
}

type Flags struct {
	LoggerLevel         loggerLevel   `yaml:"LoggerLevel,omitempty"`
	ListenAddr          string        `yaml:"ListenAddr,omitempty"`
	RemoteAddr          string        `yaml:"RemoteAddr,omitempty"`
	ConfigPath          string        `yaml:"ConfigPath,omitempty"`
	NetPprofAddrMain    string        `yaml:"NetPprofAddrMain,omitempty"`
	NetPprofAddrUI      string        `yaml:"NetPprofAddrUI,omitempty"`
	NetPprofAddrStreamD string        `yaml:"NetPprofAddrStreamD,omitempty"`
	CPUProfile          string        `yaml:"CPUProfile,omitempty"`
	HeapProfile         string        `yaml:"HeapProfile,omitempty"`
	LogstashAddr        string        `yaml:"LogstashAddr,omitempty"`
	SentryDSN           string        `yaml:"SentryDSN,omitempty"`
	Page                string        `yaml:"Page,omitempty"`
	LogFile             string        `yaml:"LogFile,omitempty"`
	Subprocess          string        `yaml:"Subprocess,omitempty"`
	SplitProcess        bool          `yaml:"SplitProcess,omitempty"`
	LockTimeout         time.Duration `yaml:"LockTimeout,omitempty"`

	OAuthListenPortTwitch  uint16 `yaml:"OAuthListenPortTwitch,omitempty"`
	OAuthListenPortYouTube uint16 `yaml:"OAuthListenPortYouTube,omitempty"`
}

var platformGetFlagsFuncs []func(*Flags)

func parseFlags() Flags {
	var loggerLevelValue logger.Level
	var defaultLogFile string
	if ForceDebug {
		loggerLevelValue = logger.LevelTrace
		switch runtime.GOOS {
		case "android":
			defaultLogFile = "~/trace.log"
		}
	} else {
		loggerLevelValue = logger.LevelWarning
		defaultLogFile = ""
	}
	pflag.Var(&loggerLevelValue, "log-level", "Log level")
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
	lockTimeout := pflag.Duration("lock-timeout", 2*time.Minute, "[debug option] change the timeout for locking, before reporting it as a deadlock")
	oauthListenPortTwitch := pflag.Uint16("oauth-listen-port-twitch", 8091, "the port that is used for OAuth callbacks while authenticating in Twitch")
	oauthListenPortYouTube := pflag.Uint16("oauth-listen-port-youtube", 8092, "the port that is used for OAuth callbacks while authenticating in YouTube")

	pflag.Parse()

	flags := Flags{
		LoggerLevel:         loggerLevel(loggerLevelValue),
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
		LockTimeout:         *lockTimeout,

		OAuthListenPortTwitch:  *oauthListenPortTwitch,
		OAuthListenPortYouTube: *oauthListenPortYouTube,
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
