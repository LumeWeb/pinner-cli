package mcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
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

// RegisterPrompts registers the website-onboarding and setup MCP prompt
// templates on the given server. Each prompt returns a sequence of messages
// that instruct the agent to drive the corresponding wizard workflow
// deterministically, using the wizard tools and pinner:// resources.
//
// Prompt capabilities (prompts/list, prompts/get) are enabled when this
// function is called via RegisterPrompts on a server that also has
// WithPromptCapabilities(true).
func RegisterPrompts(srv *server.MCPServer) {
	srv.AddPrompt(websiteOnboardingPrompt(), websiteOnboardingHandler)
	srv.AddPrompt(setupPrompt(), setupHandler)
}

// --- website-onboarding prompt ---

// websiteOnboardingPrompt returns the Prompt definition for the website
// onboarding workflow. It accepts optional arguments that pre-fill the
// wizard choices, letting the agent skip interactive gathering when the
// caller already knows the values.
func websiteOnboardingPrompt() mcp.Prompt {
	return mcp.NewPrompt(PromptWebsiteOnboarding,
		mcp.WithPromptTitle("Website Onboarding Wizard"),
		mcp.WithPromptDescription(
			"Guides the agent through the website creation wizard workflow "+
				"step by step using the websites_wizard_start and websites_wizard_step "+
				"tools. Embeds references to pinner:// resources for DNS requirements "+
				"and validation status. Optional arguments pre-fill wizard choices."),
		mcp.WithArgument(ArgDomain,
			mcp.ArgumentDescription("Domain name for the website (e.g. example.com). If omitted, the wizard will ask."),
		),
		mcp.WithArgument(ArgContentSource,
			mcp.ArgumentDescription("Content source: \"cid\" (already have a CID) or \"upload\" (need to upload first). Default: cid."),
		),
		mcp.WithArgument(ArgTargetType,
			mcp.ArgumentDescription("Content addressing type: \"ipfs\" or \"ipns\". Default: ipfs."),
		),
		mcp.WithArgument(ArgDNSMode,
			mcp.ArgumentDescription("DNS mode: \"managed\" (Pinner handles DNS) or \"self_managed\". Default: managed."),
		),
	)
}

