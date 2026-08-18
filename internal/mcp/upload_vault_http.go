package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"go.lumeweb.com/pinner-cli/internal/core/vault"
)

// vaultHTTPUpload lets a sandboxed MCP App write a picked file into the
// encrypted vault WITHOUT pushing the bytes through the MCP/LLM tool channel.
// It mirrors the httpUpload coordinator used for IPFS/curl uploads, but for
// the vault the underlying write (VaultPutHandler) is synchronous, so a
// PUT drains the request body through that handler and returns the vault
// result directly in the response - no async task handle or poll round-trip.
//
// Like httpUpload it works over BOTH transports:
//   - stdio mode: there is no transport server, so mint() spins up a loopback
//     listener on a random port (baseURL == "") and the PUT route is mounted
//     on that loopback mux via ensureLoopback.
//   - HTTP/tunnel mode: a base URL is set, so serveHTTP mounts the PUT route
//     on the shared transport mux via registerHandlers and the loopback
//     listener is intentionally not started.
//
// The one-time token is the only access control: it is unguessable (128-bit),
// expiring, and single-use, and it is bound to the vault destination path at
// mint time so a caller cannot redirect the bytes to an arbitrary path.
type vaultHTTPUpload struct {
	loopback LoopbackServer

	mu      sync.Mutex
	tokens  map[string]vaultHTTPToken
	maxByte int64
	now     func() time.Time
	put     func(ctx context.Context, r io.Reader, size int64, vaultPath string) (any, error)
}

type vaultHTTPToken struct {
	vaultPath string
	expiresAt time.Time
}

// NewVaultHTTPUpload builds a vault presigned-upload coordinator. put is the
// authenticated vault write (same signature as VaultPutHandler) that a
// minted PUT drains into; a nil put makes mint() register tokens but reject
// the actual write, keeping the coordinator constructible for tests.
func NewVaultHTTPUpload(put func(ctx context.Context, r io.Reader, size int64, vaultPath string) (any, error), maxBytes int64) *vaultHTTPUpload {
	if maxBytes <= 0 {
		maxBytes = int64(defaultRelayMaxBytes) // package relay cap
	}
	return &vaultHTTPUpload{
		tokens:  map[string]vaultHTTPToken{},
		maxByte: maxBytes,
		now:     time.Now,
		put:     put,
	}
}

// SetBaseURL points the coordinator at the externally reachable base URL (the
// public/tunnel URL in HTTP mode, or empty for the loopback-derived URL in
// stdio mode).
func (vu *vaultHTTPUpload) SetBaseURL(url string) {
	vu.loopback.SetBaseURL(url)
}

// AddTrustedOrigins extends the origin corsUpload reflects for the Uppy XHR
// PUT beyond the coordinator's own base/loopback origin, allowing a configured
// MCP host that serves the vault app iframe from its own origin to upload. See
// LoopbackServer.AddTrustedOrigins.
func (vu *vaultHTTPUpload) AddTrustedOrigins(origins ...string) {
	vu.loopback.AddTrustedOrigins(origins...)
}

// Stop shuts down the loopback listener, if any.
func (vu *vaultHTTPUpload) Stop(ctx context.Context) {
	vu.loopback.Stop(ctx)
}

// registerHandlers mounts the one-time vault-upload PUT route on the shared
// mux (HTTP/tunnel mode) or the loopback mux (stdio mode via ensureLoopback).
func (vu *vaultHTTPUpload) registerHandlers(mux *http.ServeMux) {
	mux.HandleFunc("/vault-upload/", corsUpload(vu.allowedUploadOrigins, vu.putHandler))
}

// allowedUploadOrigins is the callback corsUpload uses to scope the reflected
// origin to the vault coordinator's own transport/base origin.
func (vu *vaultHTTPUpload) allowedUploadOrigins() []string { return vu.loopback.AcceptedOrigins() }

