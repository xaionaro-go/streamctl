package streamd

import (
	"context"
	"os/exec"
	"sync/atomic"
)

// externalChatHandler tracks a spawned chat handler process.
type externalChatHandler struct {
	cmd             *exec.Cmd
	cancelFunc      context.CancelFunc
	lastMessageTime atomic.Int64 // UnixNano of last keepalive from this handler
}
