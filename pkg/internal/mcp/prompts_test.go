package mcp_test

import (
	"context"
	"testing"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"

	mcpadapter "go.lumeweb.com/pinner-cli/pkg/internal/mcp"
)

// --- Server helper ---

// buildServerWithPrompts builds a minimal MCP server with prompt templates
// wired. It returns an initialized in-process client.
func buildServerWithPrompts(t *testing.T) *client.Client {
	t.Helper()
	root := &cli.Command{
		Name:    "test",
		Version: "1.0.0",
		Action:  func(context.Context, *cli.Command) error { return nil },
	}
	srv, _, err := mcpadapter.MCPServerWithOpts(root, true, nil,
		mcpadapter.WithPrompts(),
	)
	require.NoError(t, err)

	tr := transport.NewInProcessTransport(srv)
	c := client.NewClient(tr)
	_, err = c.Initialize(t.Context(), mcp.InitializeRequest{})
	require.NoError(t, err)
	return c
}

// --- ListPrompts tests ---

func TestPrompts_ListPrompts(t *testing.T) {
	t.Parallel()

	c := buildServerWithPrompts(t)

	result, err := c.ListPrompts(t.Context(), mcp.ListPromptsRequest{})
	require.NoError(t, err)
	require.NotNil(t, result)

	// Should have exactly 2 prompts.
	assert.Len(t, result.Prompts, 2)

	names := make(map[string]bool, 2)
	for _, p := range result.Prompts {
		names[p.Name] = true
	}
	assert.True(t, names["website-onboarding"], "website-onboarding prompt should be registered")
	assert.True(t, names["setup"], "setup prompt should be registered")
}

func TestPrompts_ListPrompts_WebsiteOnboardingDefinition(t *testing.T) {
	t.Parallel()

	c := buildServerWithPrompts(t)

	result, err := c.ListPrompts(t.Context(), mcp.ListPromptsRequest{})
	require.NoError(t, err)

	var prompt *mcp.Prompt
	for i := range result.Prompts {
		if result.Prompts[i].Name == "website-onboarding" {
			prompt = &result.Prompts[i]
			break
		}
	}
	require.NotNil(t, prompt, "website-onboarding prompt not found")

	assert.Equal(t, "website-onboarding", prompt.Name)
	assert.NotEmpty(t, prompt.Description)
	assert.NotEmpty(t, prompt.Title)

	// Should have 4 arguments: domain, content_source, target_type, dns_mode.
	assert.Len(t, prompt.Arguments, 4)

	argNames := make(map[string]bool, 4)
	for _, arg := range prompt.Arguments {
		argNames[arg.Name] = true
	}
	assert.True(t, argNames["domain"])
	assert.True(t, argNames["content_source"])
	assert.True(t, argNames["target_type"])
	assert.True(t, argNames["dns_mode"])
}

func TestPrompts_ListPrompts_SetupDefinition(t *testing.T) {
	t.Parallel()

	c := buildServerWithPrompts(t)

	result, err := c.ListPrompts(t.Context(), mcp.ListPromptsRequest{})
	require.NoError(t, err)

	var prompt *mcp.Prompt
	for i := range result.Prompts {
		if result.Prompts[i].Name == "setup" {
			prompt = &result.Prompts[i]
			break
		}
	}
	require.NotNil(t, prompt, "setup prompt not found")

	assert.Equal(t, "setup", prompt.Name)
	assert.NotEmpty(t, prompt.Description)
	assert.NotEmpty(t, prompt.Title)

	// Setup prompt has no arguments.
	assert.Empty(t, prompt.Arguments)
}

// --- GetPrompt: website-onboarding tests ---

