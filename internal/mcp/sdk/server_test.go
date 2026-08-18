package sdk

import (
	"testing"
)

// TestServerOptionsAdvertisesUI verifies the SDK seam advertises the MCP Apps
// UI extension on every server's capabilities (Pinner ships app tooling by
// default), both for explicit and nil options.
func TestServerOptionsAdvertisesUI(t *testing.T) {
	so := serverOptions(&ServerOptions{})
	if so == nil {
		t.Fatal("expected non-nil ServerOptions")
	}
	if so.Capabilities == nil {
		t.Fatal("expected non-nil Capabilities")
	}
	if _, ok := so.Capabilities.Extensions[UICapabilityID]; !ok {
		t.Fatalf("extension %s not advertised on construction", UICapabilityID)
	}
	// Nil options still advertise UI (Pinner ships app tooling by default).
	soNil := serverOptions(nil)
	if soNil.Capabilities == nil {
		t.Fatal("expected UI advertisement even for nil options")
	}
	if _, ok := soNil.Capabilities.Extensions[UICapabilityID]; !ok {
		t.Fatalf("extension %s not advertised for nil options", UICapabilityID)
	}
}
