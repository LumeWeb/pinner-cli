package mcp

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

// cloudflaredDownloadBase is the GitHub release URL prefix for cloudflared.
const cloudflaredDownloadBase = "https://github.com/cloudflare/cloudflared/releases/latest/download/"

// cloudflaredDownloadURL returns the direct-download URL for cloudflared on the
// given OS/arch, or an error if unsupported. macOS and some platforms ship a
// .tgz; Linux/Windows ship a raw binary.
func cloudflaredDownloadURL(goos, goarch string) (string, error) {
	switch goos {
	case "linux":
		switch goarch {
		case "amd64":
			return cloudflaredDownloadBase + "cloudflared-linux-amd64", nil
		case "386":
			return cloudflaredDownloadBase + "cloudflared-linux-386", nil
		case "arm64":
			return cloudflaredDownloadBase + "cloudflared-linux-arm64", nil
		case "arm":
			return cloudflaredDownloadBase + "cloudflared-linux-arm", nil
		}
	case "darwin":
		switch goarch {
		case "amd64":
			return cloudflaredDownloadBase + "cloudflared-darwin-amd64.tgz", nil
		case "arm64":
			return cloudflaredDownloadBase + "cloudflared-darwin-arm64.tgz", nil
		}
	case "windows":
		switch goarch {
		case "amd64":
			return cloudflaredDownloadBase + "cloudflared-windows-amd64.exe", nil
		case "386":
			return cloudflaredDownloadBase + "cloudflared-windows-386.exe", nil
		}
	}
	return "", fmt.Errorf("no cloudflared download for %s/%s", goos, goarch)
}

// ensureCloudflaredBinary makes a cloudflared binary available, preferring the
// platform package manager and falling back to a direct download. It returns
// the resolved binary path. Best-effort: an error means the caller should warn
// and continue (the tunnel will fail at Start with a clear message).
func ensureCloudflaredBinary(ctx context.Context) (string, error) {
	if p, err := exec.LookPath("cloudflared"); err == nil {
		return p, nil
	}

	// Prefer a platform installer when its package manager is present.
	switch runtime.GOOS {
	case "darwin":
		if _, err := exec.LookPath("brew"); err == nil {
			cmd := exec.CommandContext(ctx, "brew", "install", "cloudflared")
			if err := cmd.Run(); err == nil {
				if p, perr := exec.LookPath("cloudflared"); perr == nil {
					return p, nil
				}
			}
		}
	case "linux":
		for _, pm := range []struct{ bin, args []string }{
			{[]string{"apt-get"}, []string{"install", "-y", "cloudflared"}},
			{[]string{"dnf"}, []string{"install", "-y", "cloudflared"}},
		} {
			if _, err := exec.LookPath(pm.bin[0]); err == nil {
				pmArgs := append(pm.bin, pm.args...)
				cmd := exec.CommandContext(ctx, pmArgs[0], pmArgs[1:]...)
				if err := cmd.Run(); err == nil {
					if p, perr := exec.LookPath("cloudflared"); perr == nil {
						return p, nil
					}
				}
			}
		}
	case "windows":
		if _, err := exec.LookPath("winget"); err == nil {
			cmd := exec.CommandContext(ctx, "winget", "install", "Cloudflare.cloudflared", "--accept-source-agreements", "--accept-package-agreements")
			if err := cmd.Run(); err == nil {
				if p, perr := exec.LookPath("cloudflared"); perr == nil {
					return p, nil
				}
			}
		}
	}

	return downloadCloudflared(ctx)
}

// downloadCloudflared downloads the direct binary for the current platform into
// the pinned pinner bin dir, handling the macOS .tgz wrapper, and makes it
// executable.
func downloadCloudflared(ctx context.Context) (string, error) {
	url, err := cloudflaredDownloadURL(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return "", err
	}
	dir, err := cloudflaredBinDir()
	if err != nil {
		return "", err
	}
	target := filepath.Join(dir, cloudflaredExeName())

	httpClient := &http.Client{Timeout: 60 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("download cloudflared: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download cloudflared: HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read cloudflared download: %w", err)
	}
	bin, err := extractCloudflaredArchive(body)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(target, bin, 0o755); err != nil {
		return "", fmt.Errorf("write cloudflared binary: %w", err)
	}
	return target, nil
}

// extractCloudflaredArchive returns the raw executable bytes, unwrapping the
// .tgz wrapper macOS ships if present (plain binaries pass through unchanged).
func extractCloudflaredArchive(body []byte) ([]byte, error) {
	if !bytes.HasPrefix(body, []byte{0x1f, 0x8b}) { // gzip magic
		return body, nil
	}
	gr, err := gzip.NewReader(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("cloudflared .tgz: %w", err)
	}
	defer gr.Close()
	tr := tar.NewReader(gr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("cloudflared .tgz: %w", err)
		}
		if hdr.Typeflag == tar.TypeReg && filepath.Base(hdr.Name) == cloudflaredExeName() {
			b, rerr := io.ReadAll(tr)
			if rerr != nil {
				return nil, rerr
			}
			return b, nil
		}
	}
	return nil, fmt.Errorf("no cloudflared executable found in archive")
}
