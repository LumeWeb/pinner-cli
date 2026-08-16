//go:build darwin

package mcp

import (
	"context"
	"os/exec"
)

// installCloudflaredViaPlatformPackageManager installs cloudflared via Homebrew
// when it is present on macOS. It reports success so the caller can re-check
// PATH. Best-effort: a failure is not fatal (the caller falls back to a direct
// download).
func installCloudflaredViaPlatformPackageManager(ctx context.Context) bool {
	if _, err := exec.LookPath("brew"); err != nil {
		return false
	}
	cmd := exec.CommandContext(ctx, "brew", "install", "cloudflared")
	return cmd.Run() == nil
}
