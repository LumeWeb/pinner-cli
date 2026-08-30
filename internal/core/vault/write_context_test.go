package vault

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWriteContextProfile(t *testing.T) {
	require.Equal(t, "", WriteContextProfile(nil))
	require.Equal(t, "", WriteContextProfile(map[string]any{}))

	work := map[string]any{MetaKeyProfile: "work"}
	require.Equal(t, "work", WriteContextProfile(work))

	// A non-string profile value decodes as empty rather than panicking.
	bad := map[string]any{MetaKeyProfile: 7}
	require.Equal(t, "", WriteContextProfile(bad))
}

func TestWriteContextColumns(t *testing.T) {
	src, host, agent := WriteContextColumns(nil)
	require.Equal(t, "", src)
	require.Equal(t, "", host)
	require.Equal(t, "", agent)

	m := map[string]any{
		MetaKeySrc:     "mcp",
		MetaKeyHost:    "cursor",
		"agent":        "orch-a",
		MetaKeyProfile: "work",
	}
	src, host, agent = WriteContextColumns(m)
	require.Equal(t, "mcp", src)
	require.Equal(t, "cursor", host)
	require.Equal(t, "orch-a", agent)
}
