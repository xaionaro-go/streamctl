package main

import (
	"bytes"
	"context"
	"fmt"
	_ "net/http/pprof"
	"os"
	"runtime"
	"time"

	child_process_manager "github.com/AgustinSRG/go-child-process-manager"
	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/cmd/streampanel/autoupdater"
	"github.com/xaionaro-go/streamctl/pkg/buildvars"
	"github.com/xaionaro-go/streamctl/pkg/mainprocess"
	"github.com/xaionaro-go/streamctl/pkg/observability"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamd/grpc/go/streamd_grpc"
	"github.com/xaionaro-go/streamctl/pkg/streampanel"
	_ "github.com/xaionaro-go/streamctl/pkg/streamserver"
)

const forceNetPProfOnAndroid = true

func main() {
	err := child_process_manager.InitializeChildProcessManager()
	if err != nil {
		panic(err)
	}
	defer child_process_manager.DisposeChildProcessManager()

	flags := parseFlags()
	ctx := getContext(flags)
	{
		// rerunning flag parsing just for logs of parsing the flags (after initializing the logger in `getContext` above)
		for _, platformGetFlagsFunc := range platformGetFlagsFuncs {
			platformGetFlagsFunc(&flags)
		}
		logger.Debugf(ctx, "flags == %#+v", flags)
	}
	ctx, cancelFunc := initRuntime(ctx, flags, ProcessNameMain)
	defer cancelFunc()

	if flags.Subprocess != "" {
		runSubprocess(ctx, flags.Subprocess)
		return
	}
	ctx = belt.WithField(ctx, "process", ProcessNameMain)
	defer func() { observability.PanicIfNotNil(ctx, recover()) }()
	observability.Go(ctx, func() {
		<-ctx.Done()
		logger.Debugf(ctx, "context is cancelled")
	})

	if flags.SplitProcess && flags.RemoteAddr == "" {
		runSplitProcesses(ctx, cancelFunc, flags)
		return
	}

	runPanel(ctx, cancelFunc, flags, nil)
}

