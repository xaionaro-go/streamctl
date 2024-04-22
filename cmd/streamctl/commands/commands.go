package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"strings"
	"sync"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/goccy/go-yaml"
	"github.com/spf13/cobra"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/twitch"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/youtube"
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

	GenerateConfig = &cobra.Command{
		Use:  "generate-config",
		Args: cobra.ExactArgs(0),
		Run:  generateConfig,
	}

	SetTitle = &cobra.Command{
		Use:  "set-title",
		Args: cobra.ExactArgs(1),
		Run:  setTitle,
	}

	SetDescription = &cobra.Command{
		Use:  "set-description",
		Args: cobra.ExactArgs(1),
		Run:  setDescription,
	}

	StreamStart = &cobra.Command{
		Use:  "stream-start",
		Args: cobra.ExactArgs(0),
		Run:  streamStart,
	}

	StreamEnd = &cobra.Command{
		Use:  "stream-end",
		Args: cobra.ExactArgs(0),
		Run:  streamEnd,
	}

	YouTube = &cobra.Command{
		Use:  "youtube",
		Args: cobra.ExactArgs(0),
	}

	YouTubeListBroadcasts = &cobra.Command{
		Use:  "list-broadcasts",
		Args: cobra.ExactArgs(0),
		Run:  youTubeListBroadcasts,
	}

	YouTubeListStreams = &cobra.Command{
		Use:  "list-streams",
		Args: cobra.ExactArgs(0),
		Run:  youTubeListStreams,
	}

	LoggerLevel = logger.LevelWarning
)

func init() {
	Root.PersistentFlags().Var(&LoggerLevel, "log-level", "")
	Root.PersistentFlags().String("config-path", "~/.streamctl.yaml", "the path to the config file")
	StreamStart.PersistentFlags().String("title", "", "stream title")
	StreamStart.PersistentFlags().String("description", "", "stream description")
	StreamStart.PersistentFlags().String("profile", "", "profile")
	StreamStart.PersistentFlags().StringArray("youtube-templates", nil, "the list of templates used to create streams; if nothing is provided, then a stream won't be created")

	Root.AddCommand(GenerateConfig)
	Root.AddCommand(SetTitle)
	Root.AddCommand(SetDescription)
	Root.AddCommand(StreamStart)
	Root.AddCommand(StreamEnd)
	YouTube.AddCommand(YouTubeListBroadcasts)
	YouTube.AddCommand(YouTubeListStreams)
	Root.AddCommand(YouTube)
}

func getConfigPath() string {
	cfgPathRaw, err := Root.Flags().GetString("config-path")
	if err != nil {
		logger.Panic(Root.Context(), err)
	}

	return expandPath(cfgPathRaw)
}

func homeDir() string {
	dirname, err := os.UserHomeDir()
	if err != nil {
		logger.Panic(Root.Context(), err)
	}
	return dirname
}

func expandPath(rawPath string) string {
	switch {
	case strings.HasPrefix(rawPath, "~/"):
		return path.Join(homeDir(), rawPath[2:])
	}
	return rawPath
}

const (
	idTwitch  = twitch.ID
	idYoutube = youtube.ID
)

func newConfig() streamcontrol.Config {
	cfg := streamcontrol.Config{}
	twitch.InitConfig(cfg)
	youtube.InitConfig(cfg)
	return cfg
}

func generateConfig(cmd *cobra.Command, args []string) {
	cfgPath := getConfigPath()
	if _, err := os.Stat(cfgPath); err == nil {
		logger.Panicf(cmd.Context(), "file '%s' already exists", cfgPath)
	}
	cfg := newConfig()
	cfg[idTwitch].StreamProfiles = map[streamcontrol.ProfileName]streamcontrol.AbstractStreamProfile{"some_profile": twitch.StreamProfile{}}
	cfg[idYoutube].StreamProfiles = map[streamcontrol.ProfileName]streamcontrol.AbstractStreamProfile{"some_profile": youtube.StreamProfile{}}
	err := writeConfigToPath(cmd.Context(), cfgPath, cfg)
	if err != nil {
		logger.Panic(cmd.Context(), err)
	}
}

func writeConfigToPath(
	ctx context.Context,
	cfgPath string,
	cfg streamcontrol.Config,
) error {
	b, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("unable to serialize config %#+v: %w", cfg, err)
	}
	pathNew := cfgPath + ".new"
	err = os.WriteFile(pathNew, b, 0750)
	if err != nil {
		return fmt.Errorf("unable to write config to file '%s': %w", pathNew, err)
	}
	err = os.Rename(pathNew, cfgPath)
	if err != nil {
		return fmt.Errorf("cannot move '%s' to '%s': %w", pathNew, cfgPath, err)
	}
	logger.Infof(ctx, "wrote to '%s' config <%s>", cfgPath, b)
	return nil
}

