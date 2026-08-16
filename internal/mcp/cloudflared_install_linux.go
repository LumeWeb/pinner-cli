//go:build linux

package mcp

import (
	"context"
	"os/exec"
)

// installCloudflaredViaPlatformPackageManager installs cloudflared via the
// first available distro package manager (apt-get, then dnf) on Linux. It
// reports success so the caller can re-check PATH. Best-effort: a failure is
// not fatal (the caller falls back to a direct download).
func installCloudflaredViaPlatformPackageManager(ctx context.Context) bool {
	for _, pm := range []struct{ bin, args []string }{
		{[]string{"apt-get"}, []string{"install", "-y", "cloudflared"}},
		{[]string{"dnf"}, []string{"install", "-y", "cloudflared"}},
	} {
		if _, err := exec.LookPath(pm.bin[0]); err != nil {
			continue
		}
		pmArgs := append(pm.bin, pm.args...)
		cmd := exec.CommandContext(ctx, pmArgs[0], pmArgs[1:]...)
		if cmd.Run() == nil {
			return true
		}
	}
	return false
}
