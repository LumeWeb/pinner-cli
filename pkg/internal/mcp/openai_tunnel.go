package mcp

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

var openAITunnelID = regexp.MustCompile(`^tunnel_[0-9a-f]{32}$`)

// openAITunnel manages the official OpenAI tunnel-client process. OpenAI
// Secure MCP Tunnel is not a public URL tunnel: ChatGPT connects to the
// OpenAI-hosted endpoint for tunnelID while tunnel-client forwards requests to
// the local MCP endpoint.
type openAITunnel struct {
	tunnelBase
	tunnelID string
	apiKey   string
	cmd      *exec.Cmd
	done     chan struct{}
}

func newOpenAITunnel(tunnelID, apiKey string) (Tunnel, error) {
	if !openAITunnelID.MatchString(tunnelID) {
		return nil, fmt.Errorf("invalid OpenAI tunnel ID %q: expected tunnel_ followed by 32 lowercase hexadecimal characters", tunnelID)
	}
	if apiKey == "" {
		return nil, fmt.Errorf("OpenAI Secure MCP Tunnel requires --token or CONTROL_PLANE_API_KEY/OPENAI_API_KEY")
	}
	return &openAITunnel{tunnelID: tunnelID, apiKey: apiKey}, nil
}

func (o *openAITunnel) Name() string               { return "openai" }
func (o *openAITunnel) SupportsCustomDomain() bool { return false }
func (o *openAITunnel) RequiresToken() bool        { return false }

func (o *openAITunnel) URL() (string, error) {
	ready, url := o.getState()
	if !ready {
		return "", errUnavailable
	}
	return url, nil
}

func (o *openAITunnel) Start(ctx context.Context, localAddr string) error {
	if _, err := exec.LookPath("tunnel-client"); err != nil {
		return fmt.Errorf("tunnel-client executable not found on PATH: %w (see https://github.com/openai/tunnel-client/releases/latest)", err)
	}
	localURL, err := urlForOrigin(localAddr)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "tunnel-client",
		"run",
		"--control-plane.tunnel-id", o.tunnelID,
		"--control-plane.api-key", "env:CONTROL_PLANE_API_KEY",
		"--mcp.server-url", "channel=main,url="+localURL+"/mcp",
	)
	cmd.Env = append(os.Environ(), "CONTROL_PLANE_API_KEY="+o.apiKey)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start tunnel-client: %w", err)
	}

	o.mu.Lock()
	o.cmd = cmd
	o.done = make(chan struct{})
	done := o.done
	o.mu.Unlock()
	go func() { _ = cmd.Wait(); close(done) }()

	// The OpenAI endpoint is known from the tunnel ID. tunnel-client itself
	// remains the readiness authority; wait briefly for it to stay alive.
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	select {
	case <-done:
		return fmt.Errorf("tunnel-client exited before becoming ready")
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
	}
	o.setReady("https://api.openai.com/v1/mcp/" + o.tunnelID)
	return nil
}

func (o *openAITunnel) Stop(ctx context.Context) error {
	o.mu.Lock()
	cmd, done := o.cmd, o.done
	o.mu.Unlock()
	if cmd == nil || cmd.Process == nil || done == nil {
		return nil
	}
	select {
	case <-done:
		return nil
	default:
	}
	_ = cmd.Process.Signal(os.Interrupt)
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		_ = cmd.Process.Kill()
		return ctx.Err()
	}
}

func (o *openAITunnel) String() string {
	return strings.TrimSpace(o.tunnelID)
}
