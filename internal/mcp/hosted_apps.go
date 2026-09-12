package mcp

import (
	"go.lumeweb.com/pinner-cli/internal/mcp/hostenv"
	"go.lumeweb.com/pinner/mcp/appswire"
)

// hostedAppProfile derives the effective platform profile a hosted assembly
// resolves feature-gated registration against. A hosted assembly has no
// detected per-host profile, so without an explicit one it would fall back to
// the bare HTTP transport profile (hostenv.ProfileForTransport's zero hosted
// flags) — and that fallback carries no MCP Apps capability even when the
// assembly's app views are about to register, hiding open_app and the app
// inventory from exactly the hosted surface that serves them.
//
// FeatMCPApps is therefore NOT a static transport assumption on hosted: it is
// set only when the shared app-view table (go.lumeweb.com/pinner/mcp/appswire)
// says this assembly will actually install at least one selectable view given
// this deployment's capabilities (a hosted assembly wires NO OOB coordinators
// and its default scope has no Sia vault surface — but those are wiring/scope
// facts, not table hard-codes; a future hosted OOB wiring flips CapOOB on and
// the sign-in card becomes installable here with no table change) and the
// transfer executors wired. The registration call sites below apply the same
// dependencies to the actual install decisions (the presigned
// upload/download coordinators gate their managers; the dependency-free pin
// list and account screens need nothing), so the advertised capability and
// the registered inventory cannot drift in either direction. The CLI's agent
// guide then reads the per-server installed-app records, so prose follows the
// registered set exactly.
func hostedAppProfile(
	surface DomainScope,
	transferExecutorsWired,
	pinsWired bool,
) *hostenv.PlatformProfile {
	p := hostenv.ProfileForTransport(hostenv.TransportHTTP)
	p.DomainScope = surface
	p.Hosted = true
	available := func(spec appswire.ViewSpec) bool {
		switch spec.Launcher {
		case appswire.LauncherUploadManager:
			return transferExecutorsWired
		case appswire.LauncherDownloadManager:
			return transferExecutorsWired
		case appswire.LauncherPinCreator:
			// Hosted assemblies do not pin against a local pinning
			// provider today; a hosting that wires one flips this.
			return pinsWired
		default:
			// Dependency-free rows on the hosted surface: the pin list and
			// account screens attach to tools the hosted scope always
			// registers.
			return true
		}
	}
	// A hosted assembly wires no OOB coordinators and its default scope has
	// no Sia vault surface; both facts are deployment wiring, not table
	// policy, so the capability mask states exactly what this build wires.
	if len(appswire.Selectable(appswire.CapHosted, available)) > 0 {
		p.Features[hostenv.FeatMCPApps] = true
	}
	return &p
}
