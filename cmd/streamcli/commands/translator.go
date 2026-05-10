package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/goccy/go-yaml"
	"github.com/spf13/cobra"
	"github.com/xaionaro-go/streamctl/pkg/llm/translatorbuild"
	"github.com/xaionaro-go/streamctl/pkg/streamd/client"
	streamd_grpc "github.com/xaionaro-go/streamctl/pkg/streamd/grpc/go/streamd_grpc"
	translator_grpc "github.com/xaionaro-go/streamctl/pkg/translator/grpc/go/translator_grpc"
)

// minTranslatorWatchInterval bounds the --watch refresh rate. Going below
// 100ms turns the CLI into a busy loop hammering streamd's gRPC handler with
// no perceptible benefit, and a typo like `--watch 1` (parsed as 1ns by
// time.ParseDuration on integers... actually 1s — but very small intervals
// still flood the daemon) is more likely than a real need. We keep the
// rejection explicit so an operator who genuinely wants a faster refresh
// learns that the floor exists rather than wondering why the screen flickers.
const minTranslatorWatchInterval = 100 * time.Millisecond

var (
	Translator = &cobra.Command{
		Use:   "translator",
		Short: "Inspect and control the chat-message translator subprocess",
	}

	TranslatorStats = &cobra.Command{
		Use:   "stats",
		Short: "Print translator subprocess stats (queue, providers, counters)",
		Args:  cobra.ExactArgs(0),
		Run:   translatorStats,
	}

	TranslatorReload = &cobra.Command{
		Use:   "reload",
		Short: "Re-send the current translation config to the subprocess",
		Args:  cobra.ExactArgs(0),
		Run:   translatorReload,
	}

	TranslatorEnable = &cobra.Command{
		Use:   "enable <target-language>",
		Short: "Enable translation, persisting target_language and spawning the subprocess",
		Args:  cobra.ExactArgs(1),
		Run:   translatorEnable,
	}

	TranslatorDisable = &cobra.Command{
		Use:   "disable",
		Short: "Disable translation: kill the subprocess and clear target_language",
		Args:  cobra.ExactArgs(0),
		Run:   translatorDisable,
	}

	TranslatorRestart = &cobra.Command{
		Use:   "restart",
		Short: "Kill the translator subprocess and spawn a fresh one",
		Args:  cobra.ExactArgs(0),
		Run:   translatorRestart,
	}

	TranslatorHistory = &cobra.Command{
		Use:   "history",
		Short: "Manage the translator's chat history buffer",
	}

	TranslatorHistoryClear = &cobra.Command{
		Use:   "clear",
		Short: "Drop every recorded ChatHistoryEntry on the active chain",
		Args:  cobra.ExactArgs(0),
		Run:   translatorHistoryClear,
	}

	TranslatorTranslateCmd = &cobra.Command{
		Use:   "translate <user> <message>",
		Short: "Forward a single ad-hoc chat message through the translator subprocess",
		Args:  cobra.ExactArgs(2),
		Run:   translatorTranslate,
	}

	TranslatorQueue = &cobra.Command{
		Use:   "queue",
		Short: "Inspect or flush streamd's pending translation queue",
		Args:  cobra.ExactArgs(0),
		Run:   translatorQueueList,
	}

	TranslatorQueueListCmd = &cobra.Command{
		Use:   "list",
		Short: "List jobs currently sitting in the translation queue",
		Args:  cobra.ExactArgs(0),
		Run:   translatorQueueList,
	}

	TranslatorQueueFlushCmd = &cobra.Command{
		Use:   "flush",
		Short: "Drop every job currently sitting in the translation queue",
		Args:  cobra.ExactArgs(0),
		Run:   translatorQueueFlush,
	}

	TranslatorDump = &cobra.Command{
		Use:   "dump [file]",
		Short: "Dump the translator queue (or a selected subset via --ids) as a JSON array to file or stdout",
		Args:  cobra.MaximumNArgs(1),
		Run:   translatorDump,
	}

	TranslatorReplay = &cobra.Command{
		Use:   "replay <file>",
		Short: "Replay every entry in a dump file in order through the translator",
		Args:  cobra.ExactArgs(1),
		Run:   translatorReplay,
	}

	TranslatorConfig = &cobra.Command{
		Use:   "config",
		Short: "Inspect and edit d.Config.Translation",
	}

	TranslatorConfigGet = &cobra.Command{
		Use:   "get",
		Short: "Print the current translation config (YAML by default)",
		Args:  cobra.ExactArgs(0),
		Run:   translatorConfigGet,
	}

	TranslatorConfigSet = &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a top-level translation config key (target_language, chat_history_size, queue_size)",
		Args:  cobra.ExactArgs(2),
		Run:   translatorConfigSet,
	}

	TranslatorProviders = &cobra.Command{
		Use:   "providers",
		Short: "Inspect and edit the translation provider chain",
	}

	TranslatorProvidersList = &cobra.Command{
		Use:   "list",
		Short: "List configured translation providers",
		Args:  cobra.ExactArgs(0),
		Run:   translatorProvidersList,
	}

	TranslatorProvidersAdd = &cobra.Command{
		Use:   "add <name> <type>",
		Short: "Append a provider to the chain",
		Args:  cobra.ExactArgs(2),
		Run:   translatorProvidersAdd,
	}

	TranslatorProvidersRemove = &cobra.Command{
		Use:   "remove <name|#index>",
		Short: "Remove a provider from the chain by name or '#'-prefixed index",
		Args:  cobra.ExactArgs(1),
		Run:   translatorProvidersRemove,
	}

	TranslatorProvidersSet = &cobra.Command{
		Use:   "set <name> <key> <value>",
		Short: "Set one field on a provider (type, api_url, api_key, model, parallelism, timeout)",
		Args:  cobra.ExactArgs(3),
		Run:   translatorProvidersSet,
	}

	TranslatorDebug = &cobra.Command{
		Use:   "debug",
		Short: "Render prompts the chain WOULD send to a provider, without calling one",
	}

	TranslatorDebugCompileTranslate = &cobra.Command{
		Use:   "compile_translate_request <file>",
		Short: "For each entry in a queue dump, render the Translate prompts the chain would emit",
		Args:  cobra.ExactArgs(1),
		Run:   translatorDebugCompileTranslate,
	}

	TranslatorDebugCompileLanguageDetect = &cobra.Command{
		Use:   "compile_language_detect_request <file>",
		Short: "For each entry in a queue dump, render the language-detect prompts the chain would emit",
		Args:  cobra.ExactArgs(1),
		Run:   translatorDebugCompileLanguageDetect,
	}
)

