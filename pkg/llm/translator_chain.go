package llm

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/facebookincubator/go-belt/tool/logger"
)

type TranslatorChain struct {
	TargetLang string
	Providers  []ProviderWithSemaphore
	historyMu  sync.Mutex
	history    []ChatHistoryEntry
	historyMax int

	// Chain-level counters mirror the five Translate return paths plus the
	// not-yet-implemented queue-full skip. They are read by SnapshotStats
	// and surfaced through the translator subprocess Stats RPC. All access
	// is via atomic.* so callers and the snapshot routine never need to
	// hold a mutex.
	TotalTranslated         atomic.Int64
	TotalAlreadyTarget      atomic.Int64
	TotalDetectFailed       atomic.Int64
	TotalSpellingOnly       atomic.Int64
	TotalAllProvidersFailed atomic.Int64
	// TODO(#8): increment from chain queue-full path; see translator
	// queue-overflow task. Wired through the proto/Stats now so the
	// subprocess <-> supervisor surface won't churn when the increment
	// path lands.
	TotalSkippedQueueFull atomic.Int64

	// Per-call latency aggregate. Average = LatencySumNanos / LatencyCount,
	// computed at read time to avoid integer-rounding drift on every call.
	LatencySumNanos atomic.Int64
	LatencyCount    atomic.Int64
}

// Outcome enumerates the disposition the chain applied to one Translate call.
// Mirrors the five return paths in Translate plus the future "queue full" skip.
type Outcome int32

const (
	OutcomeUnspecified Outcome = iota
	OutcomeTranslated
	OutcomeAlreadyTarget
	OutcomeDetectFailed
	OutcomeSpellingOnly
	OutcomeAllProvidersFailed
	OutcomeSkippedQueueFull
)

// TranslatorChainStats is the read-time view of chain + per-provider counters
// returned by SnapshotStats. Fields use plain int64 because the snapshot is a
// frozen view; live counters live on atomic.Int64 in the chain/provider
// structs.
type TranslatorChainStats struct {
	TotalTranslated         int64
	TotalAlreadyTarget      int64
	TotalDetectFailed       int64
	TotalSpellingOnly       int64
	TotalAllProvidersFailed int64
	TotalSkippedQueueFull   int64
	LatencySumNanos         int64
	LatencyCount            int64
	Providers               []ProviderStatsSnapshot
}

// ProviderStatsSnapshot is the per-provider counter view returned by
// SnapshotStats. Includes only ProviderWithSemaphore counter fields plus the
// provider's Name() so callers can render dashboards without holding a
// reference to the live chain.
type ProviderStatsSnapshot struct {
	Name                 string
	TotalCalls           int64
	TotalSuccesses       int64
	TotalErrors          int64
	TotalTimeouts        int64
	TotalQueueRejections int64
	LatencySumNanos      int64
	LatencyCount         int64
}

const defaultCircuitBreakerThreshold = 3
const defaultProviderTimeout = 30 * time.Second

type ProviderWithSemaphore struct {
	Provider                Provider
	Semaphore               chan struct{} // capacity = parallelism (concurrent execution slots)
	MaxQueueSize            int64
	Queued                  atomic.Int64 // current number of goroutines waiting for a slot
	Timeout                 time.Duration
	CircuitBreakerThreshold int64
	CircuitBreakerCooldown  time.Duration
	ConsecutiveFails        atomic.Int64
	LastFailTime            atomic.Int64 // unix nanos

	// Per-provider counters. callProvider/acquireSemaphore update them on
	// each invocation; SnapshotStats reads them through atomic.Load.
	TotalCalls           atomic.Int64
	TotalSuccesses       atomic.Int64
	TotalErrors          atomic.Int64
	TotalTimeouts        atomic.Int64
	TotalQueueRejections atomic.Int64
	LatencySumNanos      atomic.Int64
	LatencyCount         atomic.Int64
}

type ChatHistoryEntry struct {
	User    string
	Message string
}

// ProviderEntry holds the configuration for adding a provider to the chain.
type ProviderEntry struct {
	Provider                Provider
	Parallelism             int
	MaxQueueSize            int
	Timeout                 time.Duration
	CircuitBreakerThreshold int64
	CircuitBreakerCooldown  time.Duration
}

