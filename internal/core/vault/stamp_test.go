package vault

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStampedMetadataReservedKeysAreImmutable(t *testing.T) {
	caller := map[string]any{
		"src":     "spoofed",
		"profile": "spoofed",
		"host":    "spoofed",
		"kind":    "artifact",
		"project": "reports",
	}
	m := StampedMetadata("mcp", "claude-desktop", "home", caller)

	// Caller cannot override the reserved write-context keys.
	require.Equal(t, "mcp", m[MetaKeySrc])
	require.Equal(t, "claude-desktop", m[MetaKeyHost])
	require.Equal(t, "home", m[MetaKeyProfile])
	// Caller KV is preserved verbatim.
	require.Equal(t, "artifact", m["kind"])
	require.Equal(t, "reports", m["project"])
	require.Len(t, m, 5)
}

func TestStampedMetadataOmitsEmptyReservedKeys(t *testing.T) {
	m := StampedMetadata("cli", "", "", nil)
	require.Equal(t, map[string]any{MetaKeySrc: "cli"}, m)
	require.NotContains(t, m, MetaKeyHost)
	require.NotContains(t, m, MetaKeyProfile)
}

func TestStampedMetadataNilWhenNothingProvided(t *testing.T) {
	require.Nil(t, StampedMetadata("", "", "", nil))
}

func TestStampedMetadataMergesCallerAgent(t *testing.T) {
	m := StampedMetadata("mcp", "cursor", "home", map[string]any{"agent": "orchestrator-a"})
	require.Equal(t, "orchestrator-a", m["agent"])
	require.Equal(t, "mcp", m[MetaKeySrc])
	require.Equal(t, "cursor", m[MetaKeyHost])
	require.Equal(t, "home", m[MetaKeyProfile])
}