func init() {
	Translator.AddCommand(TranslatorStats)
	Translator.AddCommand(TranslatorReload)
	Translator.AddCommand(TranslatorEnable)
	Translator.AddCommand(TranslatorDisable)
	Translator.AddCommand(TranslatorRestart)

	Translator.AddCommand(TranslatorHistory)
	TranslatorHistory.AddCommand(TranslatorHistoryClear)

	Translator.AddCommand(TranslatorTranslateCmd)

	Translator.AddCommand(TranslatorQueue)
	TranslatorQueue.AddCommand(TranslatorQueueListCmd)
	TranslatorQueue.AddCommand(TranslatorQueueFlushCmd)

	TranslatorQueue.AddCommand(TranslatorDump)
	TranslatorQueue.AddCommand(TranslatorReplay)
	TranslatorDump.PersistentFlags().String("ids", "",
		"comma-separated subset of dedup-key ids to dump; empty (default) dumps the entire queue")
	TranslatorReplay.PersistentFlags().String("mode", "chain",
		"replay path: 'chain' runs the full subprocess chain; 'provider' calls one named provider directly (requires --provider)")
	TranslatorReplay.PersistentFlags().String("provider", "",
		"with --mode=provider, the provider name to bypass-call (e.g. 'ollama(qwen3.5:9b-mxfp8)')")

	Translator.AddCommand(TranslatorConfig)
	TranslatorConfig.AddCommand(TranslatorConfigGet)
	TranslatorConfig.AddCommand(TranslatorConfigSet)
	TranslatorConfigGet.PersistentFlags().Bool("json", false, "use JSON output format (defaults to YAML)")

	Translator.AddCommand(TranslatorProviders)
	TranslatorProviders.AddCommand(TranslatorProvidersList)
	TranslatorProviders.AddCommand(TranslatorProvidersAdd)
	TranslatorProviders.AddCommand(TranslatorProvidersRemove)
	TranslatorProviders.AddCommand(TranslatorProvidersSet)
	TranslatorProvidersList.PersistentFlags().Bool("json", false, "use JSON output format")
	TranslatorProvidersAdd.PersistentFlags().String("api-url", "", "provider HTTP base URL")
	TranslatorProvidersAdd.PersistentFlags().String("api-key", "", "provider API key (cloud providers only)")
	TranslatorProvidersAdd.PersistentFlags().String("model", "", "model identifier")
	TranslatorProvidersAdd.PersistentFlags().Uint("parallelism", 0, "max concurrent in-flight Translate calls")
	TranslatorProvidersAdd.PersistentFlags().Duration("timeout", 0, "per-call timeout (e.g. 30s); 0 = package default")

	TranslatorStats.PersistentFlags().Bool("json", false, "use JSON output format")
	TranslatorStats.PersistentFlags().Duration("watch", 0,
		"refresh repeatedly at the given interval (e.g. 1s); 0 disables")

	Translator.AddCommand(TranslatorDebug)
	TranslatorDebug.AddCommand(TranslatorDebugCompileTranslate)
	TranslatorDebug.AddCommand(TranslatorDebugCompileLanguageDetect)
	TranslatorDebugCompileTranslate.PersistentFlags().Bool("text", false,
		"emit human-readable text form (default: JSON, suitable for piping to jq/curl)")
	TranslatorDebugCompileLanguageDetect.PersistentFlags().Bool("text", false,
		"emit human-readable text form (default: JSON, suitable for piping to jq/curl)")
}