func NewTranslatorChain(
	targetLang string,
	historyMax int,
	entries []ProviderEntry,
) *TranslatorChain {
	chain := &TranslatorChain{
		TargetLang: targetLang,
		historyMax: historyMax,
	}

	for _, e := range entries {
		par := e.Parallelism
		if par <= 0 {
			par = 1
		}
		// Semaphore capacity = parallelism + max_queue_size.
		cbThreshold := e.CircuitBreakerThreshold
		if cbThreshold <= 0 {
			cbThreshold = defaultCircuitBreakerThreshold
		}
		cbCooldown := e.CircuitBreakerCooldown
		if cbCooldown <= 0 {
			cbCooldown = 30 * time.Second
		}
		chain.Providers = append(chain.Providers, ProviderWithSemaphore{
			Provider:                e.Provider,
			Semaphore:               make(chan struct{}, par),
			MaxQueueSize:            int64(e.MaxQueueSize),
			Timeout:                 e.Timeout,
			CircuitBreakerThreshold: cbThreshold,
			CircuitBreakerCooldown:  cbCooldown,
		})
	}

	return chain
}

// Translate is the legacy entry point that returns just the translated text.
// New callers SHOULD use TranslateWithOutcome (or TranslateWithOutcomeAndMetadata
// when they also need backend timings) so they can act on the outcome (for
// telemetry, queue-full handling, etc.). Translate keeps the public signature
// stable for in-tree callers that don't yet need the outcome.
func (tc *TranslatorChain) Translate(
	ctx context.Context,
	user string,
	message string,
) (string, error) {
	result, _, _, err := tc.TranslateWithOutcomeAndMetadata(ctx, user, message)
	return result, err
}

// TranslateWithOutcome runs the full chain pipeline and returns the
// translated text alongside the disposition the chain applied. Wraps
// TranslateWithOutcomeAndMetadata for callers that don't need backend
// timings. It increments the chain-level and per-provider atomic counters
// at every return path so SnapshotStats reflects observed behaviour
// without a separate ledger.
func (tc *TranslatorChain) TranslateWithOutcome(
	ctx context.Context,
	user string,
	message string,
) (string, Outcome, error) {
	result, outcome, _, err := tc.TranslateWithOutcomeAndMetadata(ctx, user, message)
	return result, outcome, err
}

