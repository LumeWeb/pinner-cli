package model

import (
	"encoding/json"

	"go.lumeweb.com/pinner-cli/internal/mcp/hostenv"
)

// ToolTarget is a complete presentation of a tool for a specific
// capability context. Every field is self-contained — the forge does
// not merge or compose targets.
//
// ToolTarget is the per-profile presentation variant carried on
// ToolDescriptor/ToolEntry for tools that vary by host environment.
// The forge selects the best-matching target at materialization time
// based on the platform's feature set.
type ToolTarget struct {
	// Require lists features that must all be present for this target
	// to be eligible. Empty = matches any platform (universal target).
	Require hostenv.FeatureSet

	// Visible controls whether the tool appears at all for matching
	// platforms. false = suppress the tool entirely for this platform.
	Visible bool

	Description     string
	InputSchema     json.RawMessage
	OutputSchema    json.RawMessage
	Meta            map[string]any
	SecuritySchemes []SecurityScheme
	SensitiveFlags  []string
}