func translatorStats(cmd *cobra.Command, args []string) {
	ctx := cmd.Context()

	remoteAddr, err := cmd.Flags().GetString("remote-addr")
	assertNoError(ctx, err)

	isJSON, err := cmd.Flags().GetBool("json")
	assertNoError(ctx, err)

	watch, err := cmd.Flags().GetDuration("watch")
	assertNoError(ctx, err)

	switch {
	case watch == 0:
		streamD, err := client.New(ctx, remoteAddr)
		assertNoError(ctx, err)
		reply, err := streamD.TranslatorStatsRaw(ctx)
		assertNoError(ctx, err)
		printTranslatorStats(ctx, reply, isJSON)
	case watch < minTranslatorWatchInterval:
		logger.Fatalf(ctx, "--watch interval %s is below the floor of %s",
			watch, minTranslatorWatchInterval)
	default:
		runTranslatorStatsWatch(ctx, remoteAddr, isJSON, watch)
	}
}

// runTranslatorStatsWatch repeatedly prints translator stats at the
// requested interval. We rebuild the client per-tick rather than reusing one
// so a streamd restart between iterations does not freeze the watch loop on
// a stale connection.
func runTranslatorStatsWatch(
	ctx context.Context,
	remoteAddr string,
	isJSON bool,
	watch time.Duration,
) {
	ticker := time.NewTicker(watch)
	defer ticker.Stop()

	tickOnce := func() {
		streamD, err := client.New(ctx, remoteAddr)
		if err != nil {
			logger.Errorf(ctx, "client.New: %v", err)
			return
		}
		reply, err := streamD.TranslatorStatsRaw(ctx)
		if err != nil {
			logger.Errorf(ctx, "TranslatorStats: %v", err)
			return
		}
		// Clear screen + cursor home: standard ANSI escapes, supported by
		// every terminal we care about. JSON output mode also clears so a
		// caller piping `jq` always sees a single object on each refresh.
		fmt.Print("\x1b[2J\x1b[H")
		printTranslatorStats(ctx, reply, isJSON)
	}
	tickOnce()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			tickOnce()
		}
	}
}

func printTranslatorStats(
	ctx context.Context,
	reply *streamd_grpc.TranslatorStatsReply,
	isJSON bool,
) {
	if isJSON {
		b, err := json.Marshal(reply)
		assertNoError(ctx, err)
		fmt.Printf("%s\n", b)
		return
	}

	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)

	if msg := reply.GetError(); msg != "" {
		// Per-RPC, in-band error per the Translator* service rule. The
		// streamd-side stats are still meaningful (queue counters etc.)
		// so we surface the error and keep printing the rest.
		fmt.Fprintf(tw, "error\t%s\n", msg)
	}
	fmt.Fprintf(tw, "running\t%v\n", reply.GetRunning())
	fmt.Fprintf(tw, "pid\t%d\n", reply.GetPid())
	fmt.Fprintf(tw, "socket\t%s\n", reply.GetSocketPath())
	last := reply.GetLastActivityUnixNano()
	if last == 0 {
		fmt.Fprintf(tw, "last-activity\t-\n")
	} else {
		fmt.Fprintf(tw, "last-activity\t%s\n", time.Unix(0, last).Format(time.RFC3339))
	}
	fmt.Fprintf(tw, "queue\t%d/%d (drops=%d)\n",
		reply.GetQueueLen(), reply.GetQueueCap(), reply.GetQueueDrops())
	fmt.Fprintf(tw, "translated\t%d\n", reply.GetTotalTranslated())
	fmt.Fprintf(tw, "already-target\t%d\n", reply.GetTotalAlreadyTarget())
	fmt.Fprintf(tw, "detect-failed\t%d\n", reply.GetTotalDetectFailed())
	fmt.Fprintf(tw, "spelling-only\t%d\n", reply.GetTotalSpellingOnly())
	fmt.Fprintf(tw, "all-providers-failed\t%d\n", reply.GetTotalAllProvidersFailed())
	fmt.Fprintf(tw, "skipped-queue-full\t%d\n", reply.GetTotalSkippedQueueFull())
	if cnt := reply.GetLatencyCount(); cnt > 0 {
		avg := time.Duration(reply.GetLatencySumNanos() / cnt)
		fmt.Fprintf(tw, "avg-latency\t%s (n=%d)\n", avg, cnt)
	} else {
		fmt.Fprintf(tw, "avg-latency\t- (n=0)\n")
	}
	_ = tw.Flush()

	providers := reply.GetProviders()
	if len(providers) == 0 {
		return
	}

	fmt.Println()
	pw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(pw, "PROVIDER\tCALLS\tOK\tERR\tTIMEOUT\tQ-REJ\tAVG-LATENCY")
	for _, p := range providers {
		avgStr := "-"
		if cnt := p.GetLatencyCount(); cnt > 0 {
			avgStr = (time.Duration(p.GetLatencySumNanos() / cnt)).String()
		}
		fmt.Fprintf(pw, "%s\t%d\t%d\t%d\t%d\t%d\t%s\n",
			p.GetName(),
			p.GetTotalCalls(),
			p.GetTotalSuccesses(),
			p.GetTotalErrors(),
			p.GetTotalTimeouts(),
			p.GetTotalQueueRejections(),
			avgStr,
		)
	}
	_ = pw.Flush()
}