// TranslateWithOutcomeAndMetadata is the full-fat entry point: result, the
// chain disposition, the optional per-call backend metadata from the
// provider that satisfied the call (nil when no provider populated
// metadata, or when the call did not reach a real Translate), and any
// error. Used by the subprocess server to expose timings to operator
// tooling without breaking the existing TranslateWithOutcome signature.
func (tc *TranslatorChain) TranslateWithOutcomeAndMetadata(
	ctx context.Context,
	user string,
	message string,
) (_ret string, _outcome Outcome, _meta *TranslateMetadata, _err error) {
	start := time.Now()
	defer func() {
		elapsed := time.Since(start)
		tc.LatencySumNanos.Add(int64(elapsed))
		tc.LatencyCount.Add(1)
	}()

	translateStart := start
	logger.Tracef(ctx, "TranslatorChain.Translate")
	defer func() {
		logger.Debugf(ctx, "TranslatorChain.Translate[%s] %q: %v", user, message, time.Since(translateStart).Round(time.Millisecond))
	}()

	// Two-step: detect language first, translate only if needed.
	history := tc.formatHistory(ctx)

	targetCode := tc.TargetLangCode()

	detectPrompt := tc.BuildLanguageDetectPrompt(targetCode, history)

	detectResult, detectErr := tc.callFirstAvailableProvider(ctx, detectPrompt, message)
	if detectErr != nil {
		logger.Warnf(ctx, "language detection failed: %v", detectErr)
		tc.addToHistory(ctx, user, message)
		tc.TotalDetectFailed.Add(1)
		return message, OutcomeDetectFailed, nil, nil
	}

	isTarget, langCode := parseDetectResult(detectResult, targetCode)
	logger.Debugf(ctx, "language detection for [%s] %q: isTarget=%v lang=%q (raw: %q)",
		user, message, isTarget, langCode, detectResult)

	// Both signals must agree: the model said target AND the top language is target.
	// Disagreement (e.g. IS_TARGET=YES but LANGUAGES top=pt) means the model treated
	// foreign function words as proper nouns — proceed with translation.
	// Track whether we forced translation so we can skip the spelling-correction heuristic
	// below (a real translation like "Oi"→"Hi" would otherwise be incorrectly discarded).
	forcedTranslate := containsNonTargetVocabulary(message) || (isTarget && langCode != targetCode)
	if !forcedTranslate && isTarget {
		tc.addToHistory(ctx, user, message)
		tc.TotalAlreadyTarget.Add(1)
		return message, OutcomeAlreadyTarget, nil, nil
	}
	if containsNonTargetVocabulary(message) {
		isTarget = false
	}

	logger.Debugf(ctx, "detected language %q for [%s]: %q", langCode, user, message)

	translatePrompt := tc.BuildTranslatePrompt(user, history)

	userPrompt := message

	var lastErr error
	// anyProviderActuallyFailed distinguishes "every provider was skipped
	// (queue full / circuit open)" from "every provider was attempted and
	// failed". Without this discriminator the loop's all-skipped exit
	// would falsely record OutcomeAllProvidersFailed and log
	// "all LLM providers failed... <nil>" while the proto's
	// OutcomeSkippedQueueFull is the right disposition.
	anyProviderActuallyFailed := false
	for i := range tc.Providers {
		ps := &tc.Providers[i]
		isLast := i == len(tc.Providers)-1

		// Circuit breaker: skip providers with repeated failures,
		// allow a probe after cooldown to detect recovery.
		fails := ps.ConsecutiveFails.Load()
		if fails >= ps.CircuitBreakerThreshold {
			lastFail := time.Unix(0, ps.LastFailTime.Load())
			if time.Since(lastFail) < ps.CircuitBreakerCooldown {
				logger.Debugf(ctx, "provider %s circuit open (%d consecutive failures), skipping",
					ps.Provider.Name(), fails)
				continue
			}
			logger.Debugf(ctx, "provider %s circuit half-open, probing", ps.Provider.Name())
		}

		ok, err := tc.acquireSemaphore(ctx, ps, isLast)
		if err != nil {
			return message, OutcomeUnspecified, nil, err
		}
		if !ok {
			ps.TotalQueueRejections.Add(1)
			continue
		}

		result, meta, err := tc.callProvider(ctx, ps, translatePrompt, userPrompt)
		if err != nil {
			ps.ConsecutiveFails.Add(1)
			ps.LastFailTime.Store(time.Now().UnixNano())
			lastErr = err
			anyProviderActuallyFailed = true
			continue
		}

		ps.ConsecutiveFails.Store(0)

		// If the "translation" is just spelling correction of the original
		// (same words, fixed typos/capitalization/punctuation), return original unchanged.
		// Skip when translation was forced (non-target vocab or detect/lang disagreement) —
		// a short real translation like "Oi"→"Hi" would otherwise be discarded as a typo fix.
		// Surface the metadata even on the spelling-only path: the provider call still
		// happened, so its timings remain useful for diagnosing slowness.
		if !forcedTranslate && isSpellingCorrectionOnly(message, result) {
			logger.Debugf(ctx, "translation of [%s] is spelling correction only, keeping original: %q -> %q",
				user, message, result)
			tc.addToHistory(ctx, user, message)
			tc.TotalSpellingOnly.Add(1)
			return message, OutcomeSpellingOnly, meta, nil
		}

		tc.addToHistory(ctx, user, message)

		if result != message {
			logger.Debugf(ctx, "translated [%s] via %s: %q -> %q",
				user, ps.Provider.Name(), message, result)
		}

		tc.TotalTranslated.Add(1)
		return result, OutcomeTranslated, meta, nil
	}

	// Loop exited without translating. Two distinct dispositions:
	//   1. Every provider was attempted and failed (lastErr is real, OR
	//      anyProviderActuallyFailed flagged a hard failure earlier) →
	//      OutcomeAllProvidersFailed.
	//   2. Every provider was skipped before reaching Translate
	//      (semaphore queue full and/or circuit breaker open). lastErr is
	//      nil; we never logged a real provider error. →
	//      OutcomeSkippedQueueFull (the proto bucket reserved for this
	//      case; covers circuit-open as well — the operator-meaningful
	//      distinction is "translator did not actually try", not the
	//      specific reason for the skip).
	tc.addToHistory(ctx, user, message)
	if anyProviderActuallyFailed {
		logger.Errorf(ctx, "all LLM providers failed, returning original message: %v", lastErr)
		tc.TotalAllProvidersFailed.Add(1)
		return message, OutcomeAllProvidersFailed, nil, nil
	}
	logger.Warnf(ctx, "every provider was skipped (queue full or circuit open), returning original message")
	tc.TotalSkippedQueueFull.Add(1)
	return message, OutcomeSkippedQueueFull, nil, nil
}

// acquireSemaphore attempts to acquire a slot on the provider's semaphore.
// For non-last providers it returns (false, nil) when at capacity.
// For the last provider it blocks until acquired or ctx is cancelled.
func (tc *TranslatorChain) acquireSemaphore(
	ctx context.Context,
	ps *ProviderWithSemaphore,
	isLast bool,
) (bool, error) {
	// Check queue depth limit (0 = no queueing for non-last providers).
	queued := ps.Queued.Add(1)
	defer ps.Queued.Add(-1)

	maxWaiters := ps.MaxQueueSize + int64(cap(ps.Semaphore))
	if !isLast && queued > maxWaiters {
		logger.Debugf(ctx, "provider %s queue full (%d/%d), trying next",
			ps.Provider.Name(), queued, maxWaiters)
		return false, nil
	}

	select {
	case ps.Semaphore <- struct{}{}:
		return true, nil
	case <-ctx.Done():
		return false, ctx.Err()
	}
}

