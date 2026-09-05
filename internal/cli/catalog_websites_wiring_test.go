package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/urfave/cli/v3"
	ipfs "go.lumeweb.com/ipfs-sdk"

	"go.lumeweb.com/pinner-cli/internal/catalog"
	"go.lumeweb.com/pinner-cli/internal/catalogops"
)

// TestWebsitesDomainsPositionalMapping ensures every websites_domains_* op
// maps a supplied CLI positional into the op's required input (the Kody-flagged
// bug: domain-only ops were mapping positionals only to "website" and failing
// with "domain is required"). It exercises the framework's canonical mapping
// rule (right-aligned to the declared <arg> slots, surplus rejection) end to
// end for the domains ops.
func TestWebsitesDomainsPositionalMapping(t *testing.T) {
	ops := catalogops.WebsitesOperations(catalogops.WebsitesDeps{})
	byName := map[string]catalog.Operation{}
	for _, op := range ops {
		byName[op.Name()] = op
	}

	// domain-only ops: a single positional maps to the required "domain" input.
	for _, name := range []string{
		"websites_domains_remove",
		"websites_domains_verify",
		"websites_domains_dns_requirements",
		"websites_domains_dane_republish",
		"websites_domains_update",
	} {
		op, ok := byName[name]
		if !ok {
			t.Fatalf("op %s not registered", name)
		}
		input := map[string]any{}
		if err := catalog.MapPositionalArgs(op.Args(), op.Positional(), []string{"example.com"}, input); err != nil {
			t.Fatalf("%s: MapPositionalArgs err: %v", name, err)
		}
		if input["domain"] != "example.com" {
			t.Errorf("%s Positional %q -> input[domain]=%v, want example.com", name, op.Positional(), input["domain"])
		}
	}

	// add: optional website then required domain; a single positional maps to
	// the required domain (right-aligned), two map to both.
	add := byName["websites_domains_add"]
	in1 := map[string]any{}
	if err := catalog.MapPositionalArgs(add.Args(), add.Positional(), []string{"example.com"}, in1); err != nil {
		t.Fatalf("add single arg err: %v", err)
	}
	if in1["domain"] != "example.com" {
		t.Errorf("add single arg -> input[domain]=%v, want example.com", in1["domain"])
	}
	if _, ok := in1["website"]; ok {
		t.Errorf("add single arg should not populate website, got %v", in1["website"])
	}
	in2 := map[string]any{}
	if err := catalog.MapPositionalArgs(add.Args(), add.Positional(), []string{"my-site", "example.com"}, in2); err != nil {
		t.Fatalf("add two args err: %v", err)
	}
	if in2["website"] != "my-site" || in2["domain"] != "example.com" {
		t.Errorf("add two args -> %v, want website=my-site domain=example.com", in2)
	}

	// list: single required website positional.
	list := byName["websites_domains_list"]
	inList := map[string]any{}
	if err := catalog.MapPositionalArgs(list.Args(), list.Positional(), []string{"my-site"}, inList); err != nil {
		t.Fatalf("list arg err: %v", err)
	}
	if inList["website"] != "my-site" {
		t.Errorf("list Positional -> input[website]=%v, want my-site", inList["website"])
	}
}

// TestWebsitesCRUDPositionalMapping pins the strict arg contract for the
// top-level websites CRUD ops (get/update/delete/validate/ssl): a single
// <domain> positional maps to the "website" arg, while flag+positional
// conflicts and surplus trailing args are rejected. This is deliberately NOT
// backwards compatible with the legacy tolerance (--website X <domain> / extra
// trailing args silently ignored) — that BC debt is removed by design, so the
// CLI rejects ambiguous invocations instead of guessing.
func TestWebsitesCRUDPositionalMapping(t *testing.T) {
	ops := catalogops.WebsitesOperations(catalogops.WebsitesDeps{})
	byName := map[string]catalog.Operation{}
	for _, op := range ops {
		byName[op.Name()] = op
	}

	for _, name := range []string{"websites_get", "websites_update", "websites_delete", "websites_validate", "websites_ssl_status"} {
		op, ok := byName[name]
		if !ok {
			t.Fatalf("op %s not registered", name)
		}
		// Single positional maps to the website arg.
		input := map[string]any{}
		if err := catalog.MapPositionalArgs(op.Args(), op.Positional(), []string{"example.com"}, input); err != nil {
			t.Fatalf("%s single positional err: %v", name, err)
		}
		if input["website"] != "example.com" {
			t.Errorf("%s Positional %q -> input[website]=%v, want example.com", name, op.Positional(), input["website"])
		}

		// Surplus trailing arg is rejected (legacy tolerance removed).
		if err := catalog.MapPositionalArgs(op.Args(), op.Positional(), []string{"example.com", "extra"}, map[string]any{}); err == nil {
			t.Errorf("%s: surplus arg should be rejected", name)
		}

		// Flag+positional conflict is rejected (legacy preference removed).
		if err := catalog.MapPositionalArgs(op.Args(), op.Positional(), []string{"example.com"}, map[string]any{"website": "already-set"}); err == nil {
			t.Errorf("%s: flag+positional conflict should be rejected", name)
		}
	}
}

func TestRenderWebsitesResultRejectsTypedNil(t *testing.T) {
	op := catalog.NewOperation(catalog.OperationSpec{Name: "websites_get"})

	// A handler that returns (nil, nil) surfaces as a typed nil *ipfs.DomainResponse.
	var typedNil *ipfs.DomainResponse
	err := renderWebsitesResult(context.Background(), &cli.Command{}, op, typedNil)
	if err == nil {
		t.Fatal("expected an error for a typed-nil *ipfs.DomainResponse result, got nil (would panic)")
	}
}