// websiteOnboardingHandler is the prompts/get handler for the
// website-onboarding prompt. It builds a sequence of messages that
// instruct the agent to execute the websites wizard workflow.
func websiteOnboardingHandler(ctx context.Context, req mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	args := req.Params.Arguments
	domain := args[ArgDomain]
	contentSource := args[ArgContentSource]
	targetType := args[ArgTargetType]
	dnsMode := args[ArgDNSMode]

	// Validate optional arguments if provided.
	if contentSource != "" && contentSource != "cid" && contentSource != "upload" {
		return nil, fmt.Errorf("invalid content_source %q: expected \"cid\" or \"upload\"", contentSource)
	}
	if targetType != "" && targetType != "ipfs" && targetType != "ipns" {
		return nil, fmt.Errorf("invalid target_type %q: expected \"ipfs\" or \"ipns\"", targetType)
	}
	if dnsMode != "" && dnsMode != "managed" && dnsMode != "self_managed" {
		return nil, fmt.Errorf("invalid dns_mode %q: expected \"managed\" or \"self_managed\"", dnsMode)
	}

	var messages []mcp.PromptMessage

	// Step 0: Overview and prerequisites.
	messages = append(messages, mcp.NewPromptMessage(mcp.RoleUser,
		mcp.NewTextContent(websiteOnboardingOverview(domain)),
	))

	// Step 1: Read account status resource to verify authentication.
	messages = append(messages, mcp.NewPromptMessage(mcp.RoleUser,
		mcp.NewEmbeddedResource(mcp.TextResourceContents{
			URI:      AccountStatusURI,
			MIMEType: "application/json",
			Text:     "Read this resource at runtime to get live data.",
		}),
	))
	messages = append(messages, mcp.NewPromptMessage(mcp.RoleAssistant,
		mcp.NewTextContent(
			"Read the pinner://account/status resource above. If \"authenticated\" is false, "+
				"instruct the user to run `pinner auth` or provide an auth token via --auth-token. "+
				"Do not proceed with the wizard until authentication is confirmed.",
		),
	))

	// Step 2: Start the wizard.
	messages = append(messages, mcp.NewPromptMessage(mcp.RoleUser,
		mcp.NewTextContent(
			"Call the `websites_wizard_start` tool (no arguments). It returns a session_id "+
				"and the first step (auth_check). Store the session_id — you will pass it to "+
				"every subsequent `websites_wizard_step` call.",
		),
	))

	// Step 3: Auth check step.
	messages = append(messages, mcp.NewPromptMessage(mcp.RoleUser,
		mcp.NewTextContent(
			"Step: auth_check\n"+
				"Call `websites_wizard_step` with the session_id and an empty input object {}. "+
				"This step auto-validates the configured auth token. If it returns an error, "+
				"stop and tell the user authentication is required.",
		),
	))

	// Step 4: Content source step.
	instructions := websiteOnboardingContentStep(contentSource)
	messages = append(messages, mcp.NewPromptMessage(mcp.RoleUser,
		mcp.NewTextContent(instructions),
	))

	// Step 5: Target type step.
	if targetType != "" {
		messages = append(messages, mcp.NewPromptMessage(mcp.RoleUser,
			mcp.NewTextContent(fmt.Sprintf(
				"Step: target_type\n"+
					"Call `websites_wizard_step` with input: {\"type\": %q}\n"+
					"Use \"ipfs\" for immutable content-addressed data or \"ipns\" for mutable names.",
				targetType,
			)),
		))
	} else {
		messages = append(messages, mcp.NewPromptMessage(mcp.RoleUser,
			mcp.NewTextContent(
				"Step: target_type\n"+
					"Ask the user whether they want IPFS (immutable, content-addressed) or "+
					"IPNS (mutable name). Call `websites_wizard_step` with input: {\"type\": \"<ipfs|ipns>\"}",
			),
		))
	}

	// Step 6: Domain step.
	if domain != "" {
		messages = append(messages, mcp.NewPromptMessage(mcp.RoleUser,
			mcp.NewTextContent(fmt.Sprintf(
				"Step: domain\n"+
					"Call `websites_wizard_step` with input: {\"domain\": %q}",
				domain,
			)),
		))
	} else {
		messages = append(messages, mcp.NewPromptMessage(mcp.RoleUser,
			mcp.NewTextContent(
				"Step: domain\n"+
					"Ask the user for the domain name for their website (e.g. example.com). "+
					"Call `websites_wizard_step` with input: {\"domain\": \"<domain>\"}",
			),
		))
	}

	// Step 7: DNS mode step.
	if dnsMode != "" {
		messages = append(messages, mcp.NewPromptMessage(mcp.RoleUser,
			mcp.NewTextContent(fmt.Sprintf(
				"Step: dns_mode\n"+
					"Call `websites_wizard_step` with input: {\"mode\": %q}\n"+
					"Use \"managed\" if Pinner should handle DNS, or \"self_managed\" if the user "+
					"will configure DNS records themselves.",
				dnsMode,
			)),
		))
	} else {
		messages = append(messages, mcp.NewPromptMessage(mcp.RoleUser,
			mcp.NewTextContent(
				"Step: dns_mode\n"+
					"Ask the user whether they want managed DNS (Pinner handles it) or "+
					"self-managed DNS (they configure records). Call `websites_wizard_step` "+
					"with input: {\"mode\": \"<managed|self_managed>\"}",
			),
		))
	}

	// Step 8: Create step.
	messages = append(messages, mcp.NewPromptMessage(mcp.RoleUser,
		mcp.NewTextContent(
			"Step: create\n"+
				"This is an irreversible operation. Confirm with the user before proceeding. "+
				"Call `websites_wizard_step` with input: {\"confirm\": true} to create the website. "+
				"If the user is not ready, call with {\"confirm\": false} to see the error and retry.",
		),
	))

	// Step 9: DNS setup step — embed the resource reference.
	if domain != "" {
		messages = append(messages, mcp.NewPromptMessage(mcp.RoleUser,
			mcp.NewEmbeddedResource(mcp.TextResourceContents{
				URI:      fmt.Sprintf("pinner://websites/%s/dns-requirements", domain),
				MIMEType: "application/json",
				Text:     "Read this resource at runtime to get live data.",
			}),
		))
		messages = append(messages, mcp.NewPromptMessage(mcp.RoleAssistant,
			mcp.NewTextContent(
				"Read the pinner://websites/{domain}/dns-requirements resource above. "+
					"Present the DNS records to the user. If DNS is managed, instruct them to "+
					"update their registrar's nameservers. If self-managed, instruct them to add "+
					"the TXT, dnslink, and optional CNAME records. "+
					"Call `websites_wizard_step` with input: {} to proceed.",
			),
		))
	} else {
		messages = append(messages, mcp.NewPromptMessage(mcp.RoleUser,
			mcp.NewTextContent(
				"Step: dns_setup\n"+
					"Read the pinner://websites/{domain}/dns-requirements resource (substitute the "+
					"domain from the previous step) to get the exact DNS records. Present them to "+
					"the user. Call `websites_wizard_step` with input: {} to proceed.",
			),
		))
	}

	// Step 10: Validate step.
	validateURI := ValidationStatusTmpl
	if domain != "" {
		// We don't have the website ID at prompt time, but the agent will
		// resolve it from the wizard's create response.
		validateURI = "pinner://websites/{id}/validation-status"
	}
	messages = append(messages, mcp.NewPromptMessage(mcp.RoleUser,
		mcp.NewEmbeddedResource(mcp.TextResourceContents{
			URI:      validateURI,
			MIMEType: "application/json",
			Text:     "Read this resource at runtime to get live data.",
		}),
	))
	messages = append(messages, mcp.NewPromptMessage(mcp.RoleAssistant,
		mcp.NewTextContent(
			"Read the pinner://websites/{id}/validation-status resource (use the website ID "+
				"from the create step's response). If validation fails, instruct the user to wait "+
				"for DNS propagation (may take minutes to hours) and retry. "+
				"Call `websites_wizard_step` with input: {\"retry\": true} to re-run validation. "+
				"Call with input: {} to accept the current status and finish.",
		),
	))

	// Step 11: Completion.
	messages = append(messages, mcp.NewPromptMessage(mcp.RoleUser,
		mcp.NewTextContent(
			"When the wizard returns \"complete\": true, the website onboarding is done. "+
				"Summarize the result for the user: domain, CID, target type, DNS mode, and "+
				"validation status. If validation failed, provide the reason and suggest next steps.",
		),
	))

	return &mcp.GetPromptResult{
		Description: "Website onboarding wizard workflow with embedded resource references",
		Messages:    messages,
	}, nil
}