func translatorReload(cmd *cobra.Command, args []string) {
	ctx := cmd.Context()

	remoteAddr, err := cmd.Flags().GetString("remote-addr")
	assertNoError(ctx, err)

	streamD, err := client.New(ctx, remoteAddr)
	assertNoError(ctx, err)

	applied, errMsg, err := streamD.TranslatorReloadDetail(ctx)
	if err != nil {
		logger.Fatalf(ctx, "TranslatorReload transport error: %v", err)
	}
	if errMsg != "" {
		logger.Fatalf(ctx, "translator reload failed: %s", errMsg)
	}
	if !applied {
		logger.Fatalf(ctx, "translator reload not applied (no error reported)")
	}

	fmt.Println("translator config reloaded")
}

func translatorEnable(cmd *cobra.Command, args []string) {
	ctx := cmd.Context()
	streamD := newStreamDClient(cmd)
	target := args[0]
	err := streamD.TranslatorEnable(ctx, target)
	assertNoError(ctx, err)
	fmt.Printf("translator enabled (target_language=%q)\n", target)
}

func translatorDisable(cmd *cobra.Command, args []string) {
	ctx := cmd.Context()
	streamD := newStreamDClient(cmd)
	err := streamD.TranslatorDisable(ctx)
	assertNoError(ctx, err)
	fmt.Println("translator disabled")
}

func translatorRestart(cmd *cobra.Command, args []string) {
	ctx := cmd.Context()
	streamD := newStreamDClient(cmd)
	err := streamD.TranslatorRestart(ctx)
	assertNoError(ctx, err)
	fmt.Println("translator subprocess restarted")
}

func translatorHistoryClear(cmd *cobra.Command, args []string) {
	ctx := cmd.Context()
	streamD := newStreamDClient(cmd)
	dropped, err := streamD.TranslatorClearHistory(ctx)
	assertNoError(ctx, err)
	fmt.Printf("dropped %d history entries\n", dropped)
}

func translatorTranslate(cmd *cobra.Command, args []string) {
	ctx := cmd.Context()
	streamD := newStreamDClient(cmd)
	user, message := args[0], args[1]
	result, outcome, latency, timings, err := streamD.TranslatorTranslateWithTimings(ctx, user, message)
	assertNoError(ctx, err)

	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "result\t%s\n", result)
	fmt.Fprintf(tw, "outcome\t%s\n", outcome)
	fmt.Fprintf(tw, "latency\t%s\n", latency)
	if line := formatTranslateTimings(timings); line != "" {
		fmt.Fprintf(tw, "timings\t%s\n", line)
	}
	_ = tw.Flush()
}

// formatTranslateTimings renders the optional per-call backend timing/token
// breakdown the subprocess attached to the reply. Returns an empty string
// when the reply carries no timings (provider does not populate them, or
// the chain skipped before reaching a real Translate) so callers can omit
// the row entirely. Format mirrors the Debug log emitted by
// pkg/llm/provider_ollama.go so operators see the same shape on both
// surfaces.
func formatTranslateTimings(t *translator_grpc.TranslateTimings) string {
	if t == nil {
		return ""
	}
	load := time.Duration(t.GetLoadDurationNanos())
	prompt := time.Duration(t.GetPromptEvalDurationNanos())
	eval := time.Duration(t.GetEvalDurationNanos())
	promptTok := t.GetPromptEvalTokens()
	evalTok := t.GetEvalTokens()
	tokPerSec := 0.0
	if eval > 0 {
		tokPerSec = float64(evalTok) * float64(time.Second) / float64(eval)
	}
	return fmt.Sprintf("load=%s prompt=%s(%d tok) eval=%s(%d tok, %.1f tok/s)",
		load, prompt, promptTok, eval, evalTok, tokPerSec)
}

func translatorQueueList(cmd *cobra.Command, args []string) {
	ctx := cmd.Context()
	streamD := newStreamDClient(cmd)
	entries, err := streamD.TranslatorQueueList(ctx)
	assertNoError(ctx, err)

	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "#\tID\tPLATFORM\tUSER\tENQUEUED\tMESSAGE")
	for i, e := range entries {
		enqueued := "-"
		if !e.EnqueuedAt.IsZero() {
			enqueued = e.EnqueuedAt.Format(time.RFC3339)
		}
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\t%s\n",
			i, e.ID, e.Platform, e.User, enqueued, e.Message)
	}
	_ = tw.Flush()
}

