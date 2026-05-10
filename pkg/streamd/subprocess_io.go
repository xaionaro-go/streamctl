package streamd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"

	child_process_manager "github.com/AgustinSRG/go-child-process-manager"
)

// subprocessIO holds the IO configuration applied to every internal
// worker subprocess spawned by streamd. It carries the parent's stdio
// writers — so child stderr-bound logs reach the parent's journald
// stream instead of /dev/null — and the resolved streamd executable
// path used as the spawn target. Constructed once in main and delivered
// via OptionSubprocessIO so that pkg/streamd never references
// os.Stdout / os.Stderr directly (project rule: no stdio outside main).
type subprocessIO struct {
	stdout   io.Writer
	stderr   io.Writer
	execPath string
}

// NewSubprocessIO validates and constructs a subprocessIO. All fields are
// required: nil writers would silently drop subprocess output, and an empty
// execPath would spawn an empty command. Surfacing this at startup is
// preferable to a confusing first-spawn failure.
func NewSubprocessIO(
	stdout io.Writer,
	stderr io.Writer,
	execPath string,
) (subprocessIO, error) {
	switch {
	case stdout == nil:
		return subprocessIO{}, errors.New("subprocess stdout writer must not be nil")
	case stderr == nil:
		return subprocessIO{}, errors.New("subprocess stderr writer must not be nil")
	case execPath == "":
		return subprocessIO{}, errors.New("subprocess execPath must not be empty")
	}
	return subprocessIO{stdout: stdout, stderr: stderr, execPath: execPath}, nil
}

// spawn builds an *exec.Cmd targeting the configured streamd executable,
// wires stdio + child_process_manager, starts the process, and registers
// it with child_process_manager. The caller owns context cancellation —
// pass an already-derived cancellable context. The caller is responsible
// for cmd.Wait().
func (s subprocessIO) spawn(
	ctx context.Context,
	args []string,
) (*exec.Cmd, error) {
	if s.execPath == "" {
		return nil, errors.New("streamd subprocess spawn: OptionSubprocessIO was not passed to streamd.New (required when chat-listener or translator subprocesses are enabled)")
	}
	cmd := exec.CommandContext(ctx, s.execPath, args...)
	cmd.Stdout = s.stdout
	cmd.Stderr = s.stderr
	child_process_manager.ConfigureCommand(cmd)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start subprocess: %w", err)
	}
	child_process_manager.AddChildProcess(cmd.Process)
	return cmd, nil
}