func saveConfig(ctx context.Context, cfg streamcontrol.Config) error {
	cfgPath := getConfigPath()
	return writeConfigToPath(ctx, cfgPath, cfg)
}

func readConfigFromPath(
	ctx context.Context,
	cfgPath string,
	cfg *streamcontrol.Config,
) error {
	b, err := os.ReadFile(cfgPath)
	if err != nil {
		return fmt.Errorf("unable to read file '%s': %w", cfgPath, err)
	}
	err = yaml.Unmarshal(b, cfg)
	if err != nil {
		return fmt.Errorf("unable to unserialize config: %w: <%s>", err, b)
	}

	err = streamcontrol.ConvertStreamProfiles[twitch.StreamProfile](ctx, (*cfg)[idTwitch].StreamProfiles)
	if err != nil {
		return fmt.Errorf("unable to convert stream profiles of twitch: %w: <%s>", err, b)
	}
	logger.Debugf(ctx, "final stream profiles of twitch: %#+v", (*cfg)[idTwitch].StreamProfiles)

	err = streamcontrol.ConvertStreamProfiles[youtube.StreamProfile](ctx, (*cfg)[idYoutube].StreamProfiles)
	if err != nil {
		return fmt.Errorf("unable to convert stream profiles of twitch: %w: <%s>", err, b)
	}
	logger.Debugf(ctx, "final stream profiles of youtube: %#+v", (*cfg)[idYoutube].StreamProfiles)

	return nil
}

func readConfig(ctx context.Context) streamcontrol.Config {
	cfgPath := getConfigPath()
	cfg := newConfig()
	err := readConfigFromPath(ctx, cfgPath, &cfg)
	if err != nil {
		logger.Panic(ctx, err)
	}
	if logger.FromCtx(ctx).Level() >= logger.LevelDebug {
		if b, err := json.Marshal(cfg); err == nil {
			logger.Debugf(ctx, "cfg == %s", b)
		} else {
			logger.Debugf(ctx, "cfg == %#+v", cfg)
		}
	}
	return cfg
}

var saveConfigLock sync.Mutex

func getTwitchStreamController(
	ctx context.Context,
	cfg streamcontrol.Config,
) (*twitch.Twitch, error) {
	platCfg := streamcontrol.GetPlatformConfig[twitch.PlatformSpecificConfig, twitch.StreamProfile](ctx, cfg, idTwitch)
	if platCfg == nil {
		logger.Infof(ctx, "twitch config was not found")
		return nil, nil
	}

	logger.Debugf(ctx, "twitch config: %#+v", platCfg)
	return twitch.New(ctx, *platCfg,
		func(c twitch.Config) error {
			saveConfigLock.Lock()
			defer saveConfigLock.Unlock()
			cfg[idTwitch] = &streamcontrol.AbstractPlatformConfig{
				Config:         c.Config,
				StreamProfiles: streamcontrol.ToAbstractStreamProfiles(c.StreamProfiles),
			}
			return saveConfig(ctx, cfg)
		},
	)
}

func getYouTubeStreamController(
	ctx context.Context,
	cfg streamcontrol.Config,
) (*youtube.YouTube, error) {
	platCfg := streamcontrol.GetPlatformConfig[youtube.PlatformSpecificConfig, youtube.StreamProfile](ctx, cfg, idYoutube)
	if platCfg == nil {
		logger.Infof(ctx, "youtube config was not found")
		return nil, nil
	}

	logger.Debugf(ctx, "youtube config: %#+v", platCfg)
	return youtube.New(ctx, *platCfg,
		func(c youtube.Config) error {
			saveConfigLock.Lock()
			defer saveConfigLock.Unlock()
			cfg[idYoutube] = &streamcontrol.AbstractPlatformConfig{
				Config:         c.Config,
				StreamProfiles: streamcontrol.ToAbstractStreamProfiles(c.StreamProfiles),
			}
			return saveConfig(ctx, cfg)
		},
	)
}

func getStreamControllers(ctx context.Context, cfg streamcontrol.Config) streamcontrol.StreamControllers {
	var result streamcontrol.StreamControllers

	twitch, err := getTwitchStreamController(ctx, cfg)
	if err != nil {
		logger.Panic(ctx, err)
	}
	if twitch != nil {
		result = append(result, streamcontrol.ToAbstract(twitch))
	}

	youtube, err := getYouTubeStreamController(ctx, cfg)
	if err != nil {
		logger.Panic(ctx, err)
	}
	if youtube != nil {
		result = append(result, streamcontrol.ToAbstract(youtube))
	}

	return result
}