func translatorQueueFlush(cmd *cobra.Command, args []string) {
	ctx := cmd.Context()
	streamD := newStreamDClient(cmd)
	dropped, err := streamD.TranslatorQueueFlush(ctx)
	assertNoError(ctx, err)
	fmt.Printf("dropped %d queued jobs\n", dropped)
}

// translatorDumpEntry is the on-wire JSON shape used by `translator dump`
// and consumed by `translator replay`. Mirrors api.TranslationQueueEntry but
// with explicit JSON tags so the format is stable regardless of struct
// changes (and so EnqueuedAt round-trips as RFC3339, not the default
// time.Time encoding).
type translatorDumpEntry struct {
	ID         string    `json:"id"`
	Platform   string    `json:"platform"`
	User       string    `json:"user"`
	Message    string    `json:"message"`
	EnqueuedAt time.Time `json:"enqueuedAt"`
}

func translatorDump(cmd *cobra.Command, args []string) {
	ctx := cmd.Context()
	streamD := newStreamDClient(cmd)

	idsFlag, err := cmd.Flags().GetString("ids")
	assertNoError(ctx, err)
	wanted := parseDumpIDFilter(idsFlag)

	entries, err := streamD.TranslatorQueueList(ctx)
	assertNoError(ctx, err)

	dumps := make([]translatorDumpEntry, 0, len(entries))
	for _, e := range entries {
		if wanted != nil {
			if _, ok := wanted[e.ID]; !ok {
				continue
			}
			delete(wanted, e.ID)
		}
		dumps = append(dumps, translatorDumpEntry{
			ID:         e.ID,
			Platform:   e.Platform,
			User:       e.User,
			Message:    e.Message,
			EnqueuedAt: e.EnqueuedAt,
		})
	}
	// Surface ids the user asked for that the queue did not contain so a
	// typo or stale snapshot is not silently swallowed. Fatal so a
	// mis-spelled id never produces a partial-but-successful dump.
	if len(wanted) > 0 {
		missing := make([]string, 0, len(wanted))
		for id := range wanted {
			missing = append(missing, id)
		}
		logger.Fatalf(ctx, "ids not found in queue: %v (run `translator queue list` to see current ids)", missing)
	}

	b, err := json.MarshalIndent(dumps, "", "  ")
	assertNoError(ctx, err)

	out := writerForOptionalPath(ctx, args, 0)
	if closer, ok := out.(io.Closer); ok {
		defer func() { _ = closer.Close() }()
	}
	if _, err := out.Write(b); err != nil {
		logger.Fatalf(ctx, "write dump: %v", err)
	}
	if _, err := out.Write([]byte("\n")); err != nil {
		logger.Fatalf(ctx, "write dump: %v", err)
	}
}

// parseDumpIDFilter splits the --ids flag into a set. Empty input means
// "no filter" (return nil) so the caller can disambiguate "user passed an
// empty filter" from "user did not pass --ids at all" — both behave the
// same here and the nil-set check stays single-meaning.
func parseDumpIDFilter(raw string) map[string]struct{} {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	out := make(map[string]struct{})
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		out[part] = struct{}{}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// writerForOptionalPath returns the destination writer for `translator dump`:
// stdout when args has fewer than pathArgIdx+1 entries (or the path is "-"),
// otherwise a freshly created file at args[pathArgIdx]. Centralised so the
// dash-as-stdout convention stays consistent for future commands.
func writerForOptionalPath(
	ctx context.Context,
	args []string,
	pathArgIdx int,
) io.Writer {
	if len(args) <= pathArgIdx || args[pathArgIdx] == "-" {
		return os.Stdout
	}
	f, err := os.Create(args[pathArgIdx])
	if err != nil {
		logger.Fatalf(ctx, "create %q: %v", args[pathArgIdx], err)
	}
	return f
}

func translatorReplay(cmd *cobra.Command, args []string) {
	ctx := cmd.Context()
	streamD := newStreamDClient(cmd)
	mode, err := cmd.Flags().GetString("mode")
	assertNoError(ctx, err)
	provider, err := cmd.Flags().GetString("provider")
	assertNoError(ctx, err)

	switch mode {
	case "chain":
		if provider != "" {
			logger.Fatalf(ctx, "--provider is only valid with --mode=provider")
		}
	case "provider":
		if provider == "" {
			logger.Fatalf(ctx, "--mode=provider requires --provider <name> (run `translator providers list` to see names)")
		}
	default:
		logger.Fatalf(ctx, "unknown --mode %q (supported: chain, provider)", mode)
	}

	b, err := os.ReadFile(args[0])
	if err != nil {
		logger.Fatalf(ctx, "read %q: %v", args[0], err)
	}
	var dumps []translatorDumpEntry
	if err := json.Unmarshal(b, &dumps); err != nil {
		logger.Fatalf(ctx, "parse %q as JSON array of TranslationQueueEntry: %v", args[0], err)
	}
	if len(dumps) == 0 {
		fmt.Println("(empty dump file — nothing to replay)")
		return
	}

	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "#\tID\tUSER\tOUTCOME\tLATENCY\tRESULT")
	for i, dump := range dumps {
		var (
			result, outcome string
			latency         time.Duration
			timings         *translator_grpc.TranslateTimings
		)
		switch mode {
		case "chain":
			r, o, l, t, err := streamD.TranslatorTranslateWithTimings(ctx, dump.User, dump.Message)
			if err != nil {
				outcome = "(error: " + err.Error() + ")"
				result = ""
				latency = 0
				timings = t
			} else {
				result, outcome, latency, timings = r, o, l, t
			}
		case "provider":
			r, l, t, err := streamD.TranslatorTranslateViaProviderWithTimings(
				ctx, dump.User, dump.Message, provider)
			if err != nil {
				outcome = "(error: " + err.Error() + ")"
				result = ""
				latency = l
				timings = t
			} else {
				result = r
				outcome = "(provider-bypass: " + provider + ")"
				latency = l
				timings = t
			}
		}
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\t%s\n",
			i, dump.ID, dump.User, outcome, latency, result)
		// Secondary indented continuation row when the provider attached
		// timings. Six leading tabs keep the timing line aligned under the
		// RESULT column of the primary 6-column header so a wide tabwriter
		// re-render does not push it back to column 0.
		if line := formatTranslateTimings(timings); line != "" {
			fmt.Fprintf(tw, "\t\t\t\t\t   %s\n", line)
		}
	}
	_ = tw.Flush()
}