func runPanel(
	ctx context.Context,
	cancelFunc context.CancelFunc,
	flags Flags,
	mainProcess *mainprocess.Client,
) {
	logger.Debugf(ctx, "runPanel: %#+v", flags)
	defer logger.Debugf(ctx, "/runPanel")

	opts := []streampanel.Option{
		streampanel.OptionOAuthListenPortTwitch(flags.OAuthListenPortTwitch),
		streampanel.OptionOAuthListenPortYouTube(flags.OAuthListenPortYouTube),
	}
	if flags.RemoteAddr != "" {
		opts = append(opts, streampanel.OptionRemoteStreamDAddr(flags.RemoteAddr))
	}

	panel, panelErr := streampanel.New(flags.ConfigPath, opts...)
	if panelErr != nil {
		logger.Panic(ctx, panelErr)
	}

	switch runtime.GOOS {
	case "android":
	default:
		// TODO: remove this ugly hack:
		if panel.Config.RemoteStreamDAddr != flags.RemoteAddr {
			panel.Config.RemoteStreamDAddr = flags.RemoteAddr
		}
	}

	if panel.Config.RemoteStreamDAddr != "" {
		ctx = belt.WithField(ctx, "streamd_addr", panel.Config.RemoteStreamDAddr)
	}

	if !flags.SplitProcess && flags.ListenAddr != "" {
		logger.Debugf(ctx, `!flags.SplitProcess && flags.ListenAddr != ""`)

		err := panel.LazyInitStreamD(ctx)
		if err != nil {
			logger.Panic(ctx, err)
		}

		assert(ctx, panel.StreamD != nil)
		listener, grpcServer, streamdGRPC, _, _ := initGRPCServers(
			ctx,
			panel.StreamD,
			flags.ListenAddr,
		)

		// to erase an oauth request answered locally from "UnansweredOAuthRequests" in the GRPC server:
		panel.OnInternallySubmittedOAuthCode = func(
			ctx context.Context,
			platID streamcontrol.PlatformName,
			code string,
		) error {
			_, err := streamdGRPC.SubmitOAuthCode(ctx, &streamd_grpc.SubmitOAuthCodeRequest{
				PlatID: string(platID),
				Code:   code,
			})
			return err
		}

		observability.Go(ctx, func() {
			err = grpcServer.Serve(listener)
			if err != nil {
				logger.Panicf(ctx, "unable to server the gRPC server: %v", err)
			}
		})
	}

	if mainProcess != nil {
		setReadyFor(ctx, mainProcess, StreamDDied{}, UpdateStreamDConfig{})
		observability.Go(ctx, func() {
			err := mainProcess.Serve(
				ctx,
				func(ctx context.Context, source mainprocess.ProcessName, content any) error {
					switch msg := content.(type) {
					case MessageProcessDie:
						logger.Debugf(ctx, "received a request to kill myself")
						cancelFunc()
						os.Exit(0)
						return fmt.Errorf("this line is supposed to be unreachable (case #0)")
					case StreamDDied:
						logger.Errorf(ctx, "streamd died, killing myself as well (to get reborn)")
						cancelFunc()
						os.Exit(0)
						return fmt.Errorf("this line is supposed to be unreachable (case #1)")
					case UpdateStreamDConfig:
						logger.Debugf(ctx, "UpdateStreamDConfig: parsing the config")
						_, err := panel.Config.BuiltinStreamD.ReadFrom(bytes.NewReader([]byte(msg.Config)))
						if err != nil {
							err := fmt.Errorf("unable to deserialize the updated streamd config: %w", err)
							logger.Errorf(ctx, "%s", err)
							return err
						}
						logger.Debugf(ctx, "UpdateStreamDConfig: saving the config")
						err = panel.SaveConfig(ctx)
						if err != nil {
							err := fmt.Errorf("unable to save the updated streamd config: %w", err)
							logger.Errorf(ctx, "%s", err)
							return err
						}
						logger.Debugf(ctx, "UpdateStreamDConfig: saved the config")
					}
					return nil
				},
			)
			select {
			case <-ctx.Done():
				return
			case <-time.After(10 * time.Millisecond): // TODO: remove this hack
			}
			logger.Panicf(ctx, "communication (with the main process) error: %v", err)
		})
	}

	var loopOpts []streampanel.LoopOption
	if flags.Page != "" {
		loopOpts = append(loopOpts, streampanel.LoopOptionStartingPage(flags.Page))
	}
	if buildvars.GitCommit != "" && buildvars.BuildDate != nil && autoupdater.Available {
		logger.Debugf(ctx, "enabling an auto-updater")
		loopOpts = append(loopOpts, streampanel.LoopOptionAutoUpdater{
			AutoUpdater: autoupdater.New(
				buildvars.GitCommit,
				*buildvars.BuildDate,
				func() {
					err := mainProcess.SendMessage(ctx, ProcessNameMain, MessageApplicationPrepareForUpgrade{})
					if err != nil {
						logger.Errorf(ctx, "unable to ask 'main' to prepare for upgrade, might fail to restart after the upgrade: %v", err)
					}
				},
				func() {
					err := mainProcess.SendMessage(ctx, ProcessNameMain, MessageApplicationRestart{})
					if err != nil {
						logger.Errorf(ctx, "unable to send a request to restart the application: %v; closing the panel instead (hoping it will trigger a restart)", err)
						panel.Close()
					}
				},
			),
		})
	} else {
		logger.Debugf(ctx, "not enabling an auto-updater: %v %v %v", buildvars.GitCommit, buildvars.BuildDate, autoupdater.Available)
	}
	err := panel.Loop(ctx, loopOpts...)
	if err != nil {
		logger.Panic(ctx, err)
	}
	err = panel.Close()
	if err != nil {
		logger.Error(ctx, err)
	}
}
