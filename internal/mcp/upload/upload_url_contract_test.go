package upload

import (
	"testing"

	"github.com/stretchr/testify/require"

	"go.lumeweb.com/pinner-cli/internal/mcp/hostenv"
	"go.lumeweb.com/pinner-cli/internal/mcp/mintcontract"
	"go.lumeweb.com/pinner-cli/internal/mcp/toolforge"
)

// TestRelayURLUploadDescComposesUploadCompletionContract pins the LOW DRY
// regression: upload_url's static preamble composes the shared mintcontract
// upload completion contract (the returned CID is already pinned → pins_add is
// not needed afterward), and its no-relay fallback branch composes the
// canonical UploadMintPoll (which tool, with which handle, until which
// terminal status) — never a paraphrase that drops the completed semantics.
func TestRelayURLUploadDescComposesUploadCompletionContract(t *testing.T) {
	relayCapable, ok := toolforge.ResolveDescription(RelayURLUploadTargets, hostenv.ProfileGrokHTTP)
	require.True(t, ok, "Grok declares FeatSourceURL so the positive copy resolves")
	require.Contains(t, relayCapable,
		mintcontract.FirstUpper(mintcontract.UploadPinnedCIDCompletion)+";",
		"the preamble must carry the canonical no-pins_add completion contract verbatim")

	relayLess, ok := toolforge.ResolveDescription(RelayURLUploadTargets, hostenv.ProfileHTTPGeneric)
	require.True(t, ok, "generic HTTP has no URL-fetch relay so the fallback copy resolves")
	require.Contains(t, relayLess,
		"PUTting the agent-local file to the returned url, then "+mintcontract.UploadMintPoll+".",
		"the no-relay fallback must compose the canonical upload_status poll with the returned handle")
	require.NotContains(t, relayLess, "then poll upload_status.",
		"terse paraphrases of the poll contract must not resurface")
}