func translatorConfigGet(cmd *cobra.Command, args []string) {
	ctx := cmd.Context()
	streamD := newStreamDClient(cmd)
	cfg, err := streamD.GetConfig(ctx)
	assertNoError(ctx, err)
	tc := cfg.Translation

	isJSON, err := cmd.Flags().GetBool("json")
	assertNoError(ctx, err)
	if isJSON {
		b, err := json.MarshalIndent(tc, "", "  ")
		assertNoError(ctx, err)
		fmt.Printf("%s\n", b)
		return
	}
	b, err := yaml.Marshal(tc)
	assertNoError(ctx, err)
	fmt.Printf("%s", b)
}

// translatorConfigSet mutates one top-level field on
// d.Config.Translation. The set of accepted keys is intentionally narrow:
// each addition needs explicit type-conversion so we error on unknowns
// rather than silently dropping a typo.
func translatorConfigSet(cmd *cobra.Command, args []string) {
	ctx := cmd.Context()
	streamD := newStreamDClient(cmd)
	key, value := args[0], args[1]

	cfg, err := streamD.GetConfig(ctx)
	assertNoError(ctx, err)
	switch key {
	case "target_language":
		cfg.Translation.TargetLanguage = value
	case "chat_history_size":
		n, err := strconv.Atoi(value)
		assertNoError(ctx, err)
		cfg.Translation.ChatHistorySize = n
	case "queue_size":
		n, err := strconv.Atoi(value)
		assertNoError(ctx, err)
		cfg.Translation.QueueSize = n
	default:
		logger.Fatalf(ctx, "unknown config key %q (supported: target_language, chat_history_size, queue_size)", key)
	}
	err = streamD.SetConfig(ctx, cfg)
	assertNoError(ctx, err)
	err = streamD.SaveConfig(ctx)
	assertNoError(ctx, err)
	fmt.Printf("set %s=%s\n", key, value)
	if key == "queue_size" {
		fmt.Println("note: queue_size takes effect on streamd restart")
	}
}

func translatorProvidersList(cmd *cobra.Command, args []string) {
	ctx := cmd.Context()
	streamD := newStreamDClient(cmd)
	cfg, err := streamD.GetConfig(ctx)
	assertNoError(ctx, err)
	tc := cfg.Translation

	isJSON, err := cmd.Flags().GetBool("json")
	assertNoError(ctx, err)
	if isJSON {
		b, err := json.MarshalIndent(tc.Providers, "", "  ")
		assertNoError(ctx, err)
		fmt.Printf("%s\n", b)
		return
	}

	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "#\tNAME\tTYPE\tMODEL\tAPI_URL\tPARALLELISM\tTIMEOUT")
	for i, p := range tc.Providers {
		name := p.Name
		if name == "" {
			name = p.Type
		}
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\t%d\t%s\n",
			i, name, p.Type, p.Model, p.APIURL, p.Parallelism, p.Timeout)
	}
	_ = tw.Flush()
}

