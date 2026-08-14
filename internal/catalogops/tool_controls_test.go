package catalogops

import (
	"context"
	"strings"
	"testing"

	"go.lumeweb.com/pinner-cli/internal/catalog"
)

// argByName returns the named OperationArg from an operation, or nil.
func argByName(t *testing.T, op catalog.Operation, name string) *catalog.OperationArg {
	t.Helper()
	for i := range op.Args() {
		if op.Args()[i].Name == name {
			return &op.Args()[i]
		}
	}
	return nil
}

// websitesOps builds the websites operations with nil deps; the descriptors
// are inspectable without invoking handlers.
func websitesOps() []catalog.Operation {
	return WebsitesOperations(WebsitesDeps{})
}

// TestWebsitesCreateRequiredFields verifies P0: websites_create requires both
// the website (domain) and cid identity fields, so an agent cannot create a
// site with no target.
func TestWebsitesCreateRequiredFields(t *testing.T) {
	var op catalog.Operation
	for _, o := range websitesOps() {
		if o.Name() == "websites_create" {
			op = o
			break
		}
	}
	if op == nil {
		t.Fatal("websites_create operation not found")
	}
	for _, name := range []string{"website", "cid"} {
		a := argByName(t, op, name)
		if a == nil {
			t.Fatalf("websites_create missing arg %q", name)
		}
		if !a.Required {
			t.Errorf("websites_create arg %q must be Required=true", name)
		}
	}
}

// TestWebsitesDeleteRequiredFields verifies P0: websites_delete requires both
// the website target and confirm before a destructive delete can proceed.
func TestWebsitesDeleteRequiredFields(t *testing.T) {
	var op catalog.Operation
	for _, o := range websitesOps() {
		if o.Name() == "websites_delete" {
			op = o
			break
		}
	}
	if op == nil {
		t.Fatal("websites_delete operation not found")
	}
	for _, name := range []string{"website", "confirm"} {
		a := argByName(t, op, name)
		if a == nil {
			t.Fatalf("websites_delete missing arg %q", name)
		}
		if !a.Required {
			t.Errorf("websites_delete arg %q must be Required=true", name)
		}
	}
}

// TestIPNSKeysDeleteRequiresAgentConfirm verifies P0: ipns_keys_delete declares
// a confirm arg that is AgentRequired (MCP-only) so an agent cannot destroy a
// key without confirmation, while staying out of the CLI's required set (the
// CLI deletes keys without --force; see ipns_wiring.go).
func TestIPNSKeysDeleteRequiresAgentConfirm(t *testing.T) {
	var op catalog.Operation
	for _, o := range IPNSOperations(IPNSDeps{}) {
		if o.Name() == "ipns_keys_delete" {
			op = o
			break
		}
	}
	if op == nil {
		t.Fatal("ipns_keys_delete operation not found")
	}
	if op.Safety() != catalog.SafetyDestructive {
		t.Errorf("ipns_keys_delete must be SafetyDestructive")
	}
	a := argByName(t, op, "confirm")
	if a == nil {
		t.Fatal("ipns_keys_delete must declare a confirm arg for the destructive hand-off")
	}
	if !a.AgentRequired {
		t.Errorf("ipns_keys_delete confirm must be AgentRequired (MCP-only), not shared Required")
	}
	if a.Required {
		t.Errorf("ipns_keys_delete confirm must NOT be shared Required (CLI deletes without --force)")
	}
}

// TestIPNSKeysDeleteConfirmGate enforces the handler-side confirm check: a
// direct invocation passing confirm != true must be refused even though it is
// outside the model ActorModel SafetyDestructive gate. The confirm check is the
// first statement, so this returns before any service construction.
func TestIPNSKeysDeleteConfirmGate(t *testing.T) {
	var op catalog.Operation
	for _, o := range IPNSOperations(IPNSDeps{}) {
		if o.Name() == "ipns_keys_delete" {
			op = o
			break
		}
	}
	if op == nil {
		t.Fatal("ipns_keys_delete operation not found")
	}
	if _, err := op.Handler().Execute(context.Background(), map[string]any{"id": "k1"}); err == nil {
		t.Fatal("ipns_keys_delete without confirm must error")
	} else if !strings.Contains(err.Error(), "confirmation is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestIPNSKeysDeleteConfirmDefaultSatisfiesSharedRoute verifies the CLI path is
// not broken: the confirm arg defaults to true, so NormalizeOperationInput (the
// shared seam the CLI adapter feeds) fills confirm=true and the handler gate
// proceeds rather than rejecting a CLI delete that omits --confirm.
func TestIPNSKeysDeleteConfirmDefaultSatisfiesSharedRoute(t *testing.T) {
	var op catalog.Operation
	for _, o := range IPNSOperations(IPNSDeps{}) {
		if o.Name() == "ipns_keys_delete" {
			op = o
			break
		}
	}
	if op == nil {
		t.Fatal("ipns_keys_delete operation not found")
	}
	normalized, err := catalog.NormalizeOperationInput(op, map[string]any{"id": "k1"})
	if err != nil {
		t.Fatalf("NormalizeOperationInput: %v", err)
	}
	confirm, ok := normalized["confirm"].(bool)
	if !ok || !confirm {
		t.Fatalf("confirm must normalize to true via its Default so the CLI adapter's injected value satisfies the gate; got %v (ok=%v)", normalized["confirm"], ok)
	}
}

// TestDNSRecordTypeEnum verifies the type Enum is on dns_records_create only
// (where A/AAAA/CNAME/MX/NS/TXT are the full creation set). get/update/delete
// must remain free-form because they target existing records that may be other
// types (SRV, CAA, PTR, SOA, ...) and the DNS service passes type through
// unvalidated.
func TestDNSRecordTypeEnum(t *testing.T) {
	want := []string{"A", "AAAA", "CNAME", "MX", "NS", "TXT"}
	for _, o := range DNSOperations(DNSDeps{}) {
		name := o.Name()
		a := argByName(t, o, "type")
		if a == nil {
			continue // op without a type arg (zones list/get, etc.)
		}
		if name == "dns_records_create" {
			if len(a.Enum) != len(want) {
				t.Errorf("%s type enum = %v, want %v", name, a.Enum, want)
				continue
			}
			for i, e := range want {
				if a.Enum[i] != e {
					t.Errorf("%s type enum[%d] = %q, want %q", name, i, a.Enum[i], e)
				}
			}
			continue
		}
		if name == "dns_records_get" || name == "dns_records_update" || name == "dns_records_delete" {
			if len(a.Enum) != 0 {
				t.Errorf("%s type must NOT be Enum-constrained (existing records may be other types), got %v", name, a.Enum)
			}
		}
	}
}