// mint validates the destination vault path, registers a fresh one-time
// vault-upload endpoint bound to it, and returns its full URL. It refuses to
// mint outside the allowed upload scope or for a directory/traversal path, so
// a PUT to the minted endpoint can never write anywhere else in the vault. It
// ensures the loopback listener is running in stdio mode so the URL is always
// reachable.
func (vu *vaultHTTPUpload) mint(vaultPath string, ttl time.Duration) (string, error) {
	if err := validateVaultFilePath(vaultPath); err != nil {
		return "", err
	}
	if err := vu.loopback.EnsureLoopback(vu.registerHandlers); err != nil {
		return "", err
	}
	if ttl <= 0 {
		ttl = defaultHTTPUploadTTL
	}
	token := newHTTPToken()
	now := vu.now()
	vu.mu.Lock()
	// Prune expired minted-but-never-used tokens.
	for t, tkn := range vu.tokens {
		if now.After(tkn.expiresAt) {
			delete(vu.tokens, t)
		}
	}
	vu.tokens[token] = vaultHTTPToken{vaultPath: vaultPath, expiresAt: now.Add(ttl)}
	vu.mu.Unlock()
	vu.loopback.mu.Lock()
	defer vu.loopback.mu.Unlock()
	return vu.loopback.URLFor("vault-upload", token), nil
}

// validateVaultFilePath rejects a vault destination unless it is a well-formed
// FILE path (not a directory), free of parent-relative traversal, and not
// addressed through a profile authority. It deliberately does NOT confine the
// path to a single vault folder: a caller may write any file into any vault
// directory (e.g. vault:/docs/f.pdf). This is enforced by the vault_put_file
// tool on every source branch and by the mint coordinator before it binds a
// one-time upload URL to a destination.
func validateVaultFilePath(vaultPath string) error {
	vp, err := vault.ParseVaultPath(vaultPath)
	if err != nil {
		return err
	}
	if vp.Profile != nil {
		return fmt.Errorf("profile-authority vault paths are not supported for upload")
	}
	if vp.IsDir || vp.Name == "" {
		return fmt.Errorf("vault upload destination must be a file path, not a directory")
	}
	// Reject parent-relative traversal: after ParseVaultPath normalizes the
	// directory to a leading-/ absolute form, any remaining ".." segment is an
	// attempt to escape the destination. Also reject ".." or "." in the leaf
	// name segment — a filename of ".." is not a directory traversal in itself
	// but combined with a directory it can resolve to an unintended path.
	names := append(strings.Split(vp.Directory, "/"), vp.Name)
	for _, seg := range names {
		if seg == ".." || seg == "." {
			return fmt.Errorf("vault upload destination %q contains a path-traversal segment", vaultPath)
		}
	}
	return nil
}

// putHandler receives a Uppy XHR PUT (raw file body, formData off) and drains
// it synchronously through the authenticated vault write.
func (vu *vaultHTTPUpload) putHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		w.Header().Set("Allow", http.MethodPut)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	token := strings.TrimPrefix(r.URL.Path, "/vault-upload/")
	if token == "" {
		http.NotFound(w, r)
		return
	}
	vaultPath, ok := vu.claim(token)
	if !ok {
		http.Error(w, "invalid, expired, or already-used vault-upload endpoint", http.StatusNotFound)
		return
	}

	if vu.put == nil {
		http.Error(w, "vault write not available", http.StatusServiceUnavailable)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, vu.maxByte)

	// The vault write itself is synchronous and outlives nothing; keep it on
	// the request context but bound by a transfer budget like the relay path.
	transferCtx, cancel := context.WithTimeout(r.Context(), syncUploadBudget(r.ContentLength))
	defer cancel()
	result, err := vu.put(transferCtx, r.Body, r.ContentLength, vaultPath)
	if err != nil {
		status := http.StatusInternalServerError
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			status = http.StatusRequestEntityTooLarge
		}
		http.Error(w, err.Error(), status)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"vault_path": vaultPath,
		"result":     result,
	})
}

// claim atomically validates and consumes a one-time vault-upload token. It
// reports the bound vault path and false if the token is unknown, expired, or
// already used.
func (vu *vaultHTTPUpload) claim(token string) (string, bool) {
	vu.mu.Lock()
	defer vu.mu.Unlock()
	tkn, ok := vu.tokens[token]
	if !ok || vu.now().After(tkn.expiresAt) {
		return "", false
	}
	delete(vu.tokens, token)
	return tkn.vaultPath, true
}
