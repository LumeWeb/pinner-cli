package hostenv

import "strings"

// clientNameCopilotCLI is the exact clientInfo.name the GitHub Copilot CLI
// sends (e.g. {name: "copilot-cli", version: "0.0.0"}). GitHub Copilot CLI is
// a co-located stdio client, so clientInfo.name is the reliable signal (stdio
// carries no User-Agent header).
//
// It is matched EXACTLY (case-insensitive), not by substring: "copilot" is a
// broad token that appears inside unrelated names (e.g. "copilot-workspace",
// "github-copilot"), and a substring match would misclassify any co-located
// client carrying one. Only the full product token "copilot-cli" is accepted
// (the same hardening the cline/kilo detectors apply).
const clientNameCopilotCLI = "copilot-cli"

// copilotDetector matches the GitHub Copilot CLI (clientInfo name
// "copilot-cli"). Its capability surface is identical to the generic stdio
// profile, so it is registered as a profile alias of HostGeneric (see
// profileAliasTargets). It is its own HostType so callers can gate on
// HostIs(HostCopilotCLI) without duplicating a feature set.
type copilotDetector struct{}

func (copilotDetector) Match(req DetectRequest) (HostType, AuthMethod) {
	// GitHub Copilot CLI is a co-located stdio client — never remote or tunnel.
	if !req.CoLocated || req.TunnelOpenAI {
		return HostUnknown, ""
	}
	if req.ClientInfo != nil && strings.EqualFold(strings.TrimSpace(req.ClientInfo.Name), clientNameCopilotCLI) {
		return HostCopilotCLI, AuthNone
	}
	return HostUnknown, ""
}
