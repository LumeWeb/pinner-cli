package mcp

import (
	"os"
	"testing"

	"go.lumeweb.com/pinner-cli/internal/mcp/sdk"
)

// TestMain installs the hub's tool-registration adapter into the sdk seam
// before any test runs, mirroring the sdk.SetToolRegistrar(registerTool) call
// made at runtime in MCPCommand. App-tool registrations (RegisterAppTool /
// RegisterPinApp / RegisterAppView) depend on that hook, so without this the
// app-view tests would fail with "tool registrar not installed".
func TestMain(m *testing.M) {
	sdk.SetToolRegistrar(registerTool)
	os.Exit(m.Run())
}