func TestRenderWebsitesResultVerifyTypedNilRendersNotVerified(t *testing.T) {
	// For verify, a typed nil (the handler returned (nil, nil)) means the
	// domain's DNS could not be resolved yet — the not-verified outcome, not
	// an error.
	op := catalog.NewOperation(catalog.OperationSpec{Name: catalogops.OpWebsitesDomainsVerify})
	var typedNil *ipfs.DomainResponse
	var buf bytes.Buffer
	cmd := &cli.Command{}
	cmd.Writer = &buf
	if err := renderWebsitesResult(context.Background(), cmd, op, typedNil); err != nil {
		t.Fatalf("verify typed-nil should render the not-verified outcome, got err: %v", err)
	}
	if !strings.Contains(buf.String(), "⏳ not verified yet") {
		t.Errorf("expected the not-verified outcome on stdout, got: %q", buf.String())
	}
}

func TestIsNilPointerResultTypedNil(t *testing.T) {
	var p *int
	var s []string
	var m map[string]int
	if !isNilPointerResult(p) {
		t.Error("isNilPointerResult must report true for a typed-nil pointer")
	}
	// nil slice/map are legitimate empty results, not nil-deref hazards — the
	// guard must NOT reject them (regression: `websites domains list` returns a
	// nil slice for an account with no domains).
	if isNilPointerResult(s) || isNilPointerResult(m) {
		t.Error("isNilPointerResult must report false for nil slice/map")
	}
	if isNilPointerResult(5) || isNilPointerResult("x") || isNilPointerResult(&p) {
		t.Error("isNilPointerResult must report false for non-nil values")
	}
}

// TestRenderVerifyGuidance checks the websites_domains_verify failure path
// renders DNS self-service guidance in human output, suppresses it in JSON mode
// (so machine output stays clean), and renders nothing for non-verify ops —
// locking in the behavior the catalog migration dropped.
func TestRenderVerifyGuidance(t *testing.T) {
	capture := func(json bool) (*bytes.Buffer, Output) {
		var buf bytes.Buffer
		out := NewOutputFormatter(json, false, false, false)
		out.SetWriter(&buf)
		return &buf, out
	}

	dnsErr := errors.New("DNSSEC is not active for this domain")

	// Verify op, human output: guidance is rendered.
	bufVerify, outVerify := capture(false)
	opVerify := catalog.NewOperation(catalog.OperationSpec{Name: "websites_domains_verify"})
	renderVerifyGuidance(outVerify, opVerify, dnsErr)
	if bufVerify.Len() == 0 {
		t.Error("expected DNS guidance output for websites_domains_verify error, got none")
	}
	if !strings.Contains(bufVerify.String(), "DNSSEC") {
		t.Errorf("expected DNSSEC guidance text in output, got: %q", bufVerify.String())
	}

	// Verify op, JSON output: guidance is suppressed to keep machine output clean.
	bufJSON, outJSON := capture(true)
	renderVerifyGuidance(outJSON, opVerify, dnsErr)
	if bufJSON.Len() != 0 {
		t.Errorf("expected no guidance in JSON mode, got: %q", bufJSON.String())
	}

	// Non-verify op: no guidance is rendered regardless of format.
	bufOther, outOther := capture(false)
	opOther := catalog.NewOperation(catalog.OperationSpec{Name: "websites_domains_add"})
	renderVerifyGuidance(outOther, opOther, dnsErr)
	if bufOther.Len() != 0 {
		t.Errorf("expected no guidance for a non-verify op, got: %q", bufOther.String())
	}
}

// TestApplyPositionalArgsSurplusRejected guards against silently dropping extra
// CLI arguments: `domains rm good.example bogus` must reject `bogus` rather than
// deleting good.example while ignoring the surplus (restores legacy validation).
func TestApplyPositionalArgsSurplusRejected(t *testing.T) {
	makeOp := func(positional string) catalog.Operation {
		return catalog.NewOperation(catalog.OperationSpec{
			Name:       "websites_domains_remove",
			Positional: positional,
			Args: []catalog.OperationArg{
				{Name: "domain", Type: catalog.ArgTypeString, Required: true},
			},
		})
	}

	// Single-slot op (remove): one arg maps, two args are rejected.
	input := map[string]any{}
	if err := applyPositionalArgs(makeOp("<domain>"), input, &mockArgs{args: []string{"good.example"}}); err != nil {
		t.Fatalf("single arg should map cleanly, got err: %v", err)
	}
	if input["domain"] != "good.example" {
		t.Errorf("domain = %v, want good.example", input["domain"])
	}

	input2 := map[string]any{}
	err := applyPositionalArgs(makeOp("<domain>"), input2, &mockArgs{args: []string{"good.example", "bogus"}})
	if err == nil {
		t.Fatal("expected surplus-arg error, got nil")
	}
	if !strings.Contains(err.Error(), "bogus") {
		t.Errorf("error should name the surplus argument, got: %v", err)
	}

	// Two-slot op with optional lead (add): two args are valid, three are not.
	input3 := map[string]any{}
	err = applyPositionalArgs(catalog.NewOperation(catalog.OperationSpec{
		Name: "websites_domains_add", Positional: "[<website>] <domain>",
	}), input3, &mockArgs{args: []string{"my-site", "good.example", "bogus"}})
	if err == nil {
		t.Fatal("expected surplus-arg error for add with 3 args, got nil")
	}
}
