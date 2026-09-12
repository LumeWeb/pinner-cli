package mcp

import (
	"go.lumeweb.com/mcpplane/model"
	"go.lumeweb.com/pinner/assembly"
	"go.lumeweb.com/pinner/mcp"
)

// Prompt name constants are re-exported from the module's mcp package,
// which owns the prompt surface (descriptors + embedded templates + handlers).
// Keeping the CLI-side aliases means existing call-sites and tests compose
// unchanged while the module remains the single source of truth.
const (
	PromptWebsiteOnboarding = mcp.PromptWebsiteOnboarding
	PromptWebsiteUpdate     = mcp.PromptWebsiteUpdate
	PromptSetup             = mcp.PromptSetup
	PromptENSPublish        = mcp.PromptENSPublish
)

// Prompt argument names, as above, owned by mcp.
const (
	ArgDomain        = mcp.ArgDomain
	ArgContentSource = mcp.ArgContentSource
	ArgTargetType    = mcp.ArgTargetType
	ArgDNSMode       = mcp.ArgDNSMode

	// website-update prompt arguments.
	ArgWebsite     = mcp.ArgWebsite
	ArgCID         = mcp.ArgCID
	ArgCurrentType = mcp.ArgCurrentType

	// ens-publish prompt arguments.
	ArgENSName = mcp.ArgENSName
)

// PromptDescriptors builds the SDK-neutral prompt descriptors for the full
// surface. It delegates to mcp, which owns the prompt presentation
// surface (descriptors, embedded text/templates, and handlers); the local
// duplicate was removed so the CLI and the module cannot drift.
func PromptDescriptors() []model.PromptDescriptor {
	return mcp.PromptDescriptors()
}

// dropWizardPromptsWithoutWizardTools filters out every wizard-workflow prompt
// when the wizard start tools are absent from the (finalized) catalog. The
// surface-based filter alone cannot catch this: a hosted assembly may expose
// the websites/account domain surfaces while provisioning NO wizard tools.
func dropWizardPromptsWithoutWizardTools(catalog *ToolCatalog, prompts []model.PromptDescriptor) []model.PromptDescriptor {
	if catalog == nil {
		return prompts
	}
	_, websitesWizard := catalog.Get("websites_wizard_start")
	_, setupWizard := catalog.Get("setup_wizard_start")
	out := prompts[:0]
	for _, p := range prompts {
		switch {
		case p.Name == PromptWebsiteOnboarding && !websitesWizard:
			continue
		case p.Name == PromptSetup && !setupWizard:
			continue
		}
		out = append(out, p)
	}
	return out
}

// PromptDescriptorsForScope returns the prompt descriptors enabled for the
// given scope, delegated to the module's mcp.PromptDescriptorsForScope.
// Each prompt maps to a domain flag: website onboarding/update need the
// websites surface, setup needs the account surface, and ENS publish needs the
// ENS surface. A restricted scope (e.g. hosted) omits the prompts whose
// underlying tools are not registered. The conversion is lossless: DomainScope
// and assembly.DomainScope share the same underlying construction-time shape.
func PromptDescriptorsForScope(scope DomainScope, hosted bool) []model.PromptDescriptor {
	return mcp.PromptDescriptorsForScope(assembly.DomainScope(scope), hosted)
}
