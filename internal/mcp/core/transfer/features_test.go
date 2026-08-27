package transfer

import (
	"go.lumeweb.com/pinner-cli/internal/mcp/hostenv"
)

// transportFeatures returns the transport-derived feature set implied by the
// wiring flags (coLocated, tunnelOpenAI) for the file-descriptor factories.
func transportFeatures(coLocated, tunnelOpenAI bool) hostenv.FeatureSet {
	return hostenv.ProfileForTransport(UploadFileTransport(coLocated, tunnelOpenAI)).Features
}