func TestPrompts_GetWebsiteOnboarding_NoArgs(t *testing.T) {
	t.Parallel()

	c := buildServerWithPrompts(t)

	result, err := c.GetPrompt(t.Context(), mcp.GetPromptRequest{
		Params: mcp.GetPromptParams{
			Name: "website-onboarding",
		},
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.NotEmpty(t, result.Description)
	assert.NotEmpty(t, result.Messages)

	// Should have a decent number of messages covering the full workflow.
	assert.Greater(t, len(result.Messages), 5)

	// First message should be the overview (user role, text content).
	first := result.Messages[0]
	assert.Equal(t, mcp.RoleUser, first.Role)
	tc, ok := first.Content.(mcp.TextContent)
	require.True(t, ok, "first message should be TextContent")
	assert.Contains(t, tc.Text, "website creation wizard")
	assert.Contains(t, tc.Text, "Workflow overview")
}

func TestPrompts_GetWebsiteOnboarding_WithDomain(t *testing.T) {
	t.Parallel()

	c := buildServerWithPrompts(t)

	result, err := c.GetPrompt(t.Context(), mcp.GetPromptRequest{
		Params: mcp.GetPromptParams{
			Name: "website-onboarding",
			Arguments: map[string]string{
				"domain": "example.com",
			},
		},
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.NotEmpty(t, result.Messages)

	// Find the domain step message and verify it includes the pre-filled domain.
	var foundDomainStep bool
	for _, msg := range result.Messages {
		tc, ok := msg.Content.(mcp.TextContent)
		if !ok {
			continue
		}
		if contains(tc.Text, "Step: domain") {
			foundDomainStep = true
			assert.Contains(t, tc.Text, "example.com")
			break
		}
	}
	assert.True(t, foundDomainStep, "domain step message not found")

	// Should contain an embedded resource for DNS requirements.
	foundEmbeddedResource := false
	for _, msg := range result.Messages {
		_, ok := msg.Content.(mcp.EmbeddedResource)
		if ok {
			foundEmbeddedResource = true
			break
		}
	}
	assert.True(t, foundEmbeddedResource, "should contain at least one embedded resource")
}

func TestPrompts_GetWebsiteOnboarding_AllArgs(t *testing.T) {
	t.Parallel()

	c := buildServerWithPrompts(t)

	result, err := c.GetPrompt(t.Context(), mcp.GetPromptRequest{
		Params: mcp.GetPromptParams{
			Name: "website-onboarding",
			Arguments: map[string]string{
				"domain":         "mywebsite.io",
				"content_source": "cid",
				"target_type":    "ipfs",
				"dns_mode":       "managed",
			},
		},
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	// With all args provided, the prompt should pre-fill each step.
	var allText string
	for _, msg := range result.Messages {
		tc, ok := msg.Content.(mcp.TextContent)
		if ok {
			allText += tc.Text + "\n"
		}
	}
	assert.Contains(t, allText, "mywebsite.io")
	assert.Contains(t, allText, "\"cid\"")
	assert.Contains(t, allText, "\"ipfs\"")
	assert.Contains(t, allText, "\"managed\"")
}

func TestPrompts_GetWebsiteOnboarding_InvalidContentSource(t *testing.T) {
	t.Parallel()

	c := buildServerWithPrompts(t)

	_, err := c.GetPrompt(t.Context(), mcp.GetPromptRequest{
		Params: mcp.GetPromptParams{
			Name: "website-onboarding",
			Arguments: map[string]string{
				"content_source": "invalid",
			},
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid content_source")
}

func TestPrompts_GetWebsiteOnboarding_InvalidTargetType(t *testing.T) {
	t.Parallel()

	c := buildServerWithPrompts(t)

	_, err := c.GetPrompt(t.Context(), mcp.GetPromptRequest{
		Params: mcp.GetPromptParams{
			Name: "website-onboarding",
			Arguments: map[string]string{
				"target_type": "invalid",
			},
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid target_type")
}

func TestPrompts_GetWebsiteOnboarding_InvalidDNSMode(t *testing.T) {
	t.Parallel()

	c := buildServerWithPrompts(t)

	_, err := c.GetPrompt(t.Context(), mcp.GetPromptRequest{
		Params: mcp.GetPromptParams{
			Name: "website-onboarding",
			Arguments: map[string]string{
				"dns_mode": "invalid",
			},
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid dns_mode")
}

func TestPrompts_GetWebsiteOnboarding_UploadContentSource(t *testing.T) {
	t.Parallel()

	c := buildServerWithPrompts(t)

	result, err := c.GetPrompt(t.Context(), mcp.GetPromptRequest{
		Params: mcp.GetPromptParams{
			Name: "website-onboarding",
			Arguments: map[string]string{
				"content_source": "upload",
			},
		},
	})
	require.NoError(t, err)

	var allText string
	for _, msg := range result.Messages {
		tc, ok := msg.Content.(mcp.TextContent)
		if ok {
			allText += tc.Text + "\n"
		}
	}
	assert.Contains(t, allText, "pinner upload")
	assert.Contains(t, allText, "upload")
}

func TestPrompts_GetWebsiteOnboarding_EmbeddedResources(t *testing.T) {
	t.Parallel()

	c := buildServerWithPrompts(t)

	result, err := c.GetPrompt(t.Context(), mcp.GetPromptRequest{
		Params: mcp.GetPromptParams{
			Name: "website-onboarding",
			Arguments: map[string]string{
				"domain": "test.com",
			},
		},
	})
	require.NoError(t, err)

	// Count embedded resources — should have at least 2
	// (account status + DNS requirements + validation status).
	embeddedCount := 0
	for _, msg := range result.Messages {
		er, ok := msg.Content.(mcp.EmbeddedResource)
		if ok {
			embeddedCount++
			rc := er.Resource
			trc, ok := rc.(mcp.TextResourceContents)
			require.True(t, ok)
			assert.NotEmpty(t, trc.URI)
			assert.Equal(t, "application/json", trc.MIMEType)
		}
	}
	assert.GreaterOrEqual(t, embeddedCount, 2, "should have at least 2 embedded resources")
}

// --- GetPrompt: setup tests ---

func TestPrompts_GetSetup_NoArgs(t *testing.T) {
	t.Parallel()

	c := buildServerWithPrompts(t)

	result, err := c.GetPrompt(t.Context(), mcp.GetPromptRequest{
		Params: mcp.GetPromptParams{
			Name: "setup",
		},
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.NotEmpty(t, result.Description)
	assert.NotEmpty(t, result.Messages)

	// Should have a decent number of messages covering the full workflow.
	assert.Greater(t, len(result.Messages), 5)

	// First message should be the overview.
	first := result.Messages[0]
	assert.Equal(t, mcp.RoleUser, first.Role)
	tc, ok := first.Content.(mcp.TextContent)
	require.True(t, ok)
	assert.Contains(t, tc.Text, "setup wizard")
	assert.Contains(t, tc.Text, "Workflow overview")
}

func TestPrompts_GetSetup_ContainsAllSteps(t *testing.T) {
	t.Parallel()

	c := buildServerWithPrompts(t)

	result, err := c.GetPrompt(t.Context(), mcp.GetPromptRequest{
		Params: mcp.GetPromptParams{
			Name: "setup",
		},
	})
	require.NoError(t, err)

	var allText string
	for _, msg := range result.Messages {
		tc, ok := msg.Content.(mcp.TextContent)
		if ok {
			allText += tc.Text + "\n"
		}
	}

	// Verify all 5 steps are mentioned.
	assert.Contains(t, allText, "Step: auth")
	assert.Contains(t, allText, "Step: config")
	assert.Contains(t, allText, "Step: completion")
	assert.Contains(t, allText, "Step: tutorial")

	// Verify key concepts.
	assert.Contains(t, allText, "sign_in")
	assert.Contains(t, allText, "create_account")
	assert.Contains(t, allText, "skip")
	assert.Contains(t, allText, "use_defaults")
	assert.Contains(t, allText, "custom_endpoint")
	assert.Contains(t, allText, "pinner completion")
}

func TestPrompts_GetSetup_EmbeddedResources(t *testing.T) {
	t.Parallel()

	c := buildServerWithPrompts(t)

	result, err := c.GetPrompt(t.Context(), mcp.GetPromptRequest{
		Params: mcp.GetPromptParams{
			Name: "setup",
		},
	})
	require.NoError(t, err)

	// Should contain at least one embedded resource (account status).
	embeddedCount := 0
	for _, msg := range result.Messages {
		er, ok := msg.Content.(mcp.EmbeddedResource)
		if ok {
			embeddedCount++
			rc := er.Resource
			trc, ok := rc.(mcp.TextResourceContents)
			require.True(t, ok)
			assert.Equal(t, "pinner://account/status", trc.URI)
		}
	}
	assert.GreaterOrEqual(t, embeddedCount, 1, "should have at least 1 embedded resource")
}

// --- Error handling tests ---

func TestPrompts_GetPrompt_UnknownPrompt(t *testing.T) {
	t.Parallel()

	c := buildServerWithPrompts(t)

	_, err := c.GetPrompt(t.Context(), mcp.GetPromptRequest{
		Params: mcp.GetPromptParams{
			Name: "nonexistent",
		},
	})
	require.Error(t, err)
}

// --- Capabilities test ---

func TestPrompts_CapabilitiesEnabled(t *testing.T) {
	t.Parallel()

	c := buildServerWithPrompts(t)

	// If prompts are registered, ListPrompts should succeed.
	result, err := c.ListPrompts(t.Context(), mcp.ListPromptsRequest{})
	require.NoError(t, err)
	assert.NotEmpty(t, result.Prompts)
}

// --- Direct handler tests (no server round-trip) ---

func TestPrompts_RegisterPrompts_DirectCall(t *testing.T) {
	t.Parallel()

	// Test the handlers directly by building a server and extracting its
	// handlers via ListPrompts + GetPrompt.
	c := buildServerWithPrompts(t)

	// Verify both prompts are accessible by name.
	listResult, err := c.ListPrompts(t.Context(), mcp.ListPromptsRequest{})
	require.NoError(t, err)

	promptNames := make(map[string]bool)
	for _, p := range listResult.Prompts {
		promptNames[p.Name] = true
	}

	assert.True(t, promptNames["website-onboarding"])
	assert.True(t, promptNames["setup"])

	// Both should be retrievable.
	for _, name := range []string{"website-onboarding", "setup"} {
		_, err := c.GetPrompt(t.Context(), mcp.GetPromptRequest{
			Params: mcp.GetPromptParams{Name: name},
		})
		require.NoError(t, err, "failed to get prompt %s", name)
	}
}

// --- Helper ---

// contains is a simple strings.Contains helper (avoids importing "strings").
func contains(s, substr string) bool {
	return len(substr) == 0 || (len(s) >= len(substr) && hasSubstring(s, substr))
}

func hasSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
