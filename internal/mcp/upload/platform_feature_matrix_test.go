package upload

import (
	"testing"

	"github.com/stretchr/testify/require"

	"go.lumeweb.com/pinner-cli/internal/mcp/hostenv"
)

// TestPlatformProfileSourceFeatures pins the source-feature matrix each
// HTTP/stdio platform profile exposes to the upload tools. These features
// gate which byte paths (upload_url relay, inline data: upload_data) are
// registered for a given host and drive the positive tool description copy,
// so they are a behavior contract — not introspection.
func TestPlatformProfileSourceFeatures(t *testing.T) {
	for _, tc := range []struct {
		name                        string
		p                           hostenv.PlatformProfile
		wantMint, wantURL, wantData bool
	}{
		{"http", hostenv.ProfileHTTPGeneric, true, false, false},
		{"grok", hostenv.ProfileGrokHTTP, true, true, true},
		{"stdio", hostenv.ProfileStdioGeneric, false, false, false},
		{"claude", hostenv.ProfileClaudeHTTP, true, false, true},
		{"openaihttp", hostenv.ProfileOpenAIHTTP, true, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.wantMint, tc.p.Features.Has(hostenv.FeatSourceMint), "FeatSourceMint")
			require.Equal(t, tc.wantURL, tc.p.Features.Has(hostenv.FeatSourceURL), "FeatSourceURL")
			require.Equal(t, tc.wantData, tc.p.Features.Has(hostenv.FeatSourceData), "FeatSourceData")
		})
	}
}
