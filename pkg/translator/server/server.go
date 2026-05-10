// Package server implements the gRPC Translator service hosted inside the
// translator subprocess. The active TranslatorChain is stored behind an
// atomic.Pointer so Reconfigure can hot-swap the chain without contending
// with in-flight Translate calls — those continue to drain on the old chain
// pointer they captured at the start of the call.
package server

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/facebookincubator/go-belt/tool/logger"
	llms "github.com/xaionaro-go/streamctl/pkg/llm"
	"github.com/xaionaro-go/streamctl/pkg/translator"
	translator_grpc "github.com/xaionaro-go/streamctl/pkg/translator/grpc/go/translator_grpc"
)

// ErrNoChain is the sentinel surfaced via the in-band Error field on
// Translate / TranslateViaProvider replies when Reconfigure has never
// succeeded (or all attempts failed) and no chain is loaded. Callers should
// retry once the streamd-side supervisor has shipped a valid config. The
// per-RPC handlers MUST NOT return ErrNoChain via the gRPC error channel —
// see the service-level rule in pkg/translator/grpc/translator.proto.
var ErrNoChain = errors.New("translator: no chain configured")

// Server hosts the Translator gRPC service. It owns the active
// TranslatorChain via atomic.Pointer so Reconfigure can swap chains without
// blocking concurrent Translate calls.
type Server struct {
	translator_grpc.UnimplementedTranslatorServer

	// chain holds the live TranslatorChain. Translate Loads it at the start
	// of each call; Reconfigure Stores a freshly built chain after a
	// successful build. A Load that returns nil means "no chain yet" — see
	// ErrNoChain.
	chain atomic.Pointer[llms.TranslatorChain]
}

// New returns a Server with no chain loaded. Callers must invoke SwapChain
// or Reconfigure before Translate becomes usable.
func New() *Server {
	return &Server{}
}

// SwapChain installs newChain as the active chain, replacing whatever was
// there before. Used by tests; production code reaches the same path via
// Reconfigure. Takes ctx so tracing matches the rest of the API surface; no
// other ctx-bound work happens here because atomic.Pointer.Store is wait-free.
func (s *Server) SwapChain(
	ctx context.Context,
	newChain *llms.TranslatorChain,
) {
	logger.Tracef(ctx, "Server.SwapChain")
	defer logger.Tracef(ctx, "/Server.SwapChain")
	s.chain.Store(newChain)
}

// Translate implements translator_grpc.TranslatorServer.
//
// Errors are surfaced in-band on TranslateReply.Error per the service-level
// rule in pkg/translator/grpc/translator.proto. The handler returns
// (reply, nil) for every application-level failure (no chain loaded, chain
// translate failed); the gRPC error channel is reserved for transport
// failures.
func (s *Server) Translate(
	ctx context.Context,
	req *translator_grpc.TranslateRequest,
) (_ret *translator_grpc.TranslateReply, _err error) {
	logger.Tracef(ctx, "Server.Translate")
	defer func() { logger.Tracef(ctx, "/Server.Translate: %v", _err) }()

	chain := s.chain.Load()
	if chain == nil {
		return &translator_grpc.TranslateReply{Error: ErrNoChain.Error()}, nil
	}

	start := time.Now()
	result, outcome, meta, err := chain.TranslateWithOutcomeAndMetadata(ctx, req.GetUser(), req.GetMessage())
	latencyNanos := time.Since(start).Nanoseconds()
	if err != nil {
		return &translator_grpc.TranslateReply{
			Outcome:      mapOutcome(outcome),
			LatencyNanos: latencyNanos,
			Error:        fmt.Sprintf("translate failed: %v", err),
			Timings:      mapTimings(meta),
		}, nil
	}

	return &translator_grpc.TranslateReply{
		Result:       result,
		Outcome:      mapOutcome(outcome),
		LatencyNanos: latencyNanos,
		Timings:      mapTimings(meta),
	}, nil
}

