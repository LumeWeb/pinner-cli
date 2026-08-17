package service

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// defaultServiceName is used when a Config does not set a Name.
const defaultServiceName = "pinner-mcp"

// CommandRunner runs a system command for effect, returning its error.
type CommandRunner func(context.Context, string, ...string) error

// CommandOutputRunner runs a system command and returns its combined output.
type CommandOutputRunner func(context.Context, string, ...string) (string, error)

// runCommand executes command with args using the host binaries.
func runCommand(ctx context.Context, command string, args ...string) error {
	cmd := exec.CommandContext(ctx, command, args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s failed: %w: %s", command, err, strings.TrimSpace(string(output)))
	}
	return nil
}

// runCommandOutput executes command with args and returns its combined output.
func runCommandOutput(ctx context.Context, command string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, command, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("%s failed: %w: %s", command, err, strings.TrimSpace(string(output)))
	}
	return string(output), nil
}

// execCommandContext builds an *exec.Cmd for the given command and args,
// honoring the context for cancellation. Kept as an indirection so the Logs
// backends can run long-lived tail commands directly (they need cmd.Run(), not
// CombinedOutput, which buffers forever on a streaming tail). Stdout and stderr
// are wired to the process's own so streaming output (journalctl, log show)
// reaches the caller instead of being discarded by the default DevNull.
func execCommandContext(ctx context.Context, command string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd
}
