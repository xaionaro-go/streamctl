package streamd

import "time"

// All timeout / delay constants that govern the translator subsystem live
// here so a single grep finds them. Subprocess-shared values
// (subprocessHealthTimeout, subprocessRestartDelay) intentionally stay in
// subprocess_timeouts.go because they are not translator-specific —
// matching them across supervisors (chat handler + translator) is the
// invariant that file enforces.
const (
	// translatorSocketWaitTimeout bounds how long initTranslatorClient
	// will wait for the subprocess to bind its UNIX socket before giving
	// up. The subprocess normally listens within a few hundred
	// milliseconds; the timeout exists only to keep streamd's startup
	// bounded if exec succeeded but the subprocess is wedged on import.
	// Detection itself is event-driven via fsnotify (see
	// waitForTranslatorSocket); this constant is the upper bound on top
	// of the event, not a poll cadence.
	translatorSocketWaitTimeout = 10 * time.Second

	// translatorReconfigureTimeout bounds the initial Reconfigure RPC.
	// The subprocess applies it synchronously (parse YAML, build chain,
	// swap pointer); if it has not finished within this window something
	// is wrong.
	translatorReconfigureTimeout = 30 * time.Second

	// translatorPerCallTimeout caps a single Translate RPC. Slightly
	// larger than subprocessHealthTimeout so a temporarily slow LLM does
	// not error before the watchdog can act, but bounded enough that a
	// wedged subprocess cannot hold the worker indefinitely until the
	// supervisor restart fires.
	translatorPerCallTimeout = 35 * time.Second

	// translatorStatsTimeout caps a single Stats RPC into the translator
	// subprocess. Stats is meant to be cheap (in-memory snapshot of
	// atomic counters) so 5s is large enough that a healthy subprocess
	// always replies in time, and small enough that an unresponsive
	// subprocess does not stall `streamcli translator stats` for a
	// noticeable interactive wait.
	//
	// Distinct from translatorPerCallTimeout (35s, sized for a slow LLM
	// call): reusing the longer timeout would let a wedged subprocess
	// hold a CLI session for half a minute when the watchdog is already
	// going to restart the subprocess anyway.
	translatorStatsTimeout = 5 * time.Second

	// translatorMutationTimeout caps state-mutating subprocess RPCs that
	// race in-flight Translate calls for the chain's internal locks
	// (history, providers slice). 10s is large enough that a healthy
	// subprocess completes the mutation even when one Translate is
	// mid-prompt, and small enough that a wedged subprocess does not
	// block a CLI invocation for the full per-call budget. Reusing
	// translatorStatsTimeout (5s, doc'd for cheap reads) clipped
	// ClearHistory under translation load.
	translatorMutationTimeout = 10 * time.Second
)
