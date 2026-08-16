//go:build windows

package mcp

import (
	"context"
	"os/exec"
)

// installCloudflaredViaPlatformPackageManager installs cloudflared via winget
// when it is present on Windows. It reports success so the caller can re-check
// PATH. Best-effort: a failure is not fatal (the caller falls back to a direct
// download).
func installCloudflaredViaPlatformPackageManager(ctx context.Context) bool {
	if _, err := exec.LookPath("winget"); err != nil {
		return false
	}
	cmd := exec.CommandContext(ctx, "winget", "install", "Cloudflare.cloudflared", "--accept-source-agreements", "--accept-package-agreements")
	return cmd.Run() == nil
}
