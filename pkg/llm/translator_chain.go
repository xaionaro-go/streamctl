package llm

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/facebookincubator/go-belt/tool/logger"
)

type TranslatorChain struct {
	TargetLang string
	Providers  []ProviderWithSemaphore
	historyMu  sync.Mutex
	history    []ChatHistoryEntry
	historyMax int
}

const defaultCircuitBreakerThreshold = 3

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

func (tc *TranslatorChain) Translate(
	ctx context.Context,
	user string,
	message string,
) (_ret string, _err error) {
	logger.Tracef(ctx, "TranslatorChain.Translate")
	defer func() { logger.Tracef(ctx, "/TranslatorChain.Translate: %v", _err) }()

	// Two-step: detect language first, translate only if needed.
	history := tc.formatHistory(ctx)

	targetCode := strings.ToLower(tc.TargetLang[:2])

	detectPrompt := fmt.Sprintf(
		"Detect the language of this chat message. Reply with ONLY the ISO 639-1 code "+
			"(en, tr, hi, fr, ru, pt, id, ar, ko, he, etc).\n"+
			"Rules:\n"+
			"- %s with typos/slang/abbreviations (\"u\", \"yr\", \"duing\", \"spieck\", \"Indoneia\", \"fimaly\", \"gud\", \"nais\") → \"%s\"\n"+
			"- CRITICAL: If the message has %s grammar structure (subject-verb-object) but misspelled words → \"%s\"\n"+
			"  Examples: \"can you spieck Indoneia\" → \"%s\", \"Indian fimaly so beautiful\" → \"%s\"\n"+
			"- Mixed with substantial non-%s → the non-%s language code\n"+
			"- Emoji-only or unrecognizable → \"%s\"\n"+
			"- Phonetic approximations by non-native speakers (\"hay\"=\"hi\", \"beby\"=\"baby\", \"lov\"=\"love\", \"wecap\"=\"WhatsApp\") with NO %s sentence structure → \"xx\"\n"+
			"- Indonesian abbreviations (brpa=berapa, JM=jam, blm=belum, dah=sudah) → \"id\"\n"+
			"- Indonesian informal (nyuci=mencuci/washing, masak=cooking) → \"id\"\n"+
			"/no_think",
		tc.TargetLang, targetCode,
		tc.TargetLang, targetCode,
		targetCode, targetCode,
		tc.TargetLang, tc.TargetLang,
		targetCode,
		tc.TargetLang,
	)
	if history != "" {
		detectPrompt += "\n\nRecent chat:\n" + history
	}

	langCode, detectErr := tc.callFirstAvailableProvider(ctx, detectPrompt, message)
	if detectErr != nil {
		logger.Warnf(ctx, "language detection failed: %v", detectErr)
		tc.addToHistory(ctx, user, message)
		return message, nil
	}
	langCode = strings.TrimSpace(strings.ToLower(langCode))

	if langCode == targetCode {
		tc.addToHistory(ctx, user, message)
		return message, nil
	}

	logger.Debugf(ctx, "detected language %q for [%s]: %q", langCode, user, message)

	translatePrompt := "Translate this chat message to " + tc.TargetLang + ". " +
		"The message may be mixed. Rules:\n" +
		"- Translate ALL non-" + tc.TargetLang + " words to " + tc.TargetLang + "\n" +
		"- Turkish endearments: aşkım/aşkim→my love, canım→my dear, güzel→beautiful\n" +
		"- Turkish: açıktım = 'I got hungry' (NOT 'turned on' or 'open')\n" +
		"- Turkish: 'o' is a pronoun meaning she/he/it/those — translate as 'those'/'they'/'it', NEVER as English 'oh'\n" +
		"  Example: 'o hep hazır yiyecekler' → 'those are always ready-made foods'\n" +
		"- Russian slang: 'епта'/'ёпта' is a vulgar filler (like 'damn'), NOT an endearment\n" +
		"- Indonesian: nyuci/mencuci=washing, masak=cooking, makan=eating, brpa/berapa=how much/what time\n" +
		"- Phonetic text (hay=hi, lov=love, beby=baby, wecap=WhatsApp): interpret and write correct " + tc.TargetLang + "\n" +
		"- Translate MEANING not transliterate (Hello not Namaste/Namaskar)\n" +
		"- ABSOLUTELY NEVER add emoji that are not in the original message. Zero new emoji.\n" +
		"- Keep ALL original emoji exactly as-is\n" +
		"- Do NOT add content not implied by the original (no 'my love' unless source says it)\n" +
		"- Output ONLY the translated text, nothing else\n" +
		"/no_think"
	if history != "" {
		translatePrompt += "\n\nRecent chat for context:\n" + history
	}

	userPrompt := message

	var lastErr error
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
			return message, err
		}
		if !ok {
			continue
		}

		result, err := tc.callProvider(ctx, ps, translatePrompt, userPrompt)
		if err != nil {
			ps.ConsecutiveFails.Add(1)
			ps.LastFailTime.Store(time.Now().UnixNano())
			lastErr = err
			continue
		}

		ps.ConsecutiveFails.Store(0)

		tc.addToHistory(ctx, user, message)

		if result != message {
			logger.Debugf(ctx, "translated [%s] via %s: %q -> %q",
				user, ps.Provider.Name(), message, result)
		}

		return result, nil
	}

	// All providers failed -- return original message to avoid blocking chat.
	logger.Errorf(ctx, "all LLM providers failed, returning original message: %v", lastErr)
	tc.addToHistory(ctx, user, message)
	return message, nil
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

// callProvider invokes Translate on a single provider with timeout and semaphore management.
func (tc *TranslatorChain) callProvider(
	ctx context.Context,
	ps *ProviderWithSemaphore,
	systemPrompt string,
	userPrompt string,
) (_ret string, _err error) {
	logger.Tracef(ctx, "TranslatorChain.callProvider[%s]", ps.Provider.Name())
	defer func() { logger.Tracef(ctx, "/TranslatorChain.callProvider[%s]: %v", ps.Provider.Name(), _err) }()
	defer func() { <-ps.Semaphore }()

	callCtx := ctx
	var cancel context.CancelFunc
	if ps.Timeout > 0 {
		callCtx, cancel = context.WithTimeout(ctx, ps.Timeout)
	}
	if cancel != nil {
		defer cancel()
	}

	result, err := ps.Provider.Translate(callCtx, systemPrompt, userPrompt)
	if err != nil {
		switch {
		case errors.Is(callCtx.Err(), context.DeadlineExceeded):
			logger.Warnf(ctx, "provider %s timed out after %s", ps.Provider.Name(), ps.Timeout)
		default:
			logger.Warnf(ctx, "provider %s failed: %v", ps.Provider.Name(), err)
		}
		return "", err
	}

	return strings.TrimSpace(result), nil
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
		result, err := ps.Provider.Translate(ctx, systemPrompt, userPrompt)
		<-ps.Semaphore
		if err != nil {
			continue
		}
		return strings.TrimSpace(result), nil
	}

	last := &tc.Providers[len(tc.Providers)-1]
	select {
	case last.Semaphore <- struct{}{}:
		result, err := last.Provider.Translate(ctx, systemPrompt, userPrompt)
		<-last.Semaphore
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(result), nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
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
