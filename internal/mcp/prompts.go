package mcp

import (
	"context"
	"fmt"
)

// Prompt name constants.
const (
	PromptWebsiteOnboarding = "website-onboarding"
	PromptSetup             = "setup"
)

// Prompt argument names.
const (
	ArgDomain        = "domain"
	ArgContentSource = "content_source"
	ArgTargetType    = "target_type"
	ArgDNSMode       = "dns_mode"
)

// PromptDescriptors builds the SDK-neutral prompt descriptors for the
// website-onboarding and setup workflows. Each prompt returns a sequence of
// messages that instruct the agent to drive the corresponding wizard workflow
// deterministically, using the wizard tools and pinner:// resources. The
// message prose is rendered from embedded text/templates (prompttemplates/).
func PromptDescriptors() []PromptDescriptor {
	return []PromptDescriptor{
		{
			Name:        PromptWebsiteOnboarding,
			Title:       "Website Onboarding Wizard",
			Description: "Guides the agent through the website creation wizard workflow step by step using the websites_wizard_start and websites_wizard_step tools. Embeds references to pinner:// resources for DNS requirements and validation status. Optional arguments pre-fill wizard choices.",
			Arguments: []PromptArgumentDescriptor{
				{Name: ArgDomain, Description: "Domain name for the website (e.g. example.com). If omitted, the wizard will ask."},
				{Name: ArgContentSource, Description: `Content source: "cid" (already have a CID) or "upload" (need to upload first). Default: cid.`},
				{Name: ArgTargetType, Description: `Content addressing type: "ipfs" or "ipns". Default: ipfs.`},
				{Name: ArgDNSMode, Description: `DNS mode: "managed" (Pinner handles DNS) or "self_managed". Default: managed.`},
			},
			Handler: websiteOnboardingHandler,
		},
		{
			Name:        PromptSetup,
			Title:       "Setup Wizard",
			Description: "Guides the agent through the initial pinner setup wizard workflow step by step using the setup_wizard_start and setup_wizard_step tools. Covers authentication, configuration, shell completion, and a quick tutorial. Embeds a reference to the pinner://account/status resource.",
			Handler:     setupHandler,
		},
	}
}

