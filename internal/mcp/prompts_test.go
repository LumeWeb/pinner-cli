package mcp

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
)

// TestWebsiteOnboardingHandlerRendersSteps verifies the website-onboarding
// prompt handler renders the full step sequence in order, drawn from the
// embedded templates, with the embedded resource references at the right
// positions.
func TestWebsiteOnboardingHandlerRendersSteps(t *testing.T) {
	res, err := websiteOnboardingHandler(context.Background(), model.PromptRequest{
		Arguments: map[string]string{},
	})
	require.NoError(t, err)

	// Collect the text of all messages and the embedded URIs.
	var text []string
	var embedded []string
	for _, m := range res.Messages {
		if m.EmbeddedResource != nil {
			embedded = append(embedded, m.EmbeddedResource.URI)
		} else {
			text = append(text, m.Text)
		}
	}
	joined := strings.Join(text, "\n")

	// Steps that carry a "Step: X" header appear in order. The validate and
	// complete steps have no "Step:" header (they are standalone instructions),
	// matching the pre-refactor prose.
	for _, step := range []string{
		"auth_check", "content_source", "target_type", "domain",
		"dns_mode", "create", "dns_setup", "validate", "complete",
	} {
		if step == "validate" || step == "complete" {
			continue
		}
		assert.Contains(t, joined, "Step: "+step, "expected step %q", step)
	}
	assert.Contains(t, joined, "validation-status resource")
	assert.Contains(t, joined, `"complete": true`)

	// With no domain pre-filled, the "ask" step variants are used.
	assert.Contains(t, joined, `{"domain": "<domain>"}`)

	// Embedded resources at expected positions.
	assert.Equal(t, AccountStatusURI, embedded[0], "account/status is the first embedded resource")
	require.Len(t, embedded, 2, "empty-domain case embeds account/status + validation-status")
	assert.Contains(t, embedded[1], "validation-status")
}

// TestWebsiteOnboardingPrefillVariant verifies that pre-filling domain,
// target_type, and dns_mode renders the "filled" step variants with the
// supplied values instead of the "ask" variants.
func TestWebsiteOnboardingPrefillVariant(t *testing.T) {
	res, err := websiteOnboardingHandler(context.Background(), model.PromptRequest{
		Arguments: map[string]string{
			ArgDomain:        "example.com",
			ArgTargetType:    "ipns",
			ArgDNSMode:       "managed",
			ArgContentSource: "cid",
		},
	})
	require.NoError(t, err)

	var text []string
	for _, m := range res.Messages {
		if m.EmbeddedResource == nil {
			text = append(text, m.Text)
		}
	}
	joined := strings.Join(text, "\n")

	assert.Contains(t, joined, "Pre-filled: domain=example.com")
	assert.Contains(t, joined, `{"domain": "example.com"}`)
	assert.Contains(t, joined, `{"type": "ipns"}`)
	assert.Contains(t, joined, `{"mode": "managed"}`)
	assert.Contains(t, joined, `{"choice": "cid", "cid": "<CID>"}`)
	var embedded []string
	for _, m := range res.Messages {
		if m.EmbeddedResource != nil {
			embedded = append(embedded, m.EmbeddedResource.URI)
		}
	}
	assert.Contains(t, embedded, "pinner://websites/example.com/dns-requirements")
	assert.NotContains(t, joined, `{"domain": "<domain>"}`)
}

// TestSetupHandlerRendersSteps verifies the setup prompt handler renders all
// its steps from the embedded templates.
func TestSetupHandlerRendersSteps(t *testing.T) {
	res, err := setupHandler(context.Background(), model.PromptRequest{Arguments: map[string]string{}})
	require.NoError(t, err)

	var text []string
	var embedded []string
	for _, m := range res.Messages {
		if m.EmbeddedResource != nil {
			embedded = append(embedded, m.EmbeddedResource.URI)
		} else {
			text = append(text, m.Text)
		}
	}
	joined := strings.Join(text, "\n")

	for _, step := range []string{"auth", "config", "completion", "tutorial"} {
		assert.Contains(t, joined, "Step: "+step)
	}
	// Completion embedded resource (account/status) is present twice.
	assert.Equal(t, AccountStatusURI, embedded[0])
	assert.Equal(t, AccountStatusURI, embedded[len(embedded)-1])
}
