package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

// ngrokTunnel serves a local MCP HTTP server through ngrok. It supports the
// free tier (a provider-assigned *.ngrok-free.app dev domain) and custom
// domains on paid accounts (--url https://<domain>).
//
// The assigned public URL is discovered through ngrok's local Agent HTTP API
// (http://127.0.0.1:4040/api/endpoints), which returns structured JSON, rather
// than by scraping the agent's human log output.
type ngrokTunnel struct {
	tunnelBase
	domain string
	token  string
	cmd    *exec.Cmd
	done   chan struct{}
}

// NewNgrokTunnel returns a tunnel powered by the ngrok agent. token is the
// account authtoken (may be empty if already configured via
// `ngrok config add-authtoken` or the NGROK_AUTHTOKEN environment variable).
// domain, when set, is a custom hostname; ngrok requires a paid account for
// custom domains.
func NewNgrokTunnel(domain, token string) Tunnel {
	return &ngrokTunnel{domain: domain, token: token}
}

// Name implements Tunnel.
func (n *ngrokTunnel) Name() string { return "ngrok" }

// SupportsCustomDomain implements Tunnel.
func (n *ngrokTunnel) SupportsCustomDomain() bool { return true }

// RequiresToken implements Tunnel. ngrok needs an account authtoken in all
// cases (even the free tier), but it may be supplied three ways: via --token,
// via the NGROK_AUTHTOKEN env var, or saved in the ngrok config file by
// `ngrok config add-authtoken`. We only report true when none of those token
// sources is present, so the CLI does not falsely reject a user who has
// configured ngrok out of band. The ngrok config-file probe is centralized in
// hasProviderConfig (which honors NGROK_CONFIG and the per-OS default paths,
// including the Windows LOCALAPPDATA quirk) rather than duplicated here.
func (n *ngrokTunnel) RequiresToken() bool {
	if n.token != "" || os.Getenv("NGROK_AUTHTOKEN") != "" {
		return false
	}
	return !hasProviderConfig("ngrok")
}

// URL implements Tunnel.
func (n *ngrokTunnel) OAuthBaseURL(explicitURL, tunnelURL string) (string, error) {
	if explicitURL != "" {
		return explicitURL, nil
	}
	return tunnelURL, nil
}

func (n *ngrokTunnel) URL() (string, error) {
	ready, url := n.getState()
	if !ready {
		return "", errUnavailable
	}
	return url, nil
}

// Stop implements Tunnel.
func (n *ngrokTunnel) Stop(ctx context.Context) error {
	n.mu.Lock()
	cmd := n.cmd
	done := n.done
	n.mu.Unlock()
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	if done != nil {
		select {
		case <-done:
			return nil
		default:
		}
	}
	_ = cmd.Process.Signal(os.Interrupt)
	if done == nil {
		return nil
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		_ = cmd.Process.Kill()
		return ctx.Err()
	}
}

// Start implements Tunnel.
func (n *ngrokTunnel) Start(ctx context.Context, localAddr string) error {
	if _, err := exec.LookPath("ngrok"); err != nil {
		return fmt.Errorf("ngrok executable not found on PATH: %w (see https://ngrok.com/download)", err)
	}

	_, port, err := splitHostPort(localAddr)
	if err != nil {
		return fmt.Errorf("invalid local address %q: %w", localAddr, err)
	}

	args := []string{"http", port}
	if n.domain != "" {
		args = append(args, "--url", "https://"+strings.TrimPrefix(n.domain, "https://"))
	}
	// Disable the agent log so the human output can never be mistaken for
	// the transport; the assigned URL comes from the Agent API instead.
	args = append(args, "--log", "false")

	cmd := exec.CommandContext(ctx, "ngrok", args...)
	if n.token != "" {
		cmd.Env = append(os.Environ(), "NGROK_AUTHTOKEN="+n.token)
	}
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start ngrok: %w", err)
	}

	n.mu.Lock()
	n.cmd = cmd
	n.done = make(chan struct{})
	done := n.done
	n.mu.Unlock()
	go func() { _ = cmd.Wait(); close(done) }()

	if n.domain != "" {
		// The public URL is known up front; no discovery needed.
		n.setReady("https://" + strings.TrimPrefix(n.domain, "https://"))
		return nil
	}

	// Wait for the assigned free-tier URL to appear in the Agent API.
	n.mu.Lock()
	localPortCopy := port
	n.mu.Unlock()
	return n.waitForEndpoint(ctx, localPortCopy)
}

// waitForEndpoint polls the ngrok Agent API until an HTTP endpoint reports a
// public URL matching localPort, or the context/startup deadline expires.
func (n *ngrokTunnel) waitForEndpoint(ctx context.Context, localPort string) error {
	deadline := time.Now().Add(30 * time.Second)
	client := &http.Client{Timeout: 2 * time.Second}
	for {
		n.mu.Lock()
		done := n.done
		n.mu.Unlock()
		select {
		case <-done:
			return fmt.Errorf("ngrok exited before the tunnel became ready")
		default:
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for ngrok tunnel to become ready")
		}
		if url, ok := ngrokEndpointURL(client, "http://127.0.0.1:4040/api/endpoints", localPort); ok {
			n.setReady(url)
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// ngrokEndpointURL parses the ngrok Agent API endpoint list and returns the
// public URL of the matching tunnel. When localPort is non-empty, only
// endpoints forwarding to that local port are considered, which keeps the
// lookup correct if more than one tunnel runs under the same agent. Returns
// the https URL in preference to http. Uses structured JSON only.
func ngrokEndpointURL(client *http.Client, apiURL, localPort string) (string, bool) {
	resp, err := client.Get(apiURL)
	if err != nil {
		return "", false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", false
	}

	var body struct {
		Endpoints []struct {
			URL      string `json:"url"`
			Upstream struct {
				URL string `json:"url"`
			} `json:"upstream"`
		} `json:"endpoints"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", false
	}

	for _, e := range body.Endpoints {
		if localPort != "" && !strings.Contains(e.Upstream.URL, ":"+localPort) {
			continue
		}
		if strings.HasPrefix(e.URL, "https://") {
			return e.URL, true
		}
	}
	for _, e := range body.Endpoints {
		if localPort != "" && !strings.Contains(e.Upstream.URL, ":"+localPort) {
			continue
		}
		if strings.HasPrefix(e.URL, "http://") {
			return e.URL, true
		}
	}
	return "", false
}