// --- setup prompt ---

// setupPrompt returns the Prompt definition for the initial setup wizard.
func setupPrompt() mcp.Prompt {
	return mcp.NewPrompt(PromptSetup,
		mcp.WithPromptTitle("Setup Wizard"),
		mcp.WithPromptDescription(
			"Guides the agent through the initial pinner setup wizard workflow "+
				"step by step using the setup_wizard_start and setup_wizard_step tools. "+
				"Covers authentication, configuration, shell completion, and a quick tutorial. "+
				"Embeds a reference to the pinner://account/status resource."),
	)
}

// setupHandler is the prompts/get handler for the setup prompt.
func setupHandler(ctx context.Context, req mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	var messages []mcp.PromptMessage

	// Step 0: Overview.
	messages = append(messages, mcp.NewPromptMessage(mcp.RoleUser,
		mcp.NewTextContent(setupOverview()),
	))

	// Step 1: Read account status to check current auth state.
	messages = append(messages, mcp.NewPromptMessage(mcp.RoleUser,
		mcp.NewEmbeddedResource(mcp.TextResourceContents{
			URI:      AccountStatusURI,
			MIMEType: "application/json",
			Text:     "Read this resource at runtime to get live data.",
		}),
	))
	messages = append(messages, mcp.NewPromptMessage(mcp.RoleAssistant,
		mcp.NewTextContent(
			"Read the pinner://account/status resource above. If \"authenticated\" is true "+
				"and \"token_valid\" is true, the user may skip the auth step. Otherwise, "+
				"they will need to sign in or create an account.",
		),
	))

	// Step 2: Start the setup wizard.
	messages = append(messages, mcp.NewPromptMessage(mcp.RoleUser,
		mcp.NewTextContent(
			"Call the `setup_wizard_start` tool (no arguments). It returns a session_id "+
				"and the first step (auth). Store the session_id for subsequent calls.",
		),
	))

	// Step 3: Auth step.
	messages = append(messages, mcp.NewPromptMessage(mcp.RoleUser,
		mcp.NewTextContent(
			"Step: auth\n"+
				"Present the user with three options:\n"+
				"  1. \"create_account\" — direct them to https://pinner.xyz/register, then sign in\n"+
				"  2. \"sign_in\" — ask for email, password, and (if needed) OTP code\n"+
				"  3. \"skip\" — skip authentication for now\n\n"+
				"Call `setup_wizard_step` with the appropriate input:\n"+
				"  - {\"choice\": \"create_account\"} (will error with instructions)\n"+
				"  - {\"choice\": \"sign_in\", \"email\": \"...\", \"password\": \"...\", \"otp_code\": \"...\"}\n"+
				"  - {\"choice\": \"skip\"}\n"+
				"For sign_in, if the response says OTP is required, ask for the otp_code and retry.",
		),
	))

	// Step 4: Config step.
	messages = append(messages, mcp.NewPromptMessage(mcp.RoleUser,
		mcp.NewTextContent(
			"Step: config\n"+
				"Present the user with three options:\n"+
				"  1. \"use_defaults\" — use the default endpoint and HTTPS (recommended)\n"+
				"  2. \"custom_endpoint\" — provide a custom API endpoint and secure flag\n"+
				"  3. \"skip\" — skip configuration\n\n"+
				"Call `setup_wizard_step` with the appropriate input:\n"+
				"  - {\"choice\": \"use_defaults\"}\n"+
				"  - {\"choice\": \"custom_endpoint\", \"endpoint\": \"https://...\", \"secure\": true}\n"+
				"  - {\"choice\": \"skip\"}",
		),
	))

	// Step 5: Shell completion step.
	messages = append(messages, mcp.NewPromptMessage(mcp.RoleUser,
		mcp.NewTextContent(
			"Step: completion\n"+
				"Ask the user which shell they use (bash, zsh, fish, or pwsh). "+
				"This is informational — instruct the user to run "+
				"`pinner completion <shell>` to install completions. "+
				"Call `setup_wizard_step` with input: {\"shell\": \"<shell>\"} or {} to skip.",
		),
	))

	// Step 6: Tutorial step.
	messages = append(messages, mcp.NewPromptMessage(mcp.RoleUser,
		mcp.NewTextContent(
			"Step: tutorial\n"+
				"Provide a brief tutorial of common commands:\n"+
				"  - pinner auth          — check auth status\n"+
				"  - pinner upload <file> — upload content to IPFS\n"+
				"  - pinner pins add <cid> — pin existing content\n"+
				"  - pinner websites       — manage websites\n"+
				"  - pinner dns            — manage DNS zones\n"+
				"  - pinner doctor         — run diagnostics\n"+
				"Call `setup_wizard_step` with input: {} to complete the wizard.",
		),
	))

	// Step 7: Completion.
	messages = append(messages, mcp.NewPromptMessage(mcp.RoleUser,
		mcp.NewEmbeddedResource(mcp.TextResourceContents{
			URI:      AccountStatusURI,
			MIMEType: "application/json",
			Text:     "Read this resource at runtime to get live data.",
		}),
	))
	messages = append(messages, mcp.NewPromptMessage(mcp.RoleAssistant,
		mcp.NewTextContent(
			"When the wizard returns \"complete\": true, setup is done. "+
				"Read the pinner://account/status resource above to confirm the final state. "+
				"Summarize for the user: auth status, configured endpoint, and any next steps.",
		),
	))

	return &mcp.GetPromptResult{
		Description: "Setup wizard workflow with embedded resource references",
		Messages:    messages,
	}, nil
}

