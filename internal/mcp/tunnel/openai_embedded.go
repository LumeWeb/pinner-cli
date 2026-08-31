//go:build !no_tunnel

package tunnel

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	tunnelclient "github.com/openai/tunnel-client"
	"github.com/pterm/pterm"
)

var OpenAITunnelID = regexp.MustCompile(`^tunnel_[0-9a-f]{32}$`)

func RunEmbeddedOpenAITunnel(ctx context.Context, server *mcp.Server, tunnelID, apiKey string) error {
	if server == nil {
		return errors.New("OpenAI Secure MCP Tunnel requires an MCP server")
	}
	if !OpenAITunnelID.MatchString(tunnelID) {
		OpenTunnelDeepLink("openai", "tunnel_id")
		return fmt.Errorf("invalid OpenAI tunnel ID %q: expected tunnel_ followed by 32 lowercase hexadecimal characters (create one in the OpenAI Tunnels page)", tunnelID)
	}
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		OpenTunnelDeepLink("openai", "api_key")
		return errors.New("OpenAI Secure MCP Tunnel requires CONTROL_PLANE_API_KEY or OPENAI_API_KEY (create a Runtime API key in the OpenAI API keys page)")
	}

	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverCtx, stopServer := context.WithCancel(ctx)
	defer stopServer()
	serverDone := make(chan error, 1)
	go func() {
		serverDone <- server.Run(serverCtx, serverTransport)
	}()

	client, err := tunnelclient.New(tunnelclient.Config{
		TunnelID: tunnelID,
		APIKey:   apiKey,
	}, clientTransport)
	if err != nil {
		return fmt.Errorf("construct OpenAI Secure MCP Tunnel client: %w", err)
	}
	if err := client.Start(ctx); err != nil {
		return fmt.Errorf("start OpenAI Secure MCP Tunnel client: %w", err)
	}
	defer func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = client.Stop(stopCtx)
	}()

	if err := client.WaitUntilReady(ctx); err != nil {
		return fmt.Errorf("wait for OpenAI Secure MCP Tunnel readiness: %w", err)
	}

	pterm.Success.Printf("OpenAI Secure MCP Tunnel ID: %s\n", tunnelID)
	pterm.Info.Println("In ChatGPT, choose Connection: Tunnel and select or paste this tunnel ID")
	pterm.Info.Println("Press Ctrl+C to stop")

	select {
	case <-ctx.Done():
		return nil
	case err := <-serverDone:
		if err == nil || errors.Is(err, context.Canceled) {
			return nil
		}
		return fmt.Errorf("embedded MCP server stopped: %w", err)
	case signal := <-client.Done():
		if signal == nil {
			return errors.New("OpenAI Secure MCP Tunnel client stopped")
		}
		return fmt.Errorf("OpenAI Secure MCP Tunnel client stopped: %s", signal)
	}
}
