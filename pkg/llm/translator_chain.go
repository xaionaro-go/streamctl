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
		"Classify this chat message. Reply with EXACTLY this format (no other text):\n"+
			"IS_TARGET: YES or NO\n"+
			"LANGUAGES: code1:confidence1, code2:confidence2, ...\n\n"+
			"IS_TARGET means: is this message written primarily in %s?\n"+
			"Answer YES for %s with typos, slang, abbreviations, or broken grammar.\n"+
			"Answer NO if the message contains words from other languages that an %s-only speaker would NOT understand.\n\n"+
			"Where confidence is 0.0 to 1.0. List up to 3 most likely ISO 639-1 language codes.\n\n"+
			"Target language: %s (%s)\n"+
			"/no_think",
		tc.TargetLang, tc.TargetLang, tc.TargetLang,
		tc.TargetLang, targetCode,
	)
	if history != "" {
		detectPrompt += "\n\nRecent chat:\n" + history
	}

	detectResult, detectErr := tc.callFirstAvailableProvider(ctx, detectPrompt, message)
	if detectErr != nil {
		logger.Warnf(ctx, "language detection failed: %v", detectErr)
		tc.addToHistory(ctx, user, message)
		return message, nil
	}

	isTarget, langCode := parseDetectResult(detectResult, targetCode)
	logger.Debugf(ctx, "language detection for [%s] %q: isTarget=%v lang=%q (raw: %q)",
		user, message, isTarget, langCode, detectResult)

	if isTarget || langCode == targetCode {
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
		"- Turkish: küsmek/küserim = to sulk, to give the cold shoulder, to stop talking out of offense. NEVER translate as 'fed up' or 'angry'. Example: 'sizden küserim' → 'I'll sulk at you' or 'I'll give you the cold shoulder'\n" +
		"- Turkish: 'misafir geleceğim/geleçeğim' = 'I will come as a guest' (the SPEAKER is visiting someone). The subject is 'I' (first person -im suffix). Do NOT translate as 'a guest is coming to me' — that reverses the meaning.\n" +
		"- Turkish: sıkılmak/sıkıldıysan = to be BORED (not 'tired'). Example: 'benden sıkıldıysan' → 'if you're bored of me'\n" +
		"- Russian slang: 'епта'/'ёпта' is a vulgar filler (like 'damn'), NOT an endearment\n" +
		"- Indonesian: nyuci/mencuci=washing, masak=cooking, makan=eating, brpa/berapa=how much/what time, nambah cantik=getting more beautiful/prettier\n" +
		"- USERNAMES: If the sender's username contains a word that also appears in the message, that word is a name — keep it as-is, do NOT translate it. Example: user 'DewaJon' writes 'dewa juga lagi masak' — 'dewa' is their name, NOT the word for 'god'.\n" +
		"- Phonetic text (hay=hi, lov=love, beby=baby, wecap=WhatsApp): interpret and write correct " + tc.TargetLang + "\n" +
		"- Phonetic/broken spelling from non-native speakers: decode each word phonetically. Examples: 'cen'='can', 'ai'='I', 'sey'='say', 'sllava'='slava/glory', 'mek'='make', 'naic'='nice', 'Famili'='family'. Translate the decoded meaning.\n" +
		"  Example: 'cen ai sey sllava Ukraina' → 'Can I say glory to Ukraine'\n" +
		"  Example: 'mek naic Famili' → 'Make nice family'\n" +
		"- Translate MEANING not transliterate (Hello not Namaste/Namaskar)\n" +
		"- ABSOLUTELY NEVER add emoji that are not in the original message. Zero new emoji.\n" +
		"- Keep ALL original emoji exactly as-is\n" +
		"- Do NOT add content not implied by the original (no 'my love' unless source says it)\n" +
		"- Output ONLY the translated text, nothing else\n" +
		"The sender's username is: " + user + "\n" +
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
	timeout := ps.Timeout
	if timeout <= 0 {
		timeout = defaultProviderTimeout
	}
	callCtx, cancel = context.WithTimeout(ctx, timeout)
	defer cancel()

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