func ctxAndCfg(ctx context.Context) (context.Context, streamcontrol.Config) {
	return ctx, readConfig(ctx)
}

func assertNoError(ctx context.Context, err error) {
	if err != nil {
		logger.Panic(ctx, err)
	}
}

func setTitle(cmd *cobra.Command, args []string) {
	ctx, cfg := ctxAndCfg(cmd.Context())
	streamControllers := getStreamControllers(ctx, cfg)
	assertNoError(ctx, streamControllers.SetTitle(ctx, args[0]))
	assertNoError(ctx, streamControllers.Flush(ctx))
}

func setDescription(cmd *cobra.Command, args []string) {
	ctx, cfg := ctxAndCfg(cmd.Context())
	streamControllers := getStreamControllers(ctx, cfg)
	assertNoError(ctx, streamControllers.SetDescription(ctx, args[0]))
	assertNoError(ctx, streamControllers.Flush(ctx))
}

func streamStart(cmd *cobra.Command, args []string) {
	ctx, cfg := ctxAndCfg(cmd.Context())
	streamControllers := getStreamControllers(ctx, cfg)
	title, err := cmd.Flags().GetString("title")
	assertNoError(ctx, err)
	description, err := cmd.Flags().GetString("description")
	assertNoError(ctx, err)
	profileName, err := cmd.Flags().GetString("profile")
	assertNoError(ctx, err)
	youtubeTemplateBroadcastIDs, err := cmd.Flags().GetStringArray("youtube-templates")
	assertNoError(ctx, err)
	logger.Debugf(
		ctx,
		"title == '%s'; description == '%s'; profile == '%s', youtube-templates == %s",
		title, description, profileName, youtubeTemplateBroadcastIDs,
	)

	var profiles []streamcontrol.AbstractStreamProfile
	for _, platCfg := range cfg {
		p := platCfg.StreamProfiles[streamcontrol.ProfileName(profileName)]
		if p == nil {
			continue
		}

		profiles = append(profiles, p)
	}

	logger.Debugf(ctx, "profiles == %#+v", profiles)
	assertNoError(ctx, streamControllers.StartStream(
		ctx,
		title, description,
		profiles,
		youtube.FlagBroadcastTemplateIDs(youtubeTemplateBroadcastIDs),
		twitchStreamProfileSaver{cfg: cfg, profileName: profileName},
	))
	assertNoError(ctx, streamControllers.Flush(ctx))
}

func streamEnd(cmd *cobra.Command, args []string) {
	ctx, cfg := ctxAndCfg(cmd.Context())
	streamControllers := getStreamControllers(ctx, cfg)
	assertNoError(ctx, streamControllers.EndStream(ctx))
	assertNoError(ctx, streamControllers.Flush(ctx))
}

type twitchStreamProfileSaver struct {
	cfg         streamcontrol.Config
	profileName string
}

var _ twitch.SaveProfileHandler = (*twitchStreamProfileSaver)(nil)

func (h *twitchStreamProfileSaver) SaveProfile(
	ctx context.Context,
	streamProfile twitch.StreamProfile,
) error {
	h.cfg[idTwitch].StreamProfiles[streamcontrol.ProfileName(h.profileName)] = streamProfile
	return saveConfig(ctx, h.cfg)
}

func youTubeListStreams(cmd *cobra.Command, args []string) {
	ctx, cfg := ctxAndCfg(cmd.Context())
	youTube, err := getYouTubeStreamController(ctx, cfg)
	if err != nil {
		logger.Panic(ctx, err)
	}

	if youTube == nil {
		logger.Panic(ctx, "no youtube configuration provided")
	}

	streams, err := youTube.ListStreams(ctx)
	if err != nil {
		logger.Panic(ctx, err)
	}
	for _, stream := range streams {
		b, err := json.Marshal(stream)
		if err != nil {
			logger.Panicf(ctx, "unable to serialize stream info: %v: %#+v", err, *stream)
		}
		fmt.Printf("%s\n", b)
	}
}

func youTubeListBroadcasts(cmd *cobra.Command, args []string) {
	ctx, cfg := ctxAndCfg(cmd.Context())
	youTube, err := getYouTubeStreamController(ctx, cfg)
	if err != nil {
		logger.Panic(ctx, err)
	}

	if youTube == nil {
		logger.Panic(ctx, "no youtube configuration provided")
	}

	streams, err := youTube.ListBroadcasts(ctx)
	if err != nil {
		logger.Panic(ctx, err)
	}
	for _, stream := range streams {
		b, err := json.Marshal(stream)
		if err != nil {
			logger.Panicf(ctx, "unable to serialize stream info: %v: %#+v", err, *stream)
		}
		fmt.Printf("%s\n", b)
	}
}