// Reconfigure builds a new chain from the supplied YAML config and atomically
// installs it. On any error during parse/build the old chain is retained and
// the failure is returned via ReconfigureReply.Error (NOT a gRPC error) so the
// streamd-side supervisor can surface the message without tearing the
// subprocess down.
func (s *Server) Reconfigure(
	ctx context.Context,
	req *translator_grpc.ReconfigureRequest,
) (_ret *translator_grpc.ReconfigureReply, _err error) {
	logger.Tracef(ctx, "Server.Reconfigure")
	defer func() { logger.Tracef(ctx, "/Server.Reconfigure: %v", _err) }()

	cfg, err := translator.ParseConfigYAML(req.GetConfigYaml())
	if err != nil {
		// Warn so the subprocess's own log explains the failure even if the
		// supervisor's view of ReconfigureReply.Error is dropped.
		logger.Warnf(ctx, "Reconfigure: parse failed: %v", err)
		return &translator_grpc.ReconfigureReply{
			Applied: false,
			Error:   fmt.Sprintf("parse config: %v", err),
		}, nil
	}

	chain, err := translator.BuildChain(ctx, cfg)
	if err != nil {
		// Warn so the subprocess's own log explains the failure even if the
		// supervisor's view of ReconfigureReply.Error is dropped.
		logger.Warnf(ctx, "Reconfigure: build failed: %v", err)
		return &translator_grpc.ReconfigureReply{
			Applied: false,
			Error:   fmt.Sprintf("build chain: %v", err),
		}, nil
	}

	s.chain.Store(chain)
	return &translator_grpc.ReconfigureReply{Applied: true}, nil
}

// Stats returns a frozen snapshot of the current chain's counters. Returns
// an empty StatsReply when no chain is loaded so the caller does not have to
// special-case the bootstrap window.
func (s *Server) Stats(
	ctx context.Context,
	req *translator_grpc.StatsRequest,
) (_ret *translator_grpc.StatsReply, _err error) {
	logger.Tracef(ctx, "Server.Stats")
	defer func() { logger.Tracef(ctx, "/Server.Stats: %v", _err) }()

	chain := s.chain.Load()
	if chain == nil {
		return &translator_grpc.StatsReply{}, nil
	}

	snap := chain.SnapshotStats()
	reply := &translator_grpc.StatsReply{
		TotalTranslated:         snap.TotalTranslated,
		TotalAlreadyTarget:      snap.TotalAlreadyTarget,
		TotalDetectFailed:       snap.TotalDetectFailed,
		TotalSpellingOnly:       snap.TotalSpellingOnly,
		TotalAllProvidersFailed: snap.TotalAllProvidersFailed,
		TotalSkippedQueueFull:   snap.TotalSkippedQueueFull,
		LatencySumNanos:         snap.LatencySumNanos,
		LatencyCount:            snap.LatencyCount,
	}
	for _, p := range snap.Providers {
		reply.Providers = append(reply.Providers, &translator_grpc.ProviderStats{
			Name:                 p.Name,
			TotalCalls:           p.TotalCalls,
			TotalSuccesses:       p.TotalSuccesses,
			TotalErrors:          p.TotalErrors,
			TotalTimeouts:        p.TotalTimeouts,
			TotalQueueRejections: p.TotalQueueRejections,
			LatencySumNanos:      p.LatencySumNanos,
			LatencyCount:         p.LatencyCount,
		})
	}
	return reply, nil
}

// Ping returns immediately so the streamd-side supervisor can distinguish a
// hung subprocess from a slow translation.
func (s *Server) Ping(
	ctx context.Context,
	req *translator_grpc.PingRequest,
) (_ret *translator_grpc.PingReply, _err error) {
	logger.Tracef(ctx, "Server.Ping")
	defer func() { logger.Tracef(ctx, "/Server.Ping: %v", _err) }()

	return &translator_grpc.PingReply{}, nil
}

// ClearHistory drops every recorded ChatHistoryEntry on the active chain and
// returns the dropped count. Returns DroppedEntries=0 + no error when no
// chain is loaded so the caller does not have to special-case the bootstrap
// window.
func (s *Server) ClearHistory(
	ctx context.Context,
	req *translator_grpc.ClearHistoryRequest,
) (_ret *translator_grpc.ClearHistoryReply, _err error) {
	logger.Tracef(ctx, "Server.ClearHistory")
	defer func() { logger.Tracef(ctx, "/Server.ClearHistory: %v", _err) }()

	chain := s.chain.Load()
	if chain == nil {
		return &translator_grpc.ClearHistoryReply{}, nil
	}
	return &translator_grpc.ClearHistoryReply{
		DroppedEntries: chain.ClearHistory(ctx),
	}, nil
}

