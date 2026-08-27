package mcp

import (
	"go.lumeweb.com/pinner-cli/internal/mcp/core/transfer"
	"go.lumeweb.com/pinner-cli/internal/mcp/hostenv"
)

// transportFeatures returns the transport-derived feature set implied by the
// wiring flags (coLocated, tunnelOpenAI) for the file-descriptor factories.
// It is the general rule for choosing a descriptor's features argument.
func transportFeatures(coLocated, tunnelOpenAI bool) hostenv.FeatureSet {
	return hostenv.ProfileForTransport(transfer.UploadFileTransport(coLocated, tunnelOpenAI)).Features
}
