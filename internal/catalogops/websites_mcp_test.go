package catalogops

import (
	"testing"

	"github.com/stretchr/testify/require"

	"go.lumeweb.com/pinner-cli/internal/mcp/hostenv"
)

// TestWebsitesCreateDescGrok verifies the per-profile MCP description for
// websites_create resolves correctly for Grok (mint-only HTTP, no file
// parameter). Key invariants:
//   - The description must contain the CID structure invariant.
//   - It must contain the "do NOT combine" guidance.
//   - It must contain the archive_mode=convert guidance.
//   - It must contain the wrap=true / auto-name guidance.
//   - It must contain the pins_add-unnecessary guidance (reworded to avoid
//
// instruction-like "do NOT call" phrasing the directory validators flag).
//   - It must NOT contain the file-parameter preferred-path clause (Grok has no file).
func TestWebsitesCreateDescGrok(t *testing.T) {
	desc := websitesCreateDesc.Resolve(hostenv.ProfileGrokHTTP)

	require.Contains(t, desc, "Create a website that serves an IPFS CID")
	require.Contains(t, desc, "directory whose root contains index.html")
	require.Contains(t, desc, "rejects a CID whose root has no index.html")
	require.Contains(t, desc, "A multi-file website is published as its component files")
	require.Contains(t, desc, "archive_mode=convert")
	require.Contains(t, desc, "wrap=true")
	require.Contains(t, desc, "auto-names wrapped HTML to index.html")
	require.Contains(t, desc, "starter-site")
	require.Contains(t, desc, `{"cid":"<cid>"}`)
	require.Contains(t, desc, "platform subdomain is auto-minted")
	require.Contains(t, desc, "a domain or label is not invented for a generic request")
	require.Contains(t, desc, "the upload already pinned it, so pins_add after upload is unnecessary")
	require.Contains(t, desc, "publish_website flow")
	require.Contains(t, desc, "Returns the created website")

	require.NotContains(t, desc, "file parameter is the preferred byte path")
}

// TestWebsitesCreateDescOpenAITunnel verifies the per-profile MCP description
// for websites_create on the OpenAI tunnel (file parameter available).
// The file-parameter preferred-path clause must be present.
func TestWebsitesCreateDescOpenAITunnel(t *testing.T) {
	desc := websitesCreateDesc.Resolve(hostenv.ProfileOpenAITunnel)

	require.Contains(t, desc, "Create a website that serves an IPFS CID")
	require.Contains(t, desc, "directory whose root contains index.html")
	require.Contains(t, desc, "A multi-file website is published as its component files")
	require.Contains(t, desc, "archive_mode=convert")
	require.Contains(t, desc, "wrap=true")
	require.Contains(t, desc, "the upload already pinned it, so pins_add after upload is unnecessary")
	require.Contains(t, desc, "file parameter is the preferred byte path",
		"OpenAI tunnel has FeatFileHostInput and must see the file-parameter clause")
}

// TestWebsitesCreateDescStdio verifies the per-profile MCP description for
// websites_create on the generic stdio transport (no file parameter, has
// source.path).
func TestWebsitesCreateDescStdio(t *testing.T) {
	desc := websitesCreateDesc.Resolve(hostenv.ProfileStdioGeneric)

	require.Contains(t, desc, "Create a website that serves an IPFS CID")
	require.Contains(t, desc, "directory whose root contains index.html")
	require.Contains(t, desc, "archive_mode=convert")
	require.Contains(t, desc, "the upload already pinned it, so pins_add after upload is unnecessary")

	require.NotContains(t, desc, "file parameter is the preferred byte path",
		"stdio generic has no FeatFileHostInput")
}

// TestWebsitesCreateTargetsCarriesDescFunc verifies the catalog.Target slice
// for websites_create carries a DescFunc (not a static Description), so the
// MCP bridge will resolve it per-profile at runtime.
func TestWebsitesCreateTargetsCarriesDescFunc(t *testing.T) {
	require.Len(t, websitesCreateTargets, 1)
	target := websitesCreateTargets[0]
	require.True(t, target.Visible)
	require.Nil(t, target.Require)
	require.NotNil(t, target.DescFunc,
		"websites_create target must carry a DescFunc for per-profile resolution")
	require.Empty(t, target.Description,
		"websites_create target must not carry a static Description when DescFunc is set")
}

// TestWebsitesCreateDescNoSuperParagraph verifies the resolved description is
// composed from discrete sentences (each ending with a period) rather than
// being a single monolithic string. This is a structural assertion: the
// resolved text must contain multiple sentence boundaries.
func TestWebsitesCreateDescNoSuperParagraph(t *testing.T) {
	desc := websitesCreateDesc.Resolve(hostenv.ProfileGrokHTTP)
	require.Greater(t, len(desc), 200, "description should be substantial")

	segments := websitesCreateDesc.ResolveSegments(hostenv.ProfileGrokHTTP)
	require.Greater(t, len(segments), 5,
		"description must be composed from multiple segments, not a single string")
}