// TranslateViaProvider bypasses the chain's detector + spelling-fallback
// pipeline and calls one named provider's Translate directly. Used by
// ad-hoc replay tooling to compare a single provider's behaviour without
// the chain's outcome decisioning. The system prompt matches what the chain
// would have used (BuildTranslatePrompt with the current history snapshot)
// so the provider sees the same input it would normally receive.
//
// Errors are surfaced in-band on TranslateViaProviderReply.Error per the
// service-level rule in pkg/translator/grpc/translator.proto: no chain
// loaded, provider not found, and per-call provider failures all flow
// through the same channel.
func (s *Server) TranslateViaProvider(
	ctx context.Context,
	req *translator_grpc.TranslateViaProviderRequest,
) (_ret *translator_grpc.TranslateViaProviderReply, _err error) {
	logger.Tracef(ctx, "Server.TranslateViaProvider")
	defer func() { logger.Tracef(ctx, "/Server.TranslateViaProvider: %v", _err) }()

	chain := s.chain.Load()
	if chain == nil {
		return &translator_grpc.TranslateViaProviderReply{Error: ErrNoChain.Error()}, nil
	}

	target := s.findProviderByName(chain, req.GetProviderName())
	if target == nil {
		names := make([]string, 0, len(chain.Providers))
		for i := range chain.Providers {
			names = append(names, chain.Providers[i].Provider.Name())
		}
		return &translator_grpc.TranslateViaProviderReply{
			Error: fmt.Sprintf("provider %q not found in chain (available: %v)",
				req.GetProviderName(), names),
		}, nil
	}

	systemPrompt := chain.BuildTranslatePrompt(req.GetUser(), "")
	userPrompt := req.GetMessage()

	start := time.Now()
	result, meta, err := target.Provider.TranslateWithMetadata(ctx, systemPrompt, userPrompt)
	latencyNanos := time.Since(start).Nanoseconds()
	if err != nil {
		return &translator_grpc.TranslateViaProviderReply{
			Error:        err.Error(),
			LatencyNanos: latencyNanos,
			Timings:      mapTimings(meta),
		}, nil
	}
	return &translator_grpc.TranslateViaProviderReply{
		Result:       result,
		LatencyNanos: latencyNanos,
		Timings:      mapTimings(meta),
	}, nil
}

// CompileTranslate renders the system + user prompts the chain WOULD emit
// for a Translate call, without invoking any provider. Used by ad-hoc debug
// tooling (`streamcli translator debug compile_translate_request`) to
// inspect / pipe the prompts (e.g. to curl/jq).
//
// history_snapshot is the chain's current history rendered by formatHistory
// (under historyMu); consecutive calls may differ as the chain progresses.
//
// Errors are surfaced in-band on the reply per the service-level rule in
// pkg/translator/grpc/translator.proto.
func (s *Server) CompileTranslate(
	ctx context.Context,
	req *translator_grpc.CompileTranslateRequest,
) (_ret *translator_grpc.CompileTranslateReply, _err error) {
	logger.Tracef(ctx, "Server.CompileTranslate")
	defer func() { logger.Tracef(ctx, "/Server.CompileTranslate: %v", _err) }()

	chain := s.chain.Load()
	if chain == nil {
		return &translator_grpc.CompileTranslateReply{Error: ErrNoChain.Error()}, nil
	}

	history := chain.FormatHistorySnapshot(ctx)
	systemPrompt := chain.BuildTranslatePrompt(req.GetUser(), history)
	return &translator_grpc.CompileTranslateReply{
		SystemPrompt:    systemPrompt,
		UserPrompt:      req.GetMessage(),
		TargetLang:      chain.TargetLang,
		HistorySnapshot: history,
	}, nil
}

