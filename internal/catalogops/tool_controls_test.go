package catalogops

import (
	"context"
	"strings"
	"testing"

	ipfs "go.lumeweb.com/ipfs-sdk"

	"go.lumeweb.com/pinner-cli/internal/catalog"
	"go.lumeweb.com/pinner-cli/internal/core/config"
	configmocks "go.lumeweb.com/pinner-cli/internal/core/config/mocks"
	"go.lumeweb.com/pinner-cli/internal/core/dns"
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
// and covers the full backend-addressable set (A/AAAA/CNAME/MX/NS/TXT plus the
// extended SRV/CAA/PTR/SOA). get/update/delete remain free-form because they
// target existing records that may be other types and the DNS service passes
// type through unvalidated.
func TestDNSRecordTypeEnum(t *testing.T) {
	want := []string{"A", "AAAA", "CNAME", "MX", "NS", "TXT", "SRV", "CAA", "PTR", "SOA"}
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

// mockDNSServiceForOps is a minimal dns.Service stub sufficient to exercise
// the catalog DNS record handlers (zone resolution + update).
type mockDNSServiceForOps struct {
	updateRecordFunc func(ctx context.Context, id, name, recordType string, record ipfs.RecordRequest) (*ipfs.RecordResponse, error)
}

func (m *mockDNSServiceForOps) SetAuthToken(string)         {}
func (m *mockDNSServiceForOps) RequireAuthenticated() error { return nil }
func (m *mockDNSServiceForOps) CreateZone(ctx context.Context, d string, ns []string) (*ipfs.ZoneResponse, error) {
	return nil, nil
}
func (m *mockDNSServiceForOps) ListZones(ctx context.Context) ([]ipfs.ZoneListResponse, error) {
	return nil, nil
}
func (m *mockDNSServiceForOps) GetZone(ctx context.Context, id string) (*ipfs.ZoneResponse, error) {
	return nil, nil
}
func (m *mockDNSServiceForOps) DeleteZone(ctx context.Context, id string) error { return nil }
func (m *mockDNSServiceForOps) ValidateZone(ctx context.Context, id string) (*ipfs.ValidationResponse, error) {
	return nil, nil
}
func (m *mockDNSServiceForOps) CreateRecord(ctx context.Context, id string, r ipfs.RecordRequest) (*ipfs.RecordResponse, error) {
	return nil, nil
}
func (m *mockDNSServiceForOps) ListRecords(ctx context.Context, id string) ([]ipfs.RecordResponse, error) {
	return nil, nil
}
func (m *mockDNSServiceForOps) GetRecord(ctx context.Context, id, name, recordType string) (*ipfs.RecordResponse, error) {
	return nil, nil
}
func (m *mockDNSServiceForOps) UpdateRecord(ctx context.Context, id, name, recordType string, record ipfs.RecordRequest) (*ipfs.RecordResponse, error) {
	if m.updateRecordFunc != nil {
		return m.updateRecordFunc(ctx, id, name, recordType, record)
	}
	return &ipfs.RecordResponse{ZoneId: 1, Name: name, Type: recordType, Content: record.Content}, nil
}
func (m *mockDNSServiceForOps) DeleteRecord(ctx context.Context, id, name, recordType string) error {
	return nil
}

// TestDNSRecordsUpdateOmitsUnchangedFields guards the dns_records_update root
// fix: ttl and disabled are omitempty on the wire, so omitting them must leave
// them unchanged (Handler sends nil pointers), while providing them sets them.
// This restores the documented "fields not provided are left unchanged"
// contract that the previous default-filling (ttl=3600, disabled=false) broke.
func TestDNSRecordsUpdateOmitsUnchangedFields(t *testing.T) {
	var op catalog.Operation
	for _, o := range DNSOperations(DNSDeps{}) {
		if o.Name() == "dns_records_update" {
			op = o
			break
		}
	}
	if op == nil {
		t.Fatal("dns_records_update operation not found")
	}

	verify := func(t *testing.T, input map[string]any, wantTtlNil, wantDisabledNil bool, wantTtl int, wantDisabled bool) {
		t.Helper()
		var got ipfs.RecordRequest
		mock := &mockDNSServiceForOps{
			updateRecordFunc: func(_ context.Context, _, _, _ string, record ipfs.RecordRequest) (*ipfs.RecordResponse, error) {
				got = record
				return &ipfs.RecordResponse{ZoneId: 1, Name: "www", Type: "A", Content: record.Content}, nil
			},
		}
		deps := DNSDeps{
			CfgMgr: func() config.Manager { return configmocks.NewMockManager(t) },
			ServiceFactory: func(_ config.Manager, _ bool, _ ...dns.Option) dns.Service {
				return mock
			},
		}
		var op catalog.Operation
		for _, o := range DNSOperations(deps) {
			if o.Name() == "dns_records_update" {
				op = o
				break
			}
		}
		_, err := op.Handler().Execute(context.Background(), input)
		if err != nil {
			t.Fatalf("update handler: %v", err)
		}
		if (got.Ttl == nil) != wantTtlNil {
			t.Errorf("ttl nil=%v, want nil=%v (value=%v)", got.Ttl == nil, wantTtlNil, got.Ttl)
		}
		if got.Ttl != nil && *got.Ttl != wantTtl {
			t.Errorf("ttl=%d, want %d", *got.Ttl, wantTtl)
		}
		if (got.Disabled == nil) != wantDisabledNil {
			t.Errorf("disabled nil=%v, want nil=%v (value=%v)", got.Disabled == nil, wantDisabledNil, got.Disabled)
		}
		if got.Disabled != nil && *got.Disabled != wantDisabled {
			t.Errorf("disabled=%v, want %v", *got.Disabled, wantDisabled)
		}
	}

	base := map[string]any{"zone": "123", "name": "www", "type": "A", "content": "1.2.3.4"}

	t.Run("omits ttl and disabled when not provided", func(t *testing.T) {
		verify(t, base, true, true, 0, false)
	})
	t.Run("sets ttl when provided", func(t *testing.T) {
		in := mapsCopy(base)
		in["ttl"] = 300
		verify(t, in, false, true, 300, false)
	})
	t.Run("sets disabled when provided", func(t *testing.T) {
		in := mapsCopy(base)
		in["disabled"] = true
		verify(t, in, true, false, 0, true)
	})
}

func mapsCopy(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func forDNSOp(name string) catalog.Operation {
	for _, o := range DNSOperations(DNSDeps{}) {
		if o.Name() == name {
			return o
		}
	}
	return nil
}

// TestDNSRecordsDeleteConfirmRequired verifies the destructive DNS deletes
// declare a shared-Required confirm (no default), consistent with zones delete
// and websites_delete, so the MCP schema requires confirmation.
func TestDNSRecordsDeleteConfirmRequired(t *testing.T) {
	for _, name := range []string{"dns_zones_delete", "dns_records_delete"} {
		a := argByName(t, forDNSOp(name), "confirm")
		if a == nil {
			t.Fatalf("%s missing confirm arg", name)
		}
		if !a.Required || a.Default != "" {
			t.Errorf("%s confirm must be Required with no Default, got Required=%v Default=%q", name, a.Required, a.Default)
		}
	}
}

// TestDNSRecordExtendedTypeValidation covers the expanded SRV/CAA/SOA/PTR
// record types: valid content passes, malformed content is rejected.
func TestDNSRecordExtendedTypeValidation(t *testing.T) {
	valid := []struct{ typ, content string }{
		{"SRV", "10 60 5060 sip.example.com"},
		{"SRV", "0 0 443 _https._tcp.example.com"},
		{"CAA", "0 issue letsencrypt.org"},
		{"CAA", "128 issuewild example.com"},
		{"CAA", "0 iodef mailto:security@example.com"},
		{"SOA", "ns1.example.com hostmaster.example.com 2024010101 7200 3600 1209600 3600"},
		{"PTR", "host.example.com"},
	}
	for _, tc := range valid {
		if err := validateDNSRecord(tc.typ, tc.content); err != nil {
			t.Errorf("validateDNSRecord(%s, %q) unexpected error: %v", tc.typ, tc.content, err)
		}
	}

	invalid := []struct{ typ, content string }{
		{"SRV", "10 60 5060"},                               // missing target
		{"SRV", "a 60 5060 sip.example.com"},                // non-numeric priority
		{"SRV", "10 60 0 sip.example.com"},                  // port 0
		{"CAA", "0 issue"},                                  // missing value
		{"CAA", "256 issue letsencrypt.org"},                // flags > 255
		{"CAA", "0 bogustag example.com"},                   // unknown tag
		{"SOA", "ns1.example.com hostmaster.example.com 1"}, // too few fields
		{"SOA", "ns1.example.com 2024010101 7200"},          // rname not present as domain
		{"PTR", "not a domain"},
	}
	for _, tc := range invalid {
		if err := validateDNSRecord(tc.typ, tc.content); err == nil {
			t.Errorf("validateDNSRecord(%s, %q) expected error, got nil", tc.typ, tc.content)
		}
	}
}
