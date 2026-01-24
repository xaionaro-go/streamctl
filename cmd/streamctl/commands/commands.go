package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/goccy/go-yaml"
	"github.com/hashicorp/go-multierror"
	"github.com/spf13/cobra"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/twitch"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/youtube"
	"github.com/xaionaro-go/xsync"
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

	YouTubeListActiveBroadcasts = &cobra.Command{
		Use:  "list-active-broadcasts",
		Args: cobra.ExactArgs(0),
		Run:  youTubeListActiveBroadcasts,
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
	StreamStart.PersistentFlags().String("stream-id", "", "stream ID")
	StreamStart.PersistentFlags().
		StringArray("youtube-templates", nil, "the list of templates used to create streams; if nothing is provided, then a stream won't be created")
	StreamEnd.PersistentFlags().String("stream-id", "", "stream ID")

	Root.AddCommand(GenerateConfig)
	Root.AddCommand(SetTitle)
	Root.AddCommand(SetDescription)
	Root.AddCommand(StreamStart)
	Root.AddCommand(StreamEnd)
	YouTube.AddCommand(YouTubeListBroadcasts)
	YouTube.AddCommand(YouTubeListActiveBroadcasts)
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

const ()

func newConfig() streamcontrol.Config {
	cfg := streamcontrol.Config{}
	for _, platID := range streamcontrol.GetPlatformIDs() {
		streamcontrol.InitializeConfig(cfg, platID)
	}
	return cfg
}

func generateConfig(cmd *cobra.Command, args []string) {
	cfgPath := getConfigPath()
	if _, err := os.Stat(cfgPath); err == nil {
		logger.Panicf(cmd.Context(), "file '%s' already exists", cfgPath)
	}
	cfg := newConfig()
	for _, platID := range streamcontrol.GetPlatformIDs() {
		cfg[platID].Accounts = map[streamcontrol.AccountID]streamcontrol.RawMessage{
			streamcontrol.DefaultAccountID: streamcontrol.ToRawMessage(streamcontrol.AccountConfigBase[streamcontrol.RawMessage]{
				StreamProfiles: map[streamcontrol.StreamID]streamcontrol.StreamProfiles[streamcontrol.RawMessage]{
					streamcontrol.DefaultStreamID: {
						"some_profile": streamcontrol.ToRawMessage(streamcontrol.GetEmptyStreamProfile(platID)),
					},
				},
			}),
		}
	}
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
	logger.Infof(ctx, "wrote to '%s' the streamctl config <%s>", cfgPath, b)
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

	for _, platID := range streamcontrol.GetPlatformIDs() {
		if (*cfg)[platID] != nil {
			logger.Debugf(ctx, "loaded config for %s", platID)
		}
	}

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

var saveConfigLock xsync.Mutex

func getStreamControllers(
	ctx context.Context,
	cfg streamcontrol.Config,
) *PlatformController {
	result := &PlatformController{
		Accounts: make(map[streamcontrol.AccountID]streamcontrol.AbstractAccount),
	}

	for platID, platCfg := range cfg {
		for accountID := range platCfg.Accounts {
			ctrl, err := streamcontrol.NewAccount(ctx, platID, accountID, cfg, func(updatedCfg streamcontrol.Config) error {
				return xsync.DoR1(ctx, &saveConfigLock, func() error {
					return saveConfig(ctx, updatedCfg)
				})
			})
			if err != nil {
				logger.Panic(ctx, err)
			}
			if ctrl != nil {
				result.Accounts[accountID] = ctrl
			}
		}
	}

	return result
}

func getYouTubeStreamController(
	ctx context.Context,
	cfg streamcontrol.Config,
) (*youtube.YouTube, error) {
	sc := getStreamControllers(ctx, cfg)
	var ctrl streamcontrol.AbstractAccount
	for _, acc := range sc.Accounts {
		if acc.GetPlatformID() == youtube.ID {
			ctrl = acc
			break
		}
	}
	if ctrl == nil {
		return nil, nil
	}
	impl := ctrl.GetImplementation()
	yt, ok := impl.(*youtube.YouTube)
	if !ok {
		return nil, fmt.Errorf("expected *youtube.YouTube, but got %T", impl)
	}
	return yt, nil
}

func ctxAndCfg(ctx context.Context) (context.Context, streamcontrol.Config) {
	return ctx, readConfig(ctx)
}

func assertNoError(ctx context.Context, err error) {
	if err != nil {
		logger.Panic(ctx, err)
	}
}

func parseStreamID(s string) streamcontrol.StreamIDFullyQualified {
	var id streamcontrol.StreamIDFullyQualified
	if err := id.UnmarshalText([]byte(s)); err != nil {
		id.StreamID = streamcontrol.StreamID(s)
	}
	return id
}

func setTitle(cmd *cobra.Command, args []string) {
	ctx, cfg := ctxAndCfg(cmd.Context())
	streamControllers := getStreamControllers(ctx, cfg)
	streamSourceID, err := cmd.Flags().GetString("stream-id")
	assertNoError(ctx, err)
	parsedStreamID := parseStreamID(streamSourceID)
	assertNoError(ctx, streamControllers.SetTitle(ctx, parsedStreamID, args[0]))
	assertNoError(ctx, streamControllers.Flush(ctx, parsedStreamID))
}

func setDescription(cmd *cobra.Command, args []string) {
	ctx, cfg := ctxAndCfg(cmd.Context())
	streamControllers := getStreamControllers(ctx, cfg)
	streamSourceID, err := cmd.Flags().GetString("stream-id")
	assertNoError(ctx, err)
	parsedStreamID := parseStreamID(streamSourceID)
	assertNoError(ctx, streamControllers.SetDescription(ctx, parsedStreamID, args[0]))
	assertNoError(ctx, streamControllers.Flush(ctx, parsedStreamID))
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
	streamSourceID, err := cmd.Flags().GetString("stream-id")
	assertNoError(ctx, err)
	youtubeTemplateBroadcastIDs, err := cmd.Flags().GetStringArray("youtube-templates")
	assertNoError(ctx, err)
	logger.Debugf(
		ctx,
		"title == '%s'; description == '%s'; profile == '%s', stream-id == '%s', youtube-templates == %s",
		title, description, profileName, streamSourceID, youtubeTemplateBroadcastIDs,
	)

	overrides := &streamcontrol.StreamProfileBase{
		Title:       title,
		Description: description,
	}
	parsedStreamID := parseStreamID(streamSourceID)
	var errs *multierror.Error
	for platID, platCfg := range cfg {
		if parsedStreamID.PlatformID != "" && parsedStreamID.PlatformID != platID {
			continue
		}
		for accountID, accountRaw := range platCfg.Accounts {
			if parsedStreamID.AccountID != "" && parsedStreamID.AccountID != accountID {
				continue
			}

			id := parsedStreamID
			id.PlatformID = platID
			id.AccountID = accountID
			if id.StreamID == "" {
				id.StreamID = streamcontrol.DefaultStreamID
			}

			profiles := accountRaw.GetStreamProfiles()
			sProfs, ok := profiles[id.StreamID]
			var profile streamcontrol.StreamProfile
			if ok {
				p, ok := sProfs[streamcontrol.ProfileName(profileName)]
				if ok {
					profile = p
				} else {
					profile = overrides
				}
			} else {
				profile = overrides
			}

			err = streamControllers.StartStream(
				ctx,
				id,
				profile,
				youtube.FlagBroadcastTemplateIDs(youtubeTemplateBroadcastIDs),
				&twitchStreamProfileSaver{
					cfg:         cfg,
					profileName: profileName,
					accountID:   accountID,
					streamID:    id.StreamID,
				},
			)
			if err != nil {
				errs = multierror.Append(errs, err)
			}
		}
	}

	assertNoError(ctx, errs.ErrorOrNil())
	assertNoError(ctx, streamControllers.Flush(ctx, parsedStreamID))
}

func streamEnd(cmd *cobra.Command, args []string) {
	ctx, cfg := ctxAndCfg(cmd.Context())
	streamControllers := getStreamControllers(ctx, cfg)
	streamSourceID, err := cmd.Flags().GetString("stream-id")
	assertNoError(ctx, err)
	parsedStreamID := parseStreamID(streamSourceID)
	assertNoError(ctx, streamControllers.EndStream(ctx, parsedStreamID))
	assertNoError(ctx, streamControllers.Flush(ctx, parsedStreamID))
}

type twitchStreamProfileSaver struct {
	cfg         streamcontrol.Config
	profileName string
	accountID   streamcontrol.AccountID
	streamID    streamcontrol.StreamID
}

var _ twitch.SaveProfileHandler = (*twitchStreamProfileSaver)(nil)

func (h *twitchStreamProfileSaver) SaveProfile(
	ctx context.Context,
	streamProfile twitch.StreamProfile,
) error {
	platCfg := h.cfg[twitch.ID]
	if platCfg == nil {
		return fmt.Errorf("no twitch config")
	}
	accountRaw, ok := platCfg.Accounts[h.accountID]
	if !ok {
		return fmt.Errorf("account %s not found on twitch", h.accountID)
	}

	var accMap map[string]any
	err := yaml.Unmarshal(accountRaw, &accMap)
	if err != nil {
		return err
	}
	if accMap == nil {
		accMap = make(map[string]any)
	}

	profilesRaw, ok := accMap["stream_profiles"]
	var profiles map[streamcontrol.StreamID]map[streamcontrol.ProfileName]any
	if ok {
		_ = yaml.Unmarshal(streamcontrol.ToRawMessage(profilesRaw), &profiles)
	}
	if profiles == nil {
		profiles = make(map[streamcontrol.StreamID]map[streamcontrol.ProfileName]any)
	}
	sProfs := profiles[h.streamID]
	if sProfs == nil {
		sProfs = make(map[streamcontrol.ProfileName]any)
	}
	sProfs[streamcontrol.ProfileName(h.profileName)] = streamProfile
	profiles[h.streamID] = sProfs
	accMap["stream_profiles"] = profiles

	newRaw, err := yaml.Marshal(accMap)
	if err != nil {
		return err
	}
	platCfg.Accounts[h.accountID] = newRaw

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

	broadcasts, err := youTube.ListBroadcasts(ctx, 100, nil)
	if err != nil {
		logger.Panic(ctx, err)
	}
	for _, broadcast := range broadcasts {
		b, err := json.Marshal(broadcast)
		if err != nil {
			logger.Panicf(ctx, "unable to serialize stream info: %v: %#+v", err, *broadcast)
		}
		fmt.Printf("%s\n", b)
	}
}

func youTubeListActiveBroadcasts(cmd *cobra.Command, args []string) {
	ctx, cfg := ctxAndCfg(cmd.Context())
	youTube, err := getYouTubeStreamController(ctx, cfg)
	if err != nil {
		logger.Panic(ctx, err)
	}

	if youTube == nil {
		logger.Panic(ctx, "no youtube configuration provided")
	}

	err = youTube.IterateActiveBroadcasts(
		ctx,
		func(broadcast *youtube.LiveBroadcast) error {
			b, err := json.Marshal(broadcast)
			if err != nil {
				logger.Panic(ctx, err.Error())
			}
			fmt.Printf("%s\n", b)
			return nil
		},
		"id",
		"snippet",
		"contentDetails",
		"monetizationDetails",
		"status",
	)
	if err != nil {
		logger.Panic(ctx, err.Error())
	}
}