// --- Prompt message helpers ---

// websiteOnboardingOverview returns the introductory instruction for the
// website onboarding prompt.
func websiteOnboardingOverview(domain string) string {
	var b strings.Builder
	b.WriteString("You are guiding a user through the website creation wizard.\n\n")
	b.WriteString("Workflow overview (9 steps):\n")
	b.WriteString("  1. auth_check     — verifies the user is authenticated\n")
	b.WriteString("  2. content_source — choose CID or upload\n")
	b.WriteString("  3. target_type    — IPFS or IPNS\n")
	b.WriteString("  4. domain         — domain name for the website\n")
	b.WriteString("  5. dns_mode       — managed or self-managed DNS\n")
	b.WriteString("  6. create         — create the website (irreversible, confirm first)\n")
	b.WriteString("  7. dns_setup      — read DNS requirements resource\n")
	b.WriteString("  8. validate       — run live validation\n")
	b.WriteString("  9. complete       — summarize results\n\n")
	b.WriteString("Rules:\n")
	b.WriteString("  - Always pass the session_id from websites_wizard_start to websites_wizard_step.\n")
	b.WriteString("  - The next_step_schema in each response tells you the exact input shape.\n")
	b.WriteString("  - If a step returns an error, the session stays in the same state — you can retry.\n")
	b.WriteString("  - Read pinner:// resources for live data (DNS requirements, validation status).\n")
	if domain != "" {
		b.WriteString(fmt.Sprintf("\nPre-filled: domain=%s\n", domain))
	}
	return b.String()
}

