package mcp

import (
	"go.lumeweb.com/mcpplane/model"
	"go.lumeweb.com/pinner/mcp"
	"go.lumeweb.com/pinner/assembly"
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

// PromptDescriptorsForSurface returns the prompt descriptors enabled for the
// given surface, delegated to the module's mcp.PromptDescriptorsForSurface.
// Each prompt maps to a domain flag: website onboarding/update need the
// websites surface, setup needs the account surface, and ENS publish needs the
// ENS surface. A restricted surface (e.g. hosted) omits the prompts whose
// underlying tools are not registered. The conversion is lossless: Surface and
// assembly.Surface share the same underlying construction-time shape.
func PromptDescriptorsForSurface(surface Surface) []model.PromptDescriptor {
	return mcp.PromptDescriptorsForSurface(assembly.Surface(surface))
}
