package mcp

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

// cloudflaredVersion pins the cloudflared release this installer verifies
// against. The download URL is pinned to this tag so the embedded SHA-256
// checksums (cloudflaredChecksums) always match the fetched artifact — a
// "latest" URL would drift between releases and break the verification.
const cloudflaredVersion = "2026.8.2"

// cloudflaredDownloadBase is the GitHub release URL prefix for the pinned
// cloudflared version.
const cloudflaredDownloadBase = "https://github.com/cloudflare/cloudflared/releases/download/" + cloudflaredVersion + "/"

// cloudflaredChecksums pins the SHA-256 of each cloudflared artifact (the raw
// binary on Linux/Windows, the .tgz on macOS) for the platform combinations
// this installer supports. Computed from the official cloudflared release
// v2026.8.2. Verifying before writing the artifact prevents a tampered release
// asset from becoming code executed with the user's privileges.
var cloudflaredChecksums = map[string]string{
	"linux/amd64":   "fcfb02b575a52ca1af2e3267af4e1517bcdeb30ac48c834c69abaed3c0576ad2",
	"linux/arm64":   "7747d94570fb390cf47dcb4f9555c193c6355cda9793f0d878d9049e5d6a7790",
	"linux/386":     "39845d980a4b74b9c84530a28d8fea1fe6c476de26460275602162b349f1cbef",
	"linux/arm":     "19809425f60a6261241dfa66a42b4115bab07c295396a3c4d5d7c247fc4e1412",
	"darwin/amd64":  "f1727723c586500e2092368ae21871b3df7ddfd2cb097f22d81bee4a9c458bb4",
	"darwin/arm64":  "9042c2c5d8b2de78e60f313d5fb31b6c5c1cebde787a3caf1f2c9588084ac442",
	"windows/amd64": "c29eee2b121f5436a642eed69fd9767da7e7b8c510fa50aaa130337f931357b5",
	"windows/386":   "6acb072357618fa16c53c43e05438ed728aacd47119f1c6c3aa1a668c3299b43",
}

// cloudflaredDownloadURL returns the direct-download URL for cloudflared on the
// given OS/arch, or an error if unsupported. macOS ships a .tgz; Linux/Windows
// ship a raw binary.
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

	// Prefer a platform installer when its package manager is present. The
	// per-OS strategy lives in build-tagged files (cloudflared_install_{os}.go)
	// so each platform's install detection stays in its own compilation unit
	// rather than a runtime.GOOS switch.
	if installCloudflaredViaPlatformPackageManager(ctx) {
		if p, perr := exec.LookPath("cloudflared"); perr == nil {
			return p, nil
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

	// Verify the fetched artifact against the pinned SHA-256 before we extract
	// or write it, so a tampered or corrupt release asset cannot become code
	// executed with the user's privileges.
	if sum, ok := cloudflaredChecksums[runtime.GOOS+"/"+runtime.GOARCH]; ok {
		got := sha256.Sum256(body)
		if hex.EncodeToString(got[:]) != sum {
			return "", errors.New("cloudflared download failed checksum verification")
		}
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
