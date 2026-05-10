package streamd

import "time"

// subprocessHealthTimeout is the supervisor watchdog limit shared by every
// streamd-spawned subprocess (chat handlers, translator). When the
// subprocess has not heartbeated within this window it is declared dead and
// restarted.
//
// subprocessRestartDelay is the cool-down between a death-detected and the
// next spawn attempt; matched across supervisors so a flapping subprocess
// cannot slam the system at full speed.
//
// Both values are intentionally constants of pkg/streamd rather than
// per-supervisor consts: operators do not want to remember two different
// numbers, and the failure modes the watchdog defends against (wedged
// goroutine, lost socket, etc.) do not differ per subprocess kind.
const (
	subprocessHealthTimeout = 30 * time.Second
	subprocessRestartDelay  = 5 * time.Second
)