func translatorProvidersAdd(cmd *cobra.Command, args []string) {
	ctx := cmd.Context()
	streamD := newStreamDClient(cmd)
	name, ptype := args[0], args[1]

	apiURL, err := cmd.Flags().GetString("api-url")
	assertNoError(ctx, err)
	apiKey, err := cmd.Flags().GetString("api-key")
	assertNoError(ctx, err)
	model, err := cmd.Flags().GetString("model")
	assertNoError(ctx, err)
	parallelism, err := cmd.Flags().GetUint("parallelism")
	assertNoError(ctx, err)
	timeout, err := cmd.Flags().GetDuration("timeout")
	assertNoError(ctx, err)

	cfg, err := streamD.GetConfig(ctx)
	assertNoError(ctx, err)

	for _, p := range cfg.Translation.Providers {
		existing := p.Name
		if existing == "" {
			existing = p.Type
		}
		if existing == name {
			logger.Fatalf(ctx, "provider %q already exists in the chain (use `providers set` to edit)", name)
		}
	}

	cfg.Translation.Providers = append(cfg.Translation.Providers, translatorbuild.ProviderConfig{
		Name:        name,
		Type:        ptype,
		APIURL:      apiURL,
		APIKey:      apiKey,
		Model:       model,
		Parallelism: int(parallelism),
		Timeout:     timeout,
	})
	err = streamD.SetConfig(ctx, cfg)
	assertNoError(ctx, err)
	err = streamD.SaveConfig(ctx)
	assertNoError(ctx, err)
	fmt.Printf("added provider %q (type=%s)\n", name, ptype)
}

// findProviderIndex resolves either a name or a #N index reference into
// a position in the Providers slice. Returns -1 if not found so the
// caller can produce a precise diagnostic.
func findProviderIndex(
	providers []translatorbuild.ProviderConfig,
	ref string,
) int {
	if strings.HasPrefix(ref, "#") {
		n, err := strconv.Atoi(ref[1:])
		if err != nil {
			return -1
		}
		if n < 0 || n >= len(providers) {
			return -1
		}
		return n
	}
	for i, p := range providers {
		name := p.Name
		if name == "" {
			name = p.Type
		}
		if name == ref {
			return i
		}
	}
	return -1
}

func translatorProvidersRemove(cmd *cobra.Command, args []string) {
	ctx := cmd.Context()
	streamD := newStreamDClient(cmd)
	ref := args[0]

	cfg, err := streamD.GetConfig(ctx)
	assertNoError(ctx, err)

	idx := findProviderIndex(cfg.Translation.Providers, ref)
	if idx < 0 {
		logger.Fatalf(ctx, "provider %q not found in the chain", ref)
	}
	removed := cfg.Translation.Providers[idx]
	cfg.Translation.Providers = append(cfg.Translation.Providers[:idx], cfg.Translation.Providers[idx+1:]...)
	err = streamD.SetConfig(ctx, cfg)
	assertNoError(ctx, err)
	err = streamD.SaveConfig(ctx)
	assertNoError(ctx, err)

	displayName := removed.Name
	if displayName == "" {
		displayName = removed.Type
	}
	fmt.Printf("removed provider %q (type=%s)\n", displayName, removed.Type)
}

// translatorProvidersSet mutates one field on a single provider entry. The
// limited key set keeps the parsing path explicit; unknown keys error so a
// typo never silently no-ops.
func translatorProvidersSet(cmd *cobra.Command, args []string) {
	ctx := cmd.Context()
	streamD := newStreamDClient(cmd)
	ref, key, value := args[0], args[1], args[2]

	cfg, err := streamD.GetConfig(ctx)
	assertNoError(ctx, err)
	idx := findProviderIndex(cfg.Translation.Providers, ref)
	if idx < 0 {
		logger.Fatalf(ctx, "provider %q not found in the chain", ref)
	}
	p := &cfg.Translation.Providers[idx]
	switch key {
	case "type":
		p.Type = value
	case "api_url":
		p.APIURL = value
	case "api_key":
		p.APIKey = value
	case "model":
		p.Model = value
	case "parallelism":
		n, err := strconv.Atoi(value)
		assertNoError(ctx, err)
		if n < 0 {
			logger.Fatalf(ctx, "parallelism must be >= 0, got %d", n)
		}
		p.Parallelism = n
	case "timeout":
		d, err := time.ParseDuration(value)
		assertNoError(ctx, err)
		p.Timeout = d
	default:
		logger.Fatalf(ctx, "unknown provider key %q (supported: type, api_url, api_key, model, parallelism, timeout)", key)
	}
	err = streamD.SetConfig(ctx, cfg)
	assertNoError(ctx, err)
	err = streamD.SaveConfig(ctx)
	assertNoError(ctx, err)
	fmt.Printf("set provider %q %s=%s\n", ref, key, value)
}

// compileTranslateOutEntry is the JSON shape `translator debug
// compile_translate_request` emits, one per dump-file entry. It mirrors the
// streamd RPC reply plus the source-entry index/id/user so an operator can
// correlate output back to the dump without re-loading the source file.
type compileTranslateOutEntry struct {
	EntryIndex      int    `json:"entry_index"`
	ID              string `json:"id"`
	User            string `json:"user"`
	Message         string `json:"message"`
	SystemPrompt    string `json:"system_prompt"`
	UserPrompt      string `json:"user_prompt"`
	TargetLang      string `json:"target_lang"`
	HistorySnapshot string `json:"history_snapshot"`
	Error           string `json:"error,omitempty"`
}

