package mcp

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.lumeweb.com/pinner-cli/internal/catalogops"
)

// Verify the account_otp_disable op is registered in AccountOperations (the
// source AssembleCatalogOps feeds from for the MCP surface).
func TestAccountOTPDisableOpsRegisteredInMCPSurface(t *testing.T) {
	ops := catalogops.AccountOperations(catalogops.AccountDeps{})
	names := map[string]bool{}
	for _, op := range ops {
		names[op.Name()] = true
	}
	for _, want := range []string{
		"account_otp_disable",
	} {
		require.True(t, names[want], "MCP surface should expose %s", want)
	}
}