// websiteOnboardingHandler is the prompts/get handler for the
// website-onboarding prompt. It renders a sequence of messages — from the
// embedded website_onboarding.tmpl templates — that instruct the agent to
// execute the websites wizard workflow.
func websiteOnboardingHandler(ctx context.Context, req PromptRequest) (PromptResult, error) {
	args := req.Arguments
	domain := args[ArgDomain]
	contentSource := args[ArgContentSource]
	targetType := args[ArgTargetType]
	dnsMode := args[ArgDNSMode]

	// Validate optional arguments if provided.
	if contentSource != "" && contentSource != "cid" && contentSource != "upload" {
		return PromptResult{}, fmt.Errorf("invalid content_source %q: expected \"cid\" or \"upload\"", contentSource)
	}
	if targetType != "" && targetType != "ipfs" && targetType != "ipns" {
		return PromptResult{}, fmt.Errorf("invalid target_type %q: expected \"ipfs\" or \"ipns\"", targetType)
	}
	if dnsMode != "" && dnsMode != "managed" && dnsMode != "self_managed" {
		return PromptResult{}, fmt.Errorf("invalid dns_mode %q: expected \"managed\" or \"self_managed\"", dnsMode)
	}

	data := sitePromptData{
		Domain:        domain,
		ContentSource: contentSource,
		TargetType:    targetType,
		DNSMode:       dnsMode,
	}

	var messages []PromptMessage

	// Step 0: Overview and prerequisites.
	messages = append(messages, textMsg(renderPromptTemplate("website_overview", data)))

	// Step 1: Read account status resource to verify authentication.
	messages = append(messages, embeddedMsg(AccountStatusURI))
	messages = append(messages, textMsg(renderPromptTemplate("website_auth_status", data)))

	// Step 2: Start the wizard.
	messages = append(messages, textMsg(renderPromptTemplate("website_start", data)))

	// Step 3: Auth check step.
	messages = append(messages, textMsg(renderPromptTemplate("website_step_auth_check", data)))

	// Step 4: Content source step.
	var contentStep string
	switch contentSource {
	case "upload":
		contentStep = "website_step_content_source_upload"
	case "cid":
		contentStep = "website_step_content_source_cid"
	default:
		contentStep = "website_step_content_source_ask"
	}
	messages = append(messages, textMsg(renderPromptTemplate(contentStep, data)))

	// Step 5: Target type step.
	if targetType != "" {
		messages = append(messages, textMsg(renderPromptTemplate("website_step_target_type_filled", data)))
	} else {
		messages = append(messages, textMsg(renderPromptTemplate("website_step_target_type_ask", data)))
	}

	// Step 6: Domain step.
	if domain != "" {
		messages = append(messages, textMsg(renderPromptTemplate("website_step_domain_filled", data)))
	} else {
		messages = append(messages, textMsg(renderPromptTemplate("website_step_domain_ask", data)))
	}

	// Step 7: DNS mode step.
	if dnsMode != "" {
		messages = append(messages, textMsg(renderPromptTemplate("website_step_dns_mode_filled", data)))
	} else {
		messages = append(messages, textMsg(renderPromptTemplate("website_step_dns_mode_ask", data)))
	}

	// Step 8: Create step.
	messages = append(messages, textMsg(renderPromptTemplate("website_step_create", data)))

	// Step 9: DNS setup step: embed the resource reference.
	if domain != "" {
		messages = append(messages, embeddedMsg(fmt.Sprintf("pinner://websites/%s/dns-requirements", domain)))
		messages = append(messages, textMsg(renderPromptTemplate("website_step_dns_setup_filled", data)))
	} else {
		messages = append(messages, textMsg(renderPromptTemplate("website_step_dns_setup_ask", data)))
	}

	// Step 10: Validate step.
	validateURI := ValidationStatusTmpl
	if domain != "" {
		// We don't have the website ID at prompt time, but the agent will
		// resolve it from the wizard's create response.
		validateURI = "pinner://websites/{id}/validation-status"
	}
	messages = append(messages, embeddedMsg(validateURI))
	messages = append(messages, textMsg(renderPromptTemplate("website_step_validate", data)))

	// Step 11: Completion.
	messages = append(messages, textMsg(renderPromptTemplate("website_step_complete", data)))

	return PromptResult{
		Description: "Website onboarding wizard workflow with embedded resource references",
		Messages:    messages,
	}, nil
}

// setupHandler is the prompts/get handler for the setup prompt. It renders the
// message sequence from the embedded setup.tmpl templates.
func setupHandler(ctx context.Context, req PromptRequest) (PromptResult, error) {
	data := sitePromptData{}
	var messages []PromptMessage

	// Step 0: Overview.
	messages = append(messages, textMsg(renderPromptTemplate("setup_overview", data)))

	// Step 1: Read account status to check current auth state.
	messages = append(messages, embeddedMsg(AccountStatusURI))
	messages = append(messages, textMsg(renderPromptTemplate("setup_auth_status_check", data)))

	// Step 2: Start the setup wizard.
	messages = append(messages, textMsg(renderPromptTemplate("setup_start", data)))

	// Step 3: Auth step.
	messages = append(messages, textMsg(renderPromptTemplate("setup_step_auth", data)))

	// Step 4: Config step.
	messages = append(messages, textMsg(renderPromptTemplate("setup_step_config", data)))

	// Step 5: Shell completion step.
	messages = append(messages, textMsg(renderPromptTemplate("setup_step_completion", data)))

	// Step 6: Tutorial step.
	messages = append(messages, textMsg(renderPromptTemplate("setup_step_tutorial", data)))

	// Step 7: Completion.
	messages = append(messages, embeddedMsg(AccountStatusURI))
	messages = append(messages, textMsg(renderPromptTemplate("setup_step_complete", data)))

	return PromptResult{
		Description: "Setup wizard workflow with embedded resource references",
		Messages:    messages,
	}, nil
}

// --- Prompt message helpers ---

// textMsg builds a user-role text message.
func textMsg(text string) PromptMessage {
	return PromptMessage{Role: "user", Text: text}
}

// embeddedMsg builds a user-role embedded-resource message that instructs the
// agent to read the resource at runtime.
func embeddedMsg(uri string) PromptMessage {
	return PromptMessage{
		Role: "user",
		EmbeddedResource: &ResourceResult{
			URI:      uri,
			MIMEType: "application/json",
			Text:     "Read this resource at runtime to get live data.",
		},
	}
}
