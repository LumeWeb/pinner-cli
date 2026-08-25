package catalogops

import (
	"context"
	"fmt"
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

// TestWebsitesCreateRequiredFields verifies P0: websites_create requires --cid
// unconditionally, while the domain positional is optional because a platform
// (free) subdomain is minted when no domain is supplied. The platform-claim
// args are exposed on the agent/MCP surface, and marked AgentOnly so they are
// omitted from the CLI flags.
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
	// cid is unconditionally required.
	a := argByName(t, op, "cid")
	if a == nil {
		t.Fatal("websites_create missing arg \"cid\"")
	}
	if !a.Required {
		t.Errorf("websites_create arg \"cid\" must be Required=true")
	}
	// domain positional is optional (no-domain defaults to a minted platform).
	domain := argByName(t, op, "website")
	if domain == nil {
		t.Fatal("websites_create missing arg \"website\"")
	}
	if domain.Required {
		t.Errorf("websites_create arg \"website\" must NOT be Required (no-domain defaults to platform)")
	}
	// The platform-claim args exist and are AgentOnly (CLI omits their flags;
	// the CLI derives the platform claim by parsing/deriving instead).
	agentOnly := []string{"platform", "platform-domain", "platform-namespace", "generate", "label"}
	for _, name := range agentOnly {
		a := argByName(t, op, name)
		if a == nil {
			t.Fatalf("websites_create missing arg %q", name)
		}
		if !a.AgentOnly {
			t.Errorf("websites_create arg %q must be AgentOnly (omitted from the CLI surface)", name)
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

// TestIPNSKeysIDArgsAcceptIntegerRoundtrip is the regression guard for the
// get/delete-by-id bug: ipns_keys_list emits each key's id as a JSON integer
// (the ipfs-sdk IPNSKeyResponse.Id is an int), but the get/delete id args were
// typed ArgTypeString, so the normalizer rejected the integer with "expected
// string, got int" before the handler's StrFlexibleArg ran. The id args must be
// ArgTypeFlexibleID so an integer id passes normalize and is coerced to its
// string form.
func TestIPNSKeysIDArgsAcceptIntegerRoundtrip(t *testing.T) {
	for _, name := range []string{"ipns_keys_get", "ipns_keys_delete"} {
		var op catalog.Operation
		for _, o := range IPNSOperations(IPNSDeps{}) {
			if o.Name() == name {
				op = o
				break
			}
		}
		if op == nil {
			t.Fatalf("%s operation not found", name)
		}
		a := argByName(t, op, "id")
		if a == nil {
			t.Fatalf("%s missing id arg", name)
		}
		if a.Type != catalog.ArgTypeFlexibleID {
			t.Errorf("%s id arg must be ArgTypeFlexibleID so an integer id from ipns_keys_list is accepted; got %v", name, a.Type)
		}
		// The exact round-trip that was broken: an integer id must pass the
		// shared normalize seam and coerce to its string form.
		normalized, err := catalog.NormalizeOperationInput(op, map[string]any{"id": 42})
		if err != nil {
			t.Fatalf("%s with integer id must normalize (was rejected before ArgTypeFlexibleID): %v", name, err)
		}
		if got := catalog.StrFlexibleArg(normalized, "id", ""); got != "42" {
			t.Fatalf("%s integer id must coerce to string form, got %q", name, got)
		}
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
// the catalog DNS record handlers (zone resolution + update + create).
type mockDNSServiceForOps struct {
	updateRecordFunc func(ctx context.Context, id, name, recordType string, record ipfs.RecordRequest) (*ipfs.RecordResponse, error)
	createRecordFunc func(ctx context.Context, id string, record ipfs.RecordRequest) (*ipfs.RecordResponse, error)
	// listRecordsFunc optionally overrides the records returned by ListRecords,
	// letting tests exercise the delete-by-id resolution path.
	listRecordsFunc func(ctx context.Context, id string) ([]ipfs.RecordResponse, error)
	// deleteRecordFunc optionally captures the content selector forwarded to
	// DeleteRecord for content-scoped delete tests.
	deleteRecordFunc func(ctx context.Context, id, name, recordType string, content ...string) error
	// Optional capture cells for the record-type argument passed to get/delete,
	// since those have no request payload to inspect.
	getRecordType    *string
	deleteRecordType *string
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
	if m.createRecordFunc != nil {
		return m.createRecordFunc(ctx, id, r)
	}
	return nil, nil
}
func (m *mockDNSServiceForOps) ListRecords(ctx context.Context, id string) ([]ipfs.RecordResponse, error) {
	if m.listRecordsFunc != nil {
		return m.listRecordsFunc(ctx, id)
	}
	return nil, nil
}
func (m *mockDNSServiceForOps) GetRecord(ctx context.Context, id, name, recordType string) (*ipfs.RecordResponse, error) {
	if m.getRecordType != nil {
		*m.getRecordType = recordType
	}
	return nil, nil
}
func (m *mockDNSServiceForOps) UpdateRecord(ctx context.Context, id, name, recordType string, record ipfs.RecordRequest) (*ipfs.RecordResponse, error) {
	if m.updateRecordFunc != nil {
		return m.updateRecordFunc(ctx, id, name, recordType, record)
	}
	return &ipfs.RecordResponse{ZoneId: 1, Name: name, Type: recordType, Content: record.Content}, nil
}
func (m *mockDNSServiceForOps) DeleteRecord(ctx context.Context, id, name, recordType string, content ...string) error {
	if m.deleteRecordType != nil {
		*m.deleteRecordType = recordType
	}
	if m.deleteRecordFunc != nil {
		return m.deleteRecordFunc(ctx, id, name, recordType, content...)
	}
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

// TestDNSRecordsCreateNormalizesType guards the root fix: the create handler
// must upper-case --type before sending it to the server, so `--type txt`
// produces RecordRequest.Type == "TXT" and never hits the server's case-sensitive
// rejection (which surfaced as the opaque json-unmarshal bomb on the client).
func TestDNSRecordsCreateNormalizesType(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"lowercase type is normalized before send", "txt", "TXT"},
		{"already-uppercase is untouched", "TXT", "TXT"},
		{"mixed case is normalized", "TxT", "TXT"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got ipfs.RecordRequest
			mock := &mockDNSServiceForOps{
				createRecordFunc: func(_ context.Context, _ string, record ipfs.RecordRequest) (*ipfs.RecordResponse, error) {
					got = record
					return &ipfs.RecordResponse{ZoneId: 1, Name: record.Name, Type: record.Type, Content: record.Content}, nil
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
				if o.Name() == "dns_records_create" {
					op = o
					break
				}
			}
			if op == nil {
				t.Fatal("dns_records_create operation not found")
			}
			input := map[string]any{"zone": "123", "type": tt.in, "content": "v=spf1 include:mxroute.com -all"}
			if _, err := op.Handler().Execute(context.Background(), input); err != nil {
				t.Fatalf("create handler: %v", err)
			}
			if got.Type != tt.want {
				t.Errorf("RecordRequest.Type = %q, want %q", got.Type, tt.want)
			}
		})
	}
}

// TestDNSRecordsCreateInvokeAcceptsLowercase routes a lowercase record type
// through the full Catalog.Invoke dispatch (the MCP/agent path), not just the
// handler. The enum gate in resolveArg previously rejected "txt" before the
// handler could normalize; it is now case-insensitive, so a lowercase type
// passes dispatch and the handler uppercases it before it reaches the service.
func TestDNSRecordsCreateInvokeAcceptsLowercase(t *testing.T) {
	var got ipfs.RecordRequest
	mock := &mockDNSServiceForOps{
		createRecordFunc: func(_ context.Context, _ string, record ipfs.RecordRequest) (*ipfs.RecordResponse, error) {
			got = record
			return &ipfs.RecordResponse{ZoneId: 1, Name: record.Name, Type: record.Type, Content: record.Content}, nil
		},
	}
	deps := DNSDeps{
		CfgMgr: func() config.Manager { return configmocks.NewMockManager(t) },
		ServiceFactory: func(_ config.Manager, _ bool, _ ...dns.Option) dns.Service {
			return mock
		},
	}

	c := catalog.NewCatalog()
	for _, o := range DNSOperations(deps) {
		if err := c.Add(o); err != nil {
			t.Fatalf("Add(%q): %v", o.Name(), err)
		}
	}

	for _, tc := range []struct {
		name string
		in   string
	}{
		{"lowercase txt dispatches", "txt"},
		{"mixed-case TxT dispatches", "TxT"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := map[string]any{"zone": "123", "type": tc.in, "content": "v=spf1 include:mxroute.com -all"}
			if _, err := c.Invoke(context.Background(), "dns_records_create", input, catalog.ActorModel); err != nil {
				t.Fatalf("Catalog.Invoke dns_records_create: %v", err)
			}
			if got.Type != "TXT" {
				t.Errorf("after invoke, service received Type = %q, want TXT", got.Type)
			}
		})
	}

	// An out-of-range type is still rejected at the enum gate, even with the
	// case-insensitive match.
	t.Run("bogus type still rejected", func(t *testing.T) {
		input := map[string]any{"zone": "123", "type": "ZZZ", "content": "x"}
		if _, err := c.Invoke(context.Background(), "dns_records_create", input, catalog.ActorModel); err == nil {
			t.Fatal("Catalog.Invoke with out-of-range type must be rejected")
		}
	})
}

// TestDNSRecordsCreateNormalizesMXContent guards that a bare MX exchange host
// (no priority) is given the default priority before it reaches the service, so
// `--type MX --content mail.example.com` does not fail on the backend's
// required-priority validation. An explicit --priority flag wins over any
// priority embedded in the content; an explicit priority in the content is
// preserved when no flag is given.
func TestDNSRecordsCreateNormalizesMXContent(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		priority any // nil = flag not provided
		want     string
	}{
		{"bare host gets default priority", "mail.example.com", nil, "10 mail.example.com"},
		{"explicit priority preserved", "10 mail.example.com", nil, "10 mail.example.com"},
		{"non-default priority preserved", "20 mail.example.com", nil, "20 mail.example.com"},
		{"flag overrides embedded priority", "10 mail.example.com", 30, "30 mail.example.com"},
		{"flag applies to bare host", "mail.example.com", 25, "25 mail.example.com"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got ipfs.RecordRequest
			mock := &mockDNSServiceForOps{
				createRecordFunc: func(_ context.Context, _ string, record ipfs.RecordRequest) (*ipfs.RecordResponse, error) {
					got = record
					return &ipfs.RecordResponse{ZoneId: 1, Name: record.Name, Type: record.Type, Content: record.Content}, nil
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
				if o.Name() == "dns_records_create" {
					op = o
					break
				}
			}
			if op == nil {
				t.Fatal("dns_records_create operation not found")
			}
			input := map[string]any{"zone": "123", "type": "MX", "content": tt.content}
			if tt.priority != nil {
				input["priority"] = tt.priority
			}
			if _, err := op.Handler().Execute(context.Background(), input); err != nil {
				t.Fatalf("create handler: %v", err)
			}
			if got.Content != tt.want {
				t.Errorf("RecordRequest.Content = %q, want %q", got.Content, tt.want)
			}
		})
	}
}

// TestDNSRecordsCreateInvokeDefaultMXPriority drives create through the full
// Catalog.Invoke dispatch (which runs normalizeInputDefaults) and guards the
// actual absence semantics: when --priority is omitted, normalizeInputDefaults
// fills the optional int arg with its sentinel default (-1) rather than 0, and
// the handler must treat that as "not provided" and fall through to
// DefaultMXPriority. A bare MX host must therefore reach the service as
// "10 host", never "0 host".
func TestDNSRecordsCreateInvokeDefaultMXPriority(t *testing.T) {
	mkDeps := func(mock *mockDNSServiceForOps) DNSDeps {
		return DNSDeps{
			CfgMgr: func() config.Manager { return configmocks.NewMockManager(t) },
			ServiceFactory: func(_ config.Manager, _ bool, _ ...dns.Option) dns.Service {
				return mock
			},
		}
	}

	invoke := func(t *testing.T, input map[string]any) (string, error) {
		t.Helper()
		var got string
		mock := &mockDNSServiceForOps{
			createRecordFunc: func(_ context.Context, _ string, record ipfs.RecordRequest) (*ipfs.RecordResponse, error) {
				got = record.Content
				return &ipfs.RecordResponse{ZoneId: 1, Name: record.Name, Type: record.Type, Content: record.Content}, nil
			},
		}
		cat := catalog.NewCatalog()
		for _, o := range DNSOperations(mkDeps(mock)) {
			if err := cat.Add(o); err != nil {
				t.Fatalf("Add(%q): %v", o.Name(), err)
			}
		}
		_, err := cat.Invoke(context.Background(), "dns_records_create", input, catalog.ActorModel)
		return got, err
	}

	t.Run("omitted priority defaults MX host to 10", func(t *testing.T) {
		got, err := invoke(t, map[string]any{"zone": "123", "type": "MX", "content": "mail.example.com"})
		if err != nil {
			t.Fatalf("invoke: %v", err)
		}
		if got != "10 mail.example.com" {
			t.Errorf("omitted --priority -> %q, want %q (DefaultMXPriority must apply, not 0)", got, "10 mail.example.com")
		}
	})

	t.Run("explicit priority is honored", func(t *testing.T) {
		got, err := invoke(t, map[string]any{"zone": "123", "type": "MX", "content": "mail.example.com", "priority": 20})
		if err != nil {
			t.Fatalf("invoke: %v", err)
		}
		if got != "20 mail.example.com" {
			t.Errorf("--priority 20 -> %q, want %q", got, "20 mail.example.com")
		}
	})

	t.Run("explicit priority 0 is honored", func(t *testing.T) {
		got, err := invoke(t, map[string]any{"zone": "123", "type": "MX", "content": "mail.example.com", "priority": 0})
		if err != nil {
			t.Fatalf("invoke: %v", err)
		}
		if got != "0 mail.example.com" {
			t.Errorf("--priority 0 -> %q, want %q", got, "0 mail.example.com")
		}
	})
}

// TestDNSRecordsCreateRejectsInvalidMXPriority guards that an out-of-range or
// malformed --priority value is rejected client-side instead of reaching the
// backend. Integer ranges are enforced in the handler (this direct-handler
// invocation), including -1, which must be rejected when explicitly supplied
// rather than silently defaulted; non-exact-integer numerics (fractional
// floats) are rejected at the catalog resolveArg stage before dispatch sees
// them, so they are covered by the Catalog.Invoke-level test below.
func TestDNSRecordsCreateRejectsInvalidMXPriority(t *testing.T) {
	for _, prio := range []any{-1, 65536, 70000} {
		t.Run(fmt.Sprintf("priority-%v", prio), func(t *testing.T) {
			var called bool
			mock := &mockDNSServiceForOps{
				createRecordFunc: func(_ context.Context, _ string, _ ipfs.RecordRequest) (*ipfs.RecordResponse, error) {
					called = true
					return nil, nil
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
				if o.Name() == "dns_records_create" {
					op = o
					break
				}
			}
			if op == nil {
				t.Fatal("dns_records_create operation not found")
			}
			input := map[string]any{"zone": "123", "type": "MX", "content": "mail.example.com", "priority": prio}
			_, err := op.Handler().Execute(context.Background(), input)
			if err == nil {
				t.Fatalf("expected error for priority %v, got nil", prio)
			}
			if called {
				t.Fatalf("handler reached the DNS service for invalid priority %v", prio)
			}
			if !strings.Contains(err.Error(), "65535") {
				t.Errorf("error %q should mention the valid range", err.Error())
			}
		})
	}
}

// TestDNSRecordsCreateInvokeRejectsMalformedMXPriority drives create through
// Catalog.Invoke and guards that non-integer / fractional --priority values are
// rejected at the nullable-int resolveArg stage before dispatch, so they never
// reach the handler or the DNS service.
func TestDNSRecordsCreateInvokeRejectsMalformedMXPriority(t *testing.T) {
	mkDeps := func() DNSDeps {
		return DNSDeps{
			CfgMgr: func() config.Manager { return configmocks.NewMockManager(t) },
			ServiceFactory: func(_ config.Manager, _ bool, _ ...dns.Option) dns.Service {
				return &mockDNSServiceForOps{
					createRecordFunc: func(_ context.Context, _ string, _ ipfs.RecordRequest) (*ipfs.RecordResponse, error) {
						t.Fatal("DNS service must not be reached for malformed priority")
						return nil, nil
					},
				}
			},
		}
	}
	c := catalog.NewCatalog()
	for _, o := range DNSOperations(mkDeps()) {
		if err := c.Add(o); err != nil {
			t.Fatalf("Add(%q): %v", o.Name(), err)
		}
	}
	for _, prio := range []any{1.5, -0.5, 65535.9, "20"} {
		t.Run(fmt.Sprintf("priority-%v", prio), func(t *testing.T) {
			input := map[string]any{"zone": "123", "type": "MX", "content": "mail.example.com", "priority": prio}
			if _, err := c.Invoke(context.Background(), "dns_records_create", input, catalog.ActorModel); err == nil {
				t.Fatalf("Catalog.Invoke must reject malformed priority %v, got nil error", prio)
			}
		})
	}
}

// TestDNSRecordSelectorsNormalizeType guards that get/update/delete also
// upper-case the type before hitting the server (same case-sensitivity +
// unmarshal-bomb path as create).
func TestDNSRecordSelectorsNormalizeType(t *testing.T) {
	opFor := func(t *testing.T, deps DNSDeps, name string) catalog.Operation {
		t.Helper()
		for _, o := range DNSOperations(deps) {
			if o.Name() == name {
				return o
			}
		}
		t.Fatalf("operation %q not found", name)
		return nil
	}
	mkDeps := func(mock *mockDNSServiceForOps) DNSDeps {
		return DNSDeps{
			CfgMgr: func() config.Manager { return configmocks.NewMockManager(t) },
			ServiceFactory: func(_ config.Manager, _ bool, _ ...dns.Option) dns.Service {
				return mock
			},
		}
	}

	t.Run("get", func(t *testing.T) {
		var gotType string
		mock := &mockDNSServiceForOps{getRecordType: &gotType}
		op := opFor(t, mkDeps(mock), "dns_records_get")
		input := map[string]any{"zone": "123", "name": "www", "type": "txt"}
		if _, err := op.Handler().Execute(context.Background(), input); err != nil {
			t.Fatalf("get handler: %v", err)
		}
		if gotType != "TXT" {
			t.Errorf("get recordType = %q, want TXT", gotType)
		}
	})

	t.Run("update", func(t *testing.T) {
		var gotType string
		mock := &mockDNSServiceForOps{
			updateRecordFunc: func(_ context.Context, _, _, recordType string, _ ipfs.RecordRequest) (*ipfs.RecordResponse, error) {
				gotType = recordType
				return &ipfs.RecordResponse{ZoneId: 1, Name: "www", Type: recordType}, nil
			},
		}
		op := opFor(t, mkDeps(mock), "dns_records_update")
		input := map[string]any{"zone": "123", "name": "www", "type": "txt", "content": "1.2.3.4"}
		if _, err := op.Handler().Execute(context.Background(), input); err != nil {
			t.Fatalf("update handler: %v", err)
		}
		if gotType != "TXT" {
			t.Errorf("update recordType = %q, want TXT", gotType)
		}
	})

	t.Run("delete", func(t *testing.T) {
		var gotType string
		mock := &mockDNSServiceForOps{deleteRecordType: &gotType}
		op := opFor(t, mkDeps(mock), "dns_records_delete")
		input := map[string]any{"zone": "123", "name": "www", "type": "txt", "confirm": true}
		if _, err := op.Handler().Execute(context.Background(), input); err != nil {
			t.Fatalf("delete handler: %v", err)
		}
		if gotType != "TXT" {
			t.Errorf("delete recordType = %q, want TXT", gotType)
		}
	})
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

// TestServerSideListOpsExposeSearch is the standardization contract: every
// *_list catalog operation whose backend supports server-side search exposes a
// generic `search` argument (full-text, evaluated server-side), separate from
// and composing with its structured filters. Ops whose backend has no
// server-side search yet deliberately do not declare `search`, so agents never
// see a search that would be silently ignored.
func TestServerSideListOpsExposeSearch(t *testing.T) {
	listOps := map[string][]catalog.Operation{
		"api_keys_list":   APIKeysOperations(APIKeysDeps{}),
		"operations_list": OperationsOperations(OperationsDeps{}),
		"ipns_keys_list":  IPNSOperations(IPNSDeps{}),
		"pins_list":       PinsOperations(PinsDeps{}),
	}
	for name, ops := range listOps {
		var op catalog.Operation
		for _, o := range ops {
			if o.Name() == name {
				op = o
				break
			}
		}
		if op == nil {
			t.Fatalf("%s operation not found", name)
		}
		a := argByName(t, op, "search")
		if a == nil {
			t.Errorf("%s must declare a search arg so every server-side-searchable list tool supports text search", name)
			continue
		}
		if a.Type != catalog.ArgTypeString {
			t.Errorf("%s search arg must be ArgTypeString, got %v", name, a.Type)
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
		{"CAA", "0 issue"}, // RFC 8659 empty-value form
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
		{"CAA", "issue"},                                    // missing flags
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

// TestDNSRecordsDeleteSelection verifies the dns_records_delete handler
// selection: deleting by --content targets one value, omitting content deletes
// the whole RRSet, and deleting by --id resolves the record by its id (from
// 'dns records list') and deletes that single value.
func TestDNSRecordsDeleteSelection(t *testing.T) {
	run := func(t *testing.T, mock *mockDNSServiceForOps, input map[string]any) (any, error) {
		t.Helper()
		deps := DNSDeps{
			CfgMgr: func() config.Manager { return configmocks.NewMockManager(t) },
			ServiceFactory: func(_ config.Manager, _ bool, _ ...dns.Option) dns.Service {
				return mock
			},
		}
		var op catalog.Operation
		for _, o := range DNSOperations(deps) {
			if o.Name() == "dns_records_delete" {
				op = o
				break
			}
		}
		if op == nil {
			t.Fatal("dns_records_delete operation not found")
		}
		return op.Handler().Execute(context.Background(), input)
	}

	t.Run("forwards content selector", func(t *testing.T) {
		var got []string
		mock := &mockDNSServiceForOps{
			listRecordsFunc: func(_ context.Context, _ string) ([]ipfs.RecordResponse, error) {
				return []ipfs.RecordResponse{{Id: "aaa", Name: "www", Type: "TXT", Content: "v=spf1 include:mxroute.com -all"}}, nil
			},
			deleteRecordFunc: func(_ context.Context, _, _, _ string, content ...string) error {
				got = content
				return nil
			},
		}
		if _, err := run(t, mock, map[string]any{
			"zone": "123", "name": "www", "type": "TXT", "content": "v=spf1 include:mxroute.com -all", "confirm": true,
		}); err != nil {
			t.Fatalf("delete handler: %v", err)
		}
		if len(got) != 1 || got[0] != "v=spf1 include:mxroute.com -all" {
			t.Errorf("expected content forwarded, got %v", got)
		}
	})

	t.Run("rejects unknown content selector", func(t *testing.T) {
		var got []string
		mock := &mockDNSServiceForOps{
			listRecordsFunc: func(_ context.Context, _ string) ([]ipfs.RecordResponse, error) {
				return []ipfs.RecordResponse{{Id: "aaa", Name: "www", Type: "TXT", Content: "v=spf1 include:mxroute.com -all"}}, nil
			},
			deleteRecordFunc: func(_ context.Context, _, _, _ string, content ...string) error {
				got = content
				return nil
			},
		}
		if _, err := run(t, mock, map[string]any{
			"zone": "123", "name": "www", "type": "TXT", "content": "stale/mistyped value", "confirm": true,
		}); err == nil {
			t.Fatal("expected error for unknown content selector")
		}
		if len(got) != 0 {
			t.Errorf("DeleteRecord must not be called for unknown content, got %v", got)
		}
	})

	t.Run("apex '@' content selector resolves the empty-name record", func(t *testing.T) {
		var gotName string
		var gotContent []string
		mock := &mockDNSServiceForOps{
			listRecordsFunc: func(_ context.Context, _ string) ([]ipfs.RecordResponse, error) {
				// Apex records are stored with an empty name; '@' is only the
				// display shorthand (see internal/cli/dns.go).
				return []ipfs.RecordResponse{{Id: "aaa", Name: "", Type: "TXT", Content: "v=spf1 include:mxroute.com -all"}}, nil
			},
			deleteRecordFunc: func(_ context.Context, _ string, name, _ string, content ...string) error {
				gotName = name
				gotContent = content
				return nil
			},
		}
		if _, err := run(t, mock, map[string]any{
			"zone": "123", "name": "@", "type": "TXT", "content": "v=spf1 include:mxroute.com -all", "confirm": true,
		}); err != nil {
			t.Fatalf("apex content delete: %v", err)
		}
		if len(gotContent) != 1 || gotContent[0] != "v=spf1 include:mxroute.com -all" {
			t.Errorf("expected apex content forwarded, got %v", gotContent)
		}
		if gotName != "" {
			t.Errorf("expected apex name normalized to empty string for delete, got %q", gotName)
		}
	})

	t.Run("rejects ambiguous content selector", func(t *testing.T) {
		deleted := false
		mock := &mockDNSServiceForOps{
			listRecordsFunc: func(_ context.Context, _ string) ([]ipfs.RecordResponse, error) {
				// Two TXT records with identical SPF content: a content-scoped
				// delete would erase both, so it must be rejected.
				return []ipfs.RecordResponse{
					{Id: "a1b2c3", Name: "@", Type: "TXT", Content: "v=spf1 include:mxroute.com -all"},
					{Id: "a1b2c3", Name: "@", Type: "TXT", Content: "v=spf1 include:mxroute.com -all"},
				}, nil
			},
			deleteRecordFunc: func(_ context.Context, _, _, _ string, _ ...string) error {
				deleted = true
				return nil
			},
		}
		if _, err := run(t, mock, map[string]any{
			"zone": "123", "name": "@", "type": "TXT", "content": "v=spf1 include:mxroute.com -all", "confirm": true,
		}); err == nil {
			t.Fatal("expected error for ambiguous content selector")
		}
		if deleted {
			t.Fatal("ambiguous content must not be forwarded to DeleteRecord")
		}
	})

	t.Run("omitting content deletes whole rrset", func(t *testing.T) {
		var got []string
		var listed bool
		mock := &mockDNSServiceForOps{
			listRecordsFunc: func(_ context.Context, _ string) ([]ipfs.RecordResponse, error) {
				listed = true
				return nil, fmt.Errorf("ListRecords must not be called for a whole-RRSet delete")
			},
			deleteRecordFunc: func(_ context.Context, _, _, _ string, content ...string) error {
				got = content
				return nil
			},
		}
		if _, err := run(t, mock, map[string]any{
			"zone": "123", "name": "www", "type": "A", "confirm": true,
		}); err != nil {
			t.Fatalf("delete handler: %v", err)
		}
		if len(got) != 0 {
			t.Errorf("expected no content forwarded (whole RRSet), got %v", got)
		}
		if listed {
			t.Error("whole-RRSet delete must not list records; it should go straight to DeleteRecord")
		}
	})

	t.Run("deletes single record by id", func(t *testing.T) {
		var gotName, gotType string
		var gotContent []string
		mock := &mockDNSServiceForOps{
			listRecordsFunc: func(_ context.Context, zoneID string) ([]ipfs.RecordResponse, error) {
				return []ipfs.RecordResponse{
					{Id: "aaa", ZoneId: 123, Name: "www", Type: "A", Content: "192.0.2.10"},
					{Id: "bbb", ZoneId: 123, Name: "www", Type: "A", Content: "192.0.2.11"},
				}, nil
			},
			deleteRecordFunc: func(_ context.Context, _, name, recordType string, content ...string) error {
				gotName, gotType = name, recordType
				gotContent = content
				return nil
			},
		}
		res, err := run(t, mock, map[string]any{
			"zone": "123", "id": "bbb", "confirm": true,
		})
		if err != nil {
			t.Fatalf("delete by id: %v", err)
		}
		if gotName != "www" || gotType != "A" {
			t.Errorf("expected record name/type www/A, got %s/%s", gotName, gotType)
		}
		if len(gotContent) != 1 || gotContent[0] != "192.0.2.11" {
			t.Errorf("expected content 192.0.2.11 forwarded, got %v", gotContent)
		}
		if result, ok := res.(*DNSRecordDeleteResult); !ok || result.ID != "bbb" || result.Content != "192.0.2.11" {
			t.Errorf("unexpected delete result: %#v", res)
		}
	})

	t.Run("rejects unknown record id", func(t *testing.T) {
		mock := &mockDNSServiceForOps{
			listRecordsFunc: func(_ context.Context, _ string) ([]ipfs.RecordResponse, error) {
				return []ipfs.RecordResponse{{Id: "aaa", Name: "www", Type: "A", Content: "192.0.2.10"}}, nil
			},
		}
		if _, err := run(t, mock, map[string]any{
			"zone": "123", "id": "nope", "confirm": true,
		}); err == nil {
			t.Fatal("expected error for unknown record id")
		}
	})

	t.Run("rejects empty-content record to avoid rrset wipe", func(t *testing.T) {
		deleted := false
		mock := &mockDNSServiceForOps{
			listRecordsFunc: func(_ context.Context, _ string) ([]ipfs.RecordResponse, error) {
				return []ipfs.RecordResponse{{Id: "aaa", Name: "www", Type: "TXT", Content: ""}}, nil
			},
			deleteRecordFunc: func(_ context.Context, _, _, _ string, _ ...string) error {
				deleted = true
				return nil
			},
		}
		if _, err := run(t, mock, map[string]any{
			"zone": "123", "id": "aaa", "confirm": true,
		}); err == nil {
			t.Fatal("expected error for empty-content record")
		}
		if deleted {
			t.Fatal("empty-content record must not be forwarded to DeleteRecord")
		}
	})

	t.Run("rejects ambiguous id when records share identical content", func(t *testing.T) {
		deleted := false
		mock := &mockDNSServiceForOps{
			listRecordsFunc: func(_ context.Context, _ string) ([]ipfs.RecordResponse, error) {
				// Two TXT records with identical SPF content hash to the same id
				// ("a1b2c3"), so an id-scoped delete would erase both.
				return []ipfs.RecordResponse{
					{Id: "a1b2c3", Name: "@", Type: "TXT", Content: "v=spf1 include:mxroute.com -all"},
					{Id: "a1b2c3", Name: "@", Type: "TXT", Content: "v=spf1 include:mxroute.com -all"},
					{Id: "d4e5f6", Name: "@", Type: "TXT", Content: "v=spf1 a -all"},
				}, nil
			},
			deleteRecordFunc: func(_ context.Context, _, _, _ string, _ ...string) error {
				deleted = true
				return nil
			},
		}
		if _, err := run(t, mock, map[string]any{
			"zone": "123", "id": "a1b2c3", "confirm": true,
		}); err == nil {
			t.Fatal("expected error for ambiguous duplicate-content id")
		}
		if deleted {
			t.Fatal("ambiguous id must not be forwarded to DeleteRecord")
		}
	})
}
