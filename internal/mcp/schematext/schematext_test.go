package schematext

import (
	"testing"

	"github.com/stretchr/testify/require"

	"go.lumeweb.com/mcpplane/transfer"
)

// TestTTLFragmentDefaultsPinned is THE ONE derived-default pin for the TTL
// fragments: every schema surface's ownership test asserts only that its
// generated tag COMPOSES a fragment (Contains), while the derived value of the
// default itself ("e.g. 5m"; "default 5m") is pinned exactly once, here, where
// the fragments are built from DefaultPresignTTL. A default change or a
// normalization change is therefore caught once, not re-pinned per package.
func TestTTLFragmentDefaultsPinned(t *testing.T) {
	// Both example/default clauses derive from the coordinator-aligned
	// DefaultPresignTTL (and DefaultHTTPDownloadTTL for the GET pair —
	// aligned with the upload TTL by the download coordinator).
	require.Equal(t, transfer.DefaultHTTPUploadTTL, DefaultPresignTTL)
	require.Equal(t, "e.g. 5m", TTLExample)
	require.Equal(t, "default 5m", TTLDefault)
	require.Equal(t, "e.g. 5m", TTLDownloadGetExample)
	require.Equal(t, "default 5m", TTLDownloadGetDefault)
}

// TestWrapFragmentsComposed pins the wrap contract composition: both wrap
// descriptions carry the SHARED auto-name and explicit-name sentences, so the
// wrap meaning (website → directory root; explicit names honored; single-file
// uploads only) can never drift between upload_file's live schema and
// upload_data's pinned tag.
func TestWrapFragmentsComposed(t *testing.T) {
	require.Contains(t, WrapDesc, "Wrap a single file in a directory root",
		"the canonical wrap contract must lead with the website→directory-root rule")
	require.Contains(t, WrapDesc, WrapHTMLAutoName)
	require.Contains(t, WrapDesc, WrapExplicitName)

	require.Contains(t, WrapDataDesc, "Required when the upload is a website",
		"the data-relay wrap variant keeps the same website meaning")
	require.Contains(t, WrapDataDesc, WrapHTMLAutoName)
	require.Contains(t, WrapDataDesc, WrapExplicitName)
}

// TestArchiveRootFragments pins the shared website-archive root contract: the
// layout check and the publisher rejection clause are the fragments the
// toolforge upload_file description and the agent guide's site-bundle/CID
// rules compose (parity pinned in the owning packages' tests).
func TestArchiveRootFragments(t *testing.T) {
	require.Contains(t, ArchiveRootCheck, "index.html is at the archive root")
	require.Contains(t, RootRejectClause, "reject a CID whose root lacks index.html")
	require.Contains(t, RootRejectClause, "wrapped in a wrapper directory")
}