// callProvider invokes TranslateWithMetadata on a single provider with
// timeout and semaphore management. The returned metadata is the raw
// per-call backend telemetry from the provider that satisfied the call
// (nil when the provider does not populate it).
func (tc *TranslatorChain) callProvider(
	ctx context.Context,
	ps *ProviderWithSemaphore,
	systemPrompt string,
	userPrompt string,
) (_ret string, _meta *TranslateMetadata, _err error) {
	logger.Tracef(ctx, "TranslatorChain.callProvider[%s]", ps.Provider.Name())
	defer func() { logger.Tracef(ctx, "/TranslatorChain.callProvider[%s]: %v", ps.Provider.Name(), _err) }()
	defer func() { <-ps.Semaphore }()

	callCtx := ctx
	var cancel context.CancelFunc
	timeout := ps.Timeout
	if timeout <= 0 {
		timeout = defaultProviderTimeout
	}
	callCtx, cancel = context.WithTimeout(ctx, timeout)
	defer cancel()

	ps.TotalCalls.Add(1)
	callStart := time.Now()
	result, meta, err := ps.Provider.TranslateWithMetadata(callCtx, systemPrompt, userPrompt)
	elapsed := time.Since(callStart)
	ps.LatencySumNanos.Add(int64(elapsed))
	ps.LatencyCount.Add(1)
	if err != nil {
		ps.TotalErrors.Add(1)
		switch {
		case errors.Is(callCtx.Err(), context.DeadlineExceeded):
			ps.TotalTimeouts.Add(1)
			logger.Warnf(ctx, "provider %s timed out after %s", ps.Provider.Name(), ps.Timeout)
		default:
			logger.Warnf(ctx, "provider %s failed: %v", ps.Provider.Name(), err)
		}
		return "", nil, err
	}

	ps.TotalSuccesses.Add(1)
	return strings.TrimSpace(result), meta, nil
}

