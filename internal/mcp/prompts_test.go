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

	// Embedded resources at expected positions: account/status, then the
	// platform-domains resource (so the agent sees free-subdomain options
	// before the domain step), then validation-status.
	assert.Equal(t, AccountStatusURI, embedded[0], "account/status is the first embedded resource")
	require.Len(t, embedded, 3, "empty-domain case embeds account/status + platform-domains + validation-status")
	assert.Contains(t, embedded[1], "platform-domains")
	assert.Contains(t, embedded[2], "validation-status")
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

func TestWebsiteUpdateHandlerRendersSteps(t *testing.T) {
	res, err := websiteUpdateHandler(context.Background(), model.PromptRequest{
		Arguments: map[string]string{
			ArgWebsite:     "example.com",
			ArgCID:         "bafybeigdyrzt5sfp7udm7hu76uh7y26nf3efuylqabf3oclgtqy55fbzdi",
			ArgCurrentType: "ipfs",
		},
	})
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

	for _, step := range []string{"resolve_state", "ensure_pinned", "update", "dns_check"} {
		assert.Contains(t, joined, "Step: "+step, "expected step %q", step)
	}
	// Target-type preservation and pin-first are core to the update protocol.
	assert.Contains(t, joined, "preserve")
	assert.Contains(t, joined, "pins_add")
	assert.Contains(t, joined, "CID_NOT_PINNED")
	assert.Contains(t, joined, "reconciliation lag")
	assert.Contains(t, joined, "dns_hosting_enabled")

	// Embedded validation-status resource is present once.
	require.Equal(t, 1, len(embedded))
	assert.Contains(t, embedded[0], "validation-status")
}

func TestWebsiteUpdateHandlerRequiresFields(t *testing.T) {
	_, err := websiteUpdateHandler(context.Background(), model.PromptRequest{Arguments: map[string]string{}})
	require.Error(t, err)
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

// TestOOBSetupPromptDoesNotRequestCredentials verifies the setup auth prompt no
// longer instructs the agent to collect a password/OTP: sign_in requests only an
// email and the handler relays the out-of-band URL for browser completion, so
// secrets never transit the MCP/LLM channel.
func TestOOBSetupPromptDoesNotRequestCredentials(t *testing.T) {
	// The overview rules now live in the embedded setup.tmpl template;
	// render it the same way setupHandler does.
	out := renderPromptTemplate("setup_overview", sitePromptData{})
	require.Contains(t, out, "sign_in")
	require.Contains(t, out, "out-of-band login URL")
	require.Contains(t, out, "only the email")
	// The prompt must no longer instruct collection of a password as a sign_in
	// input field or ask for one from the user.
	assert.NotContains(t, out, "ask for email, password")

	// The setup_step_auth template carries the actual per-step collection
	// instruction (what fields the agent asks for when sign_in is chosen).
	// Guard it directly so a silent deletion/reintroduction of agent-side
	// password/OTP collection fails this test rather than leaking a secret
	// into the MCP/LLM channel.
	authStep := renderPromptTemplate("setup_step_auth", sitePromptData{})
	require.Contains(t, authStep, "out-of-band login URL", "sign_in must relay the browser URL")
	// The credential-collection prohibition must remain present.
	require.Contains(t, authStep, "NEVER ask for a password", "password collection must stay forbidden")
	require.Contains(t, authStep, "never sent to you", "secrets must stay out of the MCP/LLM channel")
	// The auth step's input schema must never carry a password/OTP field
	// (no JSON key asking the agent to supply one).
	assert.NotContains(t, authStep, `"password"`, "sign_in schema must not accept an agent-supplied password")
	assert.NotContains(t, authStep, `"otp_code"`, "sign_in schema must not accept an agent-supplied OTP")
}