// CompileLanguageDetect renders the system + user prompts the chain WOULD
// emit for the language-detection step, without invoking any provider. The
// system prompt uses the deliberately asymmetric "Recent chat:" label
// (translate uses "Recent chat for context:") — see BuildLanguageDetectPrompt.
//
// history_snapshot is the chain's current history rendered by formatHistory
// (under historyMu); consecutive calls may differ as the chain progresses.
//
// Errors are surfaced in-band on the reply per the service-level rule in
// pkg/translator/grpc/translator.proto.
func (s *Server) CompileLanguageDetect(
	ctx context.Context,
	req *translator_grpc.CompileLanguageDetectRequest,
) (_ret *translator_grpc.CompileLanguageDetectReply, _err error) {
	logger.Tracef(ctx, "Server.CompileLanguageDetect")
	defer func() { logger.Tracef(ctx, "/Server.CompileLanguageDetect: %v", _err) }()

	chain := s.chain.Load()
	if chain == nil {
		return &translator_grpc.CompileLanguageDetectReply{Error: ErrNoChain.Error()}, nil
	}

	history := chain.FormatHistorySnapshot(ctx)
	systemPrompt := chain.BuildLanguageDetectPrompt(chain.TargetLangCode(), history)
	return &translator_grpc.CompileLanguageDetectReply{
		SystemPrompt:    systemPrompt,
		UserPrompt:      req.GetMessage(),
		TargetLang:      chain.TargetLang,
		HistorySnapshot: history,
	}, nil
}

// findProviderByName returns the matching ProviderWithSemaphore on the chain
// or nil. Linear scan is acceptable: provider chains are small (typically <10).
func (s *Server) findProviderByName(
	chain *llms.TranslatorChain,
	name string,
) *llms.ProviderWithSemaphore {
	for i := range chain.Providers {
		if chain.Providers[i].Provider.Name() == name {
			return &chain.Providers[i]
		}
	}
	return nil
}

// mapTimings converts the chain-level TranslateMetadata into the proto
// TranslateTimings sub-message. Returns nil when meta is nil so the
// reply's optional `timings` field stays unset and the operator-facing
// renderer (streamcli) can omit the secondary line entirely. Each field
// is wrapped in proto3 optional pointers individually so a provider that
// only populates a subset (none do today, but the surface is ready)
// leaves the rest as "not set".
func mapTimings(meta *llms.TranslateMetadata) *translator_grpc.TranslateTimings {
	if meta == nil {
		return nil
	}
	out := &translator_grpc.TranslateTimings{}
	out.TotalDurationNanos = &meta.TotalDurationNanos
	out.LoadDurationNanos = &meta.LoadDurationNanos
	out.PromptEvalDurationNanos = &meta.PromptEvalDurationNanos
	out.EvalDurationNanos = &meta.EvalDurationNanos
	out.PromptEvalTokens = &meta.PromptEvalTokens
	out.EvalTokens = &meta.EvalTokens
	return out
}

// mapOutcome converts the chain-level Outcome enum to its proto equivalent.
// Cases mirror the five return paths in TranslatorChain.Translate plus the
// not-yet-implemented queue-full skip.
func mapOutcome(o llms.Outcome) translator_grpc.Outcome {
	switch o {
	case llms.OutcomeTranslated:
		return translator_grpc.Outcome_OUTCOME_TRANSLATED
	case llms.OutcomeAlreadyTarget:
		return translator_grpc.Outcome_OUTCOME_ALREADY_TARGET
	case llms.OutcomeDetectFailed:
		return translator_grpc.Outcome_OUTCOME_DETECT_FAILED
	case llms.OutcomeSpellingOnly:
		return translator_grpc.Outcome_OUTCOME_SPELLING_ONLY
	case llms.OutcomeAllProvidersFailed:
		return translator_grpc.Outcome_OUTCOME_ALL_PROVIDERS_FAILED
	case llms.OutcomeSkippedQueueFull:
		return translator_grpc.Outcome_OUTCOME_SKIPPED_QUEUE_FULL
	default:
		return translator_grpc.Outcome_OUTCOME_UNSPECIFIED
	}
}