// compileLanguageDetectOutEntry mirrors compileTranslateOutEntry but omits
// the `user` field — the language-detect step has no per-message user.
type compileLanguageDetectOutEntry struct {
	EntryIndex      int    `json:"entry_index"`
	ID              string `json:"id"`
	Message         string `json:"message"`
	SystemPrompt    string `json:"system_prompt"`
	UserPrompt      string `json:"user_prompt"`
	TargetLang      string `json:"target_lang"`
	HistorySnapshot string `json:"history_snapshot"`
	Error           string `json:"error,omitempty"`
}

// readQueueDump reads a queue-dump JSON file (the format `translator dump`
// emits) and returns the parsed entries. Surfaces a fatal diagnostic on
// read/parse failure so the caller does not have to special-case errors.
func readQueueDump(ctx context.Context, path string) []translatorDumpEntry {
	b, err := os.ReadFile(path)
	if err != nil {
		logger.Fatalf(ctx, "read %q: %v", path, err)
	}
	var dumps []translatorDumpEntry
	if err := json.Unmarshal(b, &dumps); err != nil {
		logger.Fatalf(ctx, "parse %q as JSON array of TranslationQueueEntry: %v", path, err)
	}
	return dumps
}

func translatorDebugCompileTranslate(cmd *cobra.Command, args []string) {
	ctx := cmd.Context()
	streamD := newStreamDClient(cmd)
	asText, err := cmd.Flags().GetBool("text")
	assertNoError(ctx, err)

	dumps := readQueueDump(ctx, args[0])

	out := make([]compileTranslateOutEntry, 0, len(dumps))
	for i, dump := range dumps {
		entry := compileTranslateOutEntry{
			EntryIndex: i,
			ID:         dump.ID,
			User:       dump.User,
			Message:    dump.Message,
		}
		sysP, userP, target, hist, err := streamD.TranslatorCompileTranslate(ctx, dump.User, dump.Message)
		if err != nil {
			entry.Error = err.Error()
		} else {
			entry.SystemPrompt = sysP
			entry.UserPrompt = userP
			entry.TargetLang = target
			entry.HistorySnapshot = hist
		}
		out = append(out, entry)
	}

	if asText {
		printCompileTranslateText(out)
		return
	}
	b, err := json.MarshalIndent(out, "", "  ")
	assertNoError(ctx, err)
	fmt.Printf("%s\n", b)
}

func translatorDebugCompileLanguageDetect(cmd *cobra.Command, args []string) {
	ctx := cmd.Context()
	streamD := newStreamDClient(cmd)
	asText, err := cmd.Flags().GetBool("text")
	assertNoError(ctx, err)

	dumps := readQueueDump(ctx, args[0])

	out := make([]compileLanguageDetectOutEntry, 0, len(dumps))
	for i, dump := range dumps {
		entry := compileLanguageDetectOutEntry{
			EntryIndex: i,
			ID:         dump.ID,
			Message:    dump.Message,
		}
		sysP, userP, target, hist, err := streamD.TranslatorCompileLanguageDetect(ctx, dump.Message)
		if err != nil {
			entry.Error = err.Error()
		} else {
			entry.SystemPrompt = sysP
			entry.UserPrompt = userP
			entry.TargetLang = target
			entry.HistorySnapshot = hist
		}
		out = append(out, entry)
	}

	if asText {
		printCompileLanguageDetectText(out)
		return
	}
	b, err := json.MarshalIndent(out, "", "  ")
	assertNoError(ctx, err)
	fmt.Printf("%s\n", b)
}

// printCompileTranslateText renders the human form of the compile-translate
// output: one block per entry with explicit prompt section dividers.
func printCompileTranslateText(out []compileTranslateOutEntry) {
	for _, e := range out {
		fmt.Printf("=== entry %d (id=%s user=%s) ===\n", e.EntryIndex, e.ID, e.User)
		if e.Error != "" {
			fmt.Printf("ERROR: %s\n\n", e.Error)
			continue
		}
		fmt.Printf("target_lang: %s\n", e.TargetLang)
		fmt.Printf("--- system prompt ---\n%s\n", e.SystemPrompt)
		fmt.Printf("--- user prompt ---\n%s\n", e.UserPrompt)
		fmt.Printf("--- history snapshot ---\n%s\n", e.HistorySnapshot)
		fmt.Println()
	}
}

// printCompileLanguageDetectText renders the human form of the
// compile-language-detect output: one block per entry with explicit prompt
// section dividers.
func printCompileLanguageDetectText(out []compileLanguageDetectOutEntry) {
	for _, e := range out {
		fmt.Printf("=== entry %d (id=%s) ===\n", e.EntryIndex, e.ID)
		if e.Error != "" {
			fmt.Printf("ERROR: %s\n\n", e.Error)
			continue
		}
		fmt.Printf("target_lang: %s\n", e.TargetLang)
		fmt.Printf("--- system prompt ---\n%s\n", e.SystemPrompt)
		fmt.Printf("--- user prompt ---\n%s\n", e.UserPrompt)
		fmt.Printf("--- history snapshot ---\n%s\n", e.HistorySnapshot)
		fmt.Println()
	}
}