// websiteOnboardingContentStep builds the content_source step instructions,
// incorporating the pre-filled content source if provided.
func websiteOnboardingContentStep(contentSource string) string {
	if contentSource == "upload" {
		return "Step: content_source\n" +
			"Content source is \"upload\". The wizard requires an existing CID — " +
			"instruct the user to run `pinner upload <file>` first, then restart " +
			"the wizard with content_source set to \"cid\". Call `websites_wizard_step` " +
			"with input: {\"choice\": \"upload\"} (this will return an error with instructions)."
	}
	if contentSource == "cid" {
		return "Step: content_source\n" +
			"Content source is \"cid\". Ask the user for their IPFS CID, then call " +
			"`websites_wizard_step` with input: {\"choice\": \"cid\", \"cid\": \"<CID>\"}"
	}
	return "Step: content_source\n" +
		"Ask the user whether they have an IPFS CID ready (\"cid\") or need to " +
		"upload content first (\"upload\"). If they choose \"upload\", they must run " +
		"`pinner upload <file>` first and restart. Call `websites_wizard_step` " +
		"with input: {\"choice\": \"<cid|upload>\", \"cid\": \"<CID if cid>\"}"
}

// setupOverview returns the introductory instruction for the setup prompt.
func setupOverview() string {
	var b strings.Builder
	b.WriteString("You are guiding a user through the initial pinner setup wizard.\n\n")
	b.WriteString("Workflow overview (5 steps):\n")
	b.WriteString("  1. auth       — sign in, create account, or skip\n")
	b.WriteString("  2. config     — use defaults, custom endpoint, or skip\n")
	b.WriteString("  3. completion — shell completion setup (informational)\n")
	b.WriteString("  4. tutorial   — quick command tour (informational)\n")
	b.WriteString("  5. complete   — finalize and verify\n\n")
	b.WriteString("Rules:\n")
	b.WriteString("  - Always pass the session_id from setup_wizard_start to setup_wizard_step.\n")
	b.WriteString("  - The next_step_schema in each response tells you the exact input shape.\n")
	b.WriteString("  - If a step returns an error, the session stays in the same state — you can retry.\n")
	b.WriteString("  - For sign_in with OTP, the first call may error asking for otp_code — retry with it.\n")
	b.WriteString("  - Read pinner://account/status to verify the auth state before and after.\n")
	return b.String()
}