func (tc *TranslatorChain) callFirstAvailableProvider(
	ctx context.Context,
	systemPrompt string,
	userPrompt string,
) (_ret string, _err error) {
	logger.Tracef(ctx, "TranslatorChain.callFirstAvailableProvider")
	defer func() { logger.Tracef(ctx, "/TranslatorChain.callFirstAvailableProvider: %v", _err) }()

	for i := range tc.Providers {
		ps := &tc.Providers[i]
		select {
		case ps.Semaphore <- struct{}{}:
		default:
			continue
		}
		result, err := tc.providerCallWithTimeout(ctx, ps, systemPrompt, userPrompt)
		<-ps.Semaphore
		if err != nil {
			continue
		}
		return strings.TrimSpace(result), nil
	}

	last := &tc.Providers[len(tc.Providers)-1]
	select {
	case last.Semaphore <- struct{}{}:
		result, err := tc.providerCallWithTimeout(ctx, last, systemPrompt, userPrompt)
		<-last.Semaphore
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(result), nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// providerCallWithTimeout invokes Translate under the provider's configured
// per-call timeout (defaultProviderTimeout when zero). Without this the
// detection step could hang indefinitely on a misbehaving provider.
func (tc *TranslatorChain) providerCallWithTimeout(
	ctx context.Context,
	ps *ProviderWithSemaphore,
	systemPrompt string,
	userPrompt string,
) (string, error) {
	timeout := ps.Timeout
	if timeout <= 0 {
		timeout = defaultProviderTimeout
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return ps.Provider.Translate(callCtx, systemPrompt, userPrompt)
}

// ClearHistory drops every recorded ChatHistoryEntry. Returns the count of
// dropped entries. Held under historyMu so it serialises against
// addToHistory and formatHistory; an in-flight Translate that already
// captured a formatted history snapshot completes against the pre-clear
// context (acceptable: the in-flight call's context is what the LLM saw).
func (tc *TranslatorChain) ClearHistory(
	ctx context.Context,
) int32 {
	logger.Tracef(ctx, "TranslatorChain.ClearHistory")
	defer logger.Tracef(ctx, "/TranslatorChain.ClearHistory")

	tc.historyMu.Lock()
	defer tc.historyMu.Unlock()

	n := int32(len(tc.history))
	tc.history = nil
	return n
}

func (tc *TranslatorChain) addToHistory(
	ctx context.Context,
	user string,
	message string,
) {
	logger.Tracef(ctx, "TranslatorChain.addToHistory")
	defer func() { logger.Tracef(ctx, "/TranslatorChain.addToHistory") }()

	tc.historyMu.Lock()
	defer tc.historyMu.Unlock()

	tc.history = append(tc.history, ChatHistoryEntry{User: user, Message: message})
	if len(tc.history) > tc.historyMax {
		tc.history = tc.history[len(tc.history)-tc.historyMax:]
	}
}

func (tc *TranslatorChain) formatHistory(
	ctx context.Context,
) string {
	logger.Tracef(ctx, "TranslatorChain.formatHistory")
	defer func() { logger.Tracef(ctx, "/TranslatorChain.formatHistory") }()

	tc.historyMu.Lock()
	defer tc.historyMu.Unlock()

	var b strings.Builder
	for _, e := range tc.history {
		fmt.Fprintf(&b, "<%s> %s\n", e.User, e.Message)
	}
	return b.String()
}

// FormatHistorySnapshot returns the chain's current history rendered as a
// flat "<user> message\n" string under historyMu. Exposed so out-of-package
// callers (e.g. the subprocess gRPC server's compile-prompt handlers) can
// snapshot the same history shape the chain itself feeds to providers.
// Consecutive calls may differ as the chain progresses.
func (tc *TranslatorChain) FormatHistorySnapshot(
	ctx context.Context,
) string {
	return tc.formatHistory(ctx)
}

// TargetLangCode returns the 2-letter lowercase ISO 639-1 code derived from
// TargetLang the same way TranslateWithOutcome derives it: lowercase the
// first two bytes. Exposed so out-of-package callers compute the detect
// step's targetCode argument from the same source of truth as the chain.
func (tc *TranslatorChain) TargetLangCode() string {
	if len(tc.TargetLang) < 2 {
		return strings.ToLower(tc.TargetLang)
	}
	return strings.ToLower(tc.TargetLang[:2])
}

// langNameToCode maps common language names/variants to ISO 639-1 codes.
var langNameToCode = map[string]string{
	"english":    "en",
	"turkish":    "tr",
	"hindi":      "hi",
	"french":     "fr",
	"russian":    "ru",
	"portuguese": "pt",
	"indonesian": "id",
	"arabic":     "ar",
	"korean":     "ko",
	"hebrew":     "he",
	"spanish":    "es",
	"german":     "de",
	"italian":    "it",
	"japanese":   "ja",
	"chinese":    "zh",
	"dutch":      "nl",
	"polish":     "pl",
	"swedish":    "sv",
	"thai":       "th",
	"vietnamese": "vi",
	"greek":      "el",
	"czech":      "cs",
	"romanian":   "ro",
	"hungarian":  "hu",
	"filipino":   "tl",
	"tagalog":    "tl",
	"malay":      "ms",
	"persian":    "fa",
	"farsi":      "fa",
	"ukrainian":  "uk",
	"bengali":    "bn",
	"tamil":      "ta",
	"urdu":       "ur",
	"albanian":   "sq",
}

// normalizeLangCode converts a language name or code to a 2-letter ISO 639-1 code.
func normalizeLangCode(s string) string {
	// Strip regional subtag: "pt-BR" → "pt", "zh-CN" → "zh".
	if idx := strings.IndexByte(s, '-'); idx > 0 {
		s = s[:idx]
	}
	if len(s) == 2 {
		return s
	}
	if code, ok := langNameToCode[s]; ok {
		return code
	}
	// Try prefix match for partial names (e.g., "engl" → "english" → "en").
	for name, code := range langNameToCode {
		if strings.HasPrefix(name, s) || strings.HasPrefix(s, name) {
			return code
		}
	}
	return s
}

// parseDetectResult parses the structured detection output:
//
//	IS_TARGET: YES or NO
//	LANGUAGES: en:0.95, tr:0.03, ...
//
// Returns (isTarget, topLanguageCode).
func parseDetectResult(raw string, targetCode string) (bool, string) {
	raw = strings.TrimSpace(raw)
	lines := strings.Split(raw, "\n")

	isTarget := false
	langCode := ""

	for _, line := range lines {
		line = strings.TrimSpace(line)
		lower := strings.ToLower(line)

		if strings.HasPrefix(lower, "is_target:") {
			val := strings.TrimSpace(line[len("is_target:"):])
			isTarget = strings.EqualFold(strings.TrimSpace(val), "YES")
		}

		if strings.HasPrefix(lower, "languages:") {
			val := strings.TrimSpace(line[len("languages:"):])
			// Parse "en:0.95, tr:0.03, ..."
			parts := strings.Split(val, ",")
			if len(parts) > 0 {
				first := strings.TrimSpace(parts[0])
				if idx := strings.Index(first, ":"); idx > 0 {
					langCode = normalizeLangCode(strings.ToLower(first[:idx]))
				} else {
					langCode = normalizeLangCode(strings.ToLower(first))
				}
			}
		}
	}

	// Fallback: if parsing failed, treat as target language (don't translate).
	if langCode == "" {
		return true, targetCode
	}

	return isTarget, langCode
}

// normalizeWord lowercases and strips leading/trailing punctuation from a word.
func normalizeWord(w string) string {
	w = strings.ToLower(w)
	return strings.TrimFunc(w, func(r rune) bool {
		return unicode.IsPunct(r) || unicode.IsSymbol(r)
	})
}

// isLatinScript returns true if all letters in the string are Latin script.
func isLatinScript(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) && !unicode.In(r, unicode.Latin) {
			return false
		}
	}
	return true
}

// isSpellingCorrectionOnly returns true when the translation output is merely a
// re-capitalized / re-punctuated version of the input with at most minor word changes,
// which indicates the LLM "translated" broken English into slightly cleaned-up English
// rather than translating from a genuinely foreign language.
func isSpellingCorrectionOnly(input, output string) bool {
	inWords := strings.Fields(input)
	outWords := strings.Fields(output)

	if len(inWords) == 0 || len(outWords) == 0 {
		return false
	}

	// Special case: single-word input → single-word output, both Latin, same first letter,
	// same length. This catches phonetic English slang like "nais"→"nice".
	if len(inWords) == 1 && len(outWords) == 1 {
		ni := normalizeWord(inWords[0])
		no := normalizeWord(outWords[0])
		if ni != "" && no != "" && isLatinScript(ni) && isLatinScript(no) &&
			len(ni) == len(no) && ni[0] == no[0] {
			return true
		}
		return false
	}

	// For multi-word messages: word counts must match for positional alignment.
	if len(inWords) != len(outWords) {
		return false
	}

	// Positional check: count exact matches and near-matches.
	// A "spelling correction" has mostly exact matches with a few near-corrections.
	// A "phonetic translation" has mostly near-matches (all words changed slightly).
	exactMatches := 0
	nearMatches := 0
	farMismatches := 0

	for i := 0; i < len(inWords); i++ {
		ni := normalizeWord(inWords[i])
		no := normalizeWord(outWords[i])
		if ni == no || ni == "" || no == "" {
			exactMatches++
			continue
		}
		if editDistanceClose(ni, no) {
			nearMatches++
		} else {
			farMismatches++
		}
	}

	// If any word is completely different, it's a real translation.
	if farMismatches > 0 {
		return false
	}

	// If all words are near-matches (no exact matches), it's likely phonetic
	// encoding (like "mek naic Famili" → "Make nice family"), not a spelling correction.
	if exactMatches == 0 {
		return false
	}

	// Has both exact matches AND near-matches, with no far mismatches.
	// This is a spelling correction (mostly English with a few typos).
	return true
}

// editDistanceClose returns true if two words are within edit distance 2 of each other.
// Uses a simplified check: if the words share a long common prefix/suffix relative to their length.
func editDistanceClose(a, b string) bool {
	if a == b {
		return true
	}
	la, lb := len(a), len(b)
	if abs(la-lb) > 2 {
		return false
	}
	// Simple check: compute edit distance with early termination.
	return levenshtein(a, b) <= 2
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func min2(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func min3(a, b, c int) int {
	return min2(min2(a, b), c)
}

// levenshtein computes the edit distance between two short strings.
func levenshtein(a, b string) int {
	la, lb := len(a), len(b)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}

	prev := make([]int, lb+1)
	curr := make([]int, lb+1)
	for j := 0; j <= lb; j++ {
		prev[j] = j
	}
	for i := 1; i <= la; i++ {
		curr[0] = i
		for j := 1; j <= lb; j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = min3(prev[j]+1, curr[j-1]+1, prev[j-1]+cost)
		}
		prev, curr = curr, prev
	}
	return prev[lb]
}

// BuildLanguageDetectPrompt returns the system prompt the chain feeds to a
// provider's Translate call for the language-detection step (the step that
// decides whether translation is needed). Exposed so a provider-bypass replay
// path can use the exact prompt the chain would have used, keeping behaviour
// comparable. The targetCode argument is the 2-letter ISO 639-1 code already
// derived from TargetLang by the caller; passing it in keeps this method a
// pure formatter that performs no derivation of its own.
//
// The history label is intentionally "Recent chat:" — this asymmetry against
// BuildTranslatePrompt's "Recent chat for context:" is observable on the
// wire. The chain ships both labels today; do NOT harmonize them as part of
// any other change without an explicit prompt-engineering review.
func (tc *TranslatorChain) BuildLanguageDetectPrompt(
	targetCode string,
	history string,
) string {
	prompt := fmt.Sprintf(
		"Classify this chat message. Reply with EXACTLY this format (no other text):\n"+
			"IS_TARGET: YES or NO\n"+
			"LANGUAGES: code1:confidence1, code2:confidence2, ...\n\n"+
			"IS_TARGET means: is this message written primarily in %s?\n"+
			"Answer YES for %s with typos, slang, abbreviations, or broken grammar.\n"+
			"Answer NO if an %s-only speaker would NOT understand the overall meaning of the message.\n"+
			"Foreign proper nouns (country names, city names, brand names) do not count — "+
			"if the sentence structure and grammar are %s, answer YES even with a foreign proper noun in it.\n\n"+
			"Where confidence is 0.0 to 1.0. List up to 3 most likely ISO 639-1 language codes.\n\n"+
			"Target language: %s (%s)\n"+
			"/no_think",
		tc.TargetLang, tc.TargetLang, tc.TargetLang,
		tc.TargetLang,
		tc.TargetLang, targetCode,
	)
	if history != "" {
		prompt += "\n\nRecent chat:\n" + history
	}
	return prompt
}

// BuildTranslatePrompt returns the system prompt the chain feeds to a provider's
// Translate call when actually translating (i.e. after the detector has decided
// translation is needed). Exposed so a provider-bypass replay path can use the
// exact prompt the chain would have used, keeping behaviour comparable.
func (tc *TranslatorChain) BuildTranslatePrompt(
	user string,
	history string,
) string {
	prompt := "Translate this chat message to " + tc.TargetLang + ". " +
		"The message may be mixed. Rules:\n" +
		"- Translate ALL non-" + tc.TargetLang + " words to " + tc.TargetLang + "\n" +
		"- Turkish endearments: aşkım/aşkim→my love, canım→my dear/my soul, güzel→beautiful\n" +
		"- Turkish: arkadaş/arkadaşım = friend/my friend (NOT 'dear' — 'dear' is canım)\n" +
		"- Turkish: havalı/havalısın = cool/stylish (NOT 'beautiful' or 'great' or 'good' — beautiful=güzel)\n" +
		"- Turkish imperatives: gönder/gonder = send (imperative, NOT past tense 'sent'). bana hediye gonder = send me a gift. Turkish bare verbs are commands/requests.\n" +
		"- Turkish: açıktım = 'I got hungry' (NOT 'turned on' or 'open')\n" +
		"- Turkish: 'o' is a pronoun meaning she/he/it/those — translate as 'those'/'they'/'it', NEVER as English 'oh'\n" +
		"  Example: 'o hep hazır yiyecekler' → 'those are always ready-made foods'\n" +
		"- Turkish: küsmek/küserim = to sulk, to give the cold shoulder, to stop talking out of offense. NEVER translate as 'fed up' or 'angry'. Example: 'sizden küserim' → 'I'll sulk at you' or 'I'll give you the cold shoulder'\n" +
		"- Turkish: 'misafir geleceğim/geleçeğim' = 'I will come as a guest' (the SPEAKER is visiting someone). The subject is 'I' (first person -im suffix). Do NOT translate as 'a guest is coming to me' — that reverses the meaning.\n" +
		"- Turkish: sıkılmak/sıkıldıysan = to be BORED (not 'tired'). Example: 'benden sıkıldıysan' → 'if you're bored of me'\n" +
		"- Russian slang: 'епта'/'ёпта' is a vulgar filler (like 'damn'), NOT an endearment\n" +
		"- Transliterated Russian/Ukrainian/Slavic: 'khavla hospodu' = 'praise the Lord' (хвала Господу). 'dobryi den' = 'good day' (добрый день, NOT evening). 'harasho'/'khorosho' = 'good/fine' (хорошо, NOT hello). 'kraciva'/'krasiva' = 'beautiful' (красива). 'perviy' = 'first' (первый). 'shcho tse' = 'what is this' (Ukrainian що це).\n" +
		"- French abbreviations: 'slt' = 'salut' (hello/hi)\n" +
		"- Indonesian: nyuci/mencuci=washing, masak=cooking, makan=eating, brpa/berapa=how much/what time, nambah cantik=getting more beautiful/prettier\n" +
		"- USERNAMES: If the sender's username contains a word that also appears in the message, that word is a name — keep it as-is, do NOT translate it. Example: user 'DewaJon' writes 'dewa juga lagi masak' — 'dewa' is their name, NOT the word for 'god'.\n" +
		"- Phonetic text (hay=hi, lov=love, beby=baby, wecap=WhatsApp): interpret and write correct " + tc.TargetLang + "\n" +
		"- Phonetic/broken spelling from non-native speakers: decode each word phonetically. Examples: 'cen'='can', 'ai'='I', 'sey'='say', 'sllava'='slava/glory', 'mek'='make', 'naic'='nice', 'Famili'='family'. Translate the decoded meaning.\n" +
		"  Example: 'cen ai sey sllava Ukraina' → 'Can I say glory to Ukraine'\n" +
		"  Example: 'mek naic Famili' → 'Make nice family'\n" +
		"- Translate MEANING, NEVER transliterate. Hindi/Sanskrit नमस्कार/नमस्ते → 'Hello' (NEVER 'Namaskar' or 'Namaste'). Always use the English equivalent word.\n" +
		"- ABSOLUTELY NEVER add emoji that are not in the original message. Zero new emoji.\n" +
		"- Keep ALL original emoji exactly as-is\n" +
		"- Do NOT add content not implied by the original (no 'my love' unless source says it)\n" +
		"- Output ONLY the translated text, nothing else\n" +
		"The sender's username is: " + user + "\n" +
		"/no_think"
	if history != "" {
		prompt += "\n\nRecent chat for context:\n" + history
	}
	return prompt
}

// SnapshotStats freezes the chain-level and per-provider counters into a
// plain struct that callers can serialise without holding live atomics.
// Reads happen via atomic.Load so the snapshot remains consistent under
// concurrent translation traffic.
func (tc *TranslatorChain) SnapshotStats() TranslatorChainStats {
	out := TranslatorChainStats{
		TotalTranslated:         tc.TotalTranslated.Load(),
		TotalAlreadyTarget:      tc.TotalAlreadyTarget.Load(),
		TotalDetectFailed:       tc.TotalDetectFailed.Load(),
		TotalSpellingOnly:       tc.TotalSpellingOnly.Load(),
		TotalAllProvidersFailed: tc.TotalAllProvidersFailed.Load(),
		TotalSkippedQueueFull:   tc.TotalSkippedQueueFull.Load(),
		LatencySumNanos:         tc.LatencySumNanos.Load(),
		LatencyCount:            tc.LatencyCount.Load(),
	}
	out.Providers = make([]ProviderStatsSnapshot, 0, len(tc.Providers))
	for i := range tc.Providers {
		ps := &tc.Providers[i]
		out.Providers = append(out.Providers, ProviderStatsSnapshot{
			Name:                 ps.Provider.Name(),
			TotalCalls:           ps.TotalCalls.Load(),
			TotalSuccesses:       ps.TotalSuccesses.Load(),
			TotalErrors:          ps.TotalErrors.Load(),
			TotalTimeouts:        ps.TotalTimeouts.Load(),
			TotalQueueRejections: ps.TotalQueueRejections.Load(),
			LatencySumNanos:      ps.LatencySumNanos.Load(),
			LatencyCount:         ps.LatencyCount.Load(),
		})
	}
	return out
}

// nonTargetWords is a set of words/abbreviations from common non-English languages
// that appear in chat and should force translation even if the rest of the message
// looks English. These are words an English-only speaker would NOT understand.
var nonTargetWords = map[string]bool{
	// Indonesian/Malay common chat words and abbreviations
	"brpa": true, "berapa": true, "makan": true, "masak": true,
	"lagi": true, "cantik": true, "sayang": true, "blm": true,
	"belum": true, "udah": true, "sudah": true, "dah": true,
	"nyuci": true, "mencuci": true, "kamu": true, "aku": true,
	"nambah": true, "indah": true, "sekali": true,

	// Russian/Ukrainian/Slavic transliterated words
	"perviy": true, "pervyy": true, "pervy": true,
	"khavla": true, "hospodu": true, "dobryi": true,
	"harasho": true, "khorosho": true, "kraciva": true, "krasiva": true,
	"shcho": true, "tse": true,

	// French abbreviations
	"slt": true,

	// Turkish endearments (often capitalized, mistaken for proper nouns by the detector)
	"aşkım": true, "aşkim": true, "askim": true, "askım": true,
	"canım": true, "canim": true,
	"hayatım": true, "hayatim": true,
	"güzelim": true, "guzelim": true,
	"sevgilim": true,
	"tatlım":   true, "tatlim": true,
	"birtanem": true, "birtanesi": true,

	// Phonetic/broken English from non-native speakers (not real English words)
	"mek": true, "naic": true,
}

// containsNonTargetVocabulary checks if the message contains any known non-English
// vocabulary words that should force translation.
func containsNonTargetVocabulary(message string) bool {
	words := strings.Fields(message)
	for _, w := range words {
		nw := normalizeWord(w)
		if nonTargetWords[nw] {
			return true
		}
	}
	return false
}
