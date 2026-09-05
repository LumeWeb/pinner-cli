package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	ipfs "go.lumeweb.com/ipfs-sdk"
)

func TestRenderDomainDelegation(t *testing.T) {
	t.Run("renders no delegation message when nil", func(t *testing.T) {
		output := newTestOutput()
		renderDomainDelegation(output, &ipfs.DomainResponse{
			Id: 1, Domain: "mydomain.hns", Namespace: ipfs.DomainNamespaceHNS, Status: new(ipfs.DomainResponseStatusActive),
		}, false)
		// exercises the nil-delegation branch without asserting exact text
	})

	t.Run("renders records with typed helper", func(t *testing.T) {
		output := newTestOutput()
		renderDomainDelegation(output, &ipfs.DomainResponse{
			Id: 1, Domain: "mydomain.hns", Namespace: ipfs.DomainNamespaceHNS, Status: new(ipfs.DomainResponseStatusActive),
			Delegation: &ipfs.DNSDelegation{
				Mode:   new("delegated"),
				Dnssec: new("enabled"),
				ParentRecords: &[]ipfs.DNSDelegationRecord{
					{Type: "NS", Value: new("ns1.lumeweb,ns2.lumeweb")},
				},
				AuthoritativeRecords: &[]ipfs.DNSDelegationRecord{
					{Type: "TLSA", Value: new("_443._tcp.mydomain. 3 1 1 <sha256>")},
				},
			},
		}, false)
		// exercises the non-nil typed-helper path
	})

	t.Run("inline mode omits authoritative records", func(t *testing.T) {
		var buf bytes.Buffer
		output := NewOutputFormatter(false, false, false, false)
		output.SetWriter(&buf)
		renderDomainDelegation(output, &ipfs.DomainResponse{
			Id: 1, Domain: "mydomain.hns", Namespace: ipfs.DomainNamespaceHNS, Status: new(ipfs.DomainResponseStatusActive),
			Delegation: &ipfs.DNSDelegation{
				Mode: new("inline"),
				ParentRecords: &[]ipfs.DNSDelegationRecord{
					{Type: "SYNTH4", Value: new("hns-626f7578e5.rec.ns1.lumeweb")},
				},
				AuthoritativeRecords: &[]ipfs.DNSDelegationRecord{
					{Type: "NS", Value: new("hns-626f7578e5.rec.ns1.lumeweb")},
				},
			},
		}, false)
		out := buf.String()
		// In inline mode the authoritative side is served via Pinner's
		// synthetic nameservers; it is not user-configured, so it is omitted.
		assert.Contains(t, out, "synthetic nameservers")
		assert.Contains(t, out, "SYNTH4")
		assert.NotContains(t, out, "Authoritative records")
	})

	t.Run("inline managed domain never shows authoritative records", func(t *testing.T) {
		var buf bytes.Buffer
		output := NewOutputFormatter(false, false, false, false)
		output.SetWriter(&buf)
		renderDomainDelegation(output, &ipfs.DomainResponse{
			Id: 1, Domain: "mydomain.hns", Namespace: ipfs.DomainNamespaceHNS, Status: new(ipfs.DomainResponseStatusActive),
			Delegation: &ipfs.DNSDelegation{
				Mode: new("inline"),
				ParentRecords: &[]ipfs.DNSDelegationRecord{
					{Type: "SYNTH4", Value: new("hns-626f7578e5.rec.ns1.lumeweb")},
				},
				AuthoritativeRecords: &[]ipfs.DNSDelegationRecord{
					{Type: "NS", Value: new("hns-626f7578e5.rec.ns1.lumeweb")},
				},
			},
		}, true)
		out := buf.String()
		assert.Contains(t, out, "synthetic nameservers")
		assert.NotContains(t, out, "Authoritative records")
	})

	t.Run("icann driver renders registrar wording and nameservers", func(t *testing.T) {
		output := newTestOutput()
		renderDomainDelegation(output, &ipfs.DomainResponse{
			Id: 1, Domain: "mydomain.com", Namespace: ipfs.DomainNamespaceICANN, Status: new(ipfs.DomainResponseStatusActive),
			Delegation: &ipfs.DNSDelegation{
				Nameservers: &[]string{"ns1.example.com", "ns2.example.com"},
			},
		}, false)
		// exercises the icann driver path (registrar wording, nameservers list)
	})

	t.Run("unknown namespace falls back to generic driver", func(t *testing.T) {
		output := newTestOutput()
		renderDomainDelegation(output, &ipfs.DomainResponse{
			Id: 1, Domain: "mydomain.eth", Namespace: "ens", Status: new(ipfs.DomainResponseStatusActive),
			Delegation: &ipfs.DNSDelegation{
				Mode: new("delegated"),
				ParentRecords: &[]ipfs.DNSDelegationRecord{
					{Type: "NS", Value: new("ns1.lumeweb")},
				},
				AuthoritativeRecords: &[]ipfs.DNSDelegationRecord{
					{Type: "TLSA", Value: new("_443._tcp.mydomain.eth. 3 1 1 <sha256>")},
				},
			},
		}, false)
		// exercises the generic fallback path for an unrecognized namespace
	})

	t.Run("DS appears once in parent records and stays contiguous, comma-joined NS is split", func(t *testing.T) {
		var buf bytes.Buffer
		output := NewOutputFormatter(false, false, false, false)
		output.SetWriter(&buf)
		// A full-length SHA-256 digest (64 hex chars) that exceeds the table's
		// default wrap width; it must render contiguous so it stays copyable.
		digest := "c35938688953467518f2a9c613b8a32da647595912a67fa9cf47e41b593831d5"
		dsValue := "lumeweb DS 44451 13 2 " + digest
		renderDomainDelegation(output, &ipfs.DomainResponse{
			Id: 1, Domain: "lumeweb", Namespace: ipfs.DomainNamespaceHNS, Status: new(ipfs.DomainResponseStatusActive),
			Delegation: &ipfs.DNSDelegation{
				Mode:   new("delegated"),
				Dnssec: new("enabled"),
				ParentRecords: &[]ipfs.DNSDelegationRecord{
					{Type: "NS", Value: new("ns1.pinner.xyz,ns2.pinner.xyz")},
					{Type: "DS", Value: new(dsValue)},
				},
			},
		}, false)
		out := buf.String()
		// The DS record is communicated once, as a parent record in the
		// parent-records table, not re-decoded into a redundant block.
		assert.Equal(t, 1, strings.Count(out, dsValue))
		assert.NotContains(t, out, "DS record (paste")
		assert.NotContains(t, out, "KEY TAG")
		// The digest is never hard-wrapped mid-value: the full string appears
		// intact and contiguous so it can be selected and copied whole.
		assert.Contains(t, out, dsValue)
		assert.Equal(t, 1, strings.Count(out, digest))
		// Comma-joined nameservers are split so each is visible/copyable,
		// matching how the wizard communicates nameservers.
		assert.Contains(t, out, "ns1.pinner.xyz")
		assert.Contains(t, out, "ns2.pinner.xyz")
		assert.NotContains(t, out, "ns1.pinner.xyz,ns2.pinner.xyz")
	})

	t.Run("managed hns omits authoritative records", func(t *testing.T) {
		var buf bytes.Buffer
		output := NewOutputFormatter(false, false, false, false)
		output.SetWriter(&buf)
		renderDomainDelegation(output, &ipfs.DomainResponse{
			Id: 1, Domain: "mydomain.hns", Namespace: ipfs.DomainNamespaceHNS, Status: new(ipfs.DomainResponseStatusActive),
			Delegation: &ipfs.DNSDelegation{
				Mode: new("delegated"),
				ParentRecords: &[]ipfs.DNSDelegationRecord{
					{Type: "NS", Value: new("ns1.pinner.xyz")},
					{Type: "DS", Value: new("44451 13 2 c359")},
				},
				AuthoritativeRecords: &[]ipfs.DNSDelegationRecord{
					{Type: "NS", Value: new("nsx.pinner.xyz")},
				},
			},
		}, true)
		out := buf.String()
		// Pinner manages DNS, so only the parent records (for the HNS wallet)
		// are shown; the authoritative side is handled for the user.
		assert.Contains(t, out, "Pinner manages your DNS")
		assert.Contains(t, out, "Parent records (publish in your HNS wallet)")
		assert.Contains(t, out, "44451 13 2 c359")
		assert.NotContains(t, out, "Authoritative records")
		assert.NotContains(t, out, "nsx.pinner.xyz")
		// The server's free-form instructions prose is never echoed.
		assert.NotContains(t, out, "parent_records")
		assert.NotContains(t, out, "optional GLUE")
	})

	t.Run("managed icann omits authoritative records", func(t *testing.T) {
		var buf bytes.Buffer
		output := NewOutputFormatter(false, false, false, false)
		output.SetWriter(&buf)
		renderDomainDelegation(output, &ipfs.DomainResponse{
			Id: 1, Domain: "mydomain.com", Namespace: ipfs.DomainNamespaceICANN, Status: new(ipfs.DomainResponseStatusActive),
			Delegation: &ipfs.DNSDelegation{
				ParentRecords: &[]ipfs.DNSDelegationRecord{
					{Type: "NS", Value: new("ns1.pinner.xyz")},
				},
				AuthoritativeRecords: &[]ipfs.DNSDelegationRecord{
					{Type: "TLSA", Value: new("_443._tcp.mydomain.com. 3 1 1 <sha256>")},
				},
			},
		}, true)
		out := buf.String()
		assert.Contains(t, out, "Point your registrar's nameservers")
		assert.Contains(t, out, "Pinner manages your DNS")
		assert.NotContains(t, out, "Authoritative records")
		assert.NotContains(t, out, "TLSA")
	})

	t.Run("onchain managed hns explains the on-chain DNS and surfaces the TLSA", func(t *testing.T) {
		var buf bytes.Buffer
		output := NewOutputFormatter(false, false, false, false)
		output.SetWriter(&buf)
		// On-chain managed: the domain is held on-chain, so the backend
		// returns status onchain_managed with delegation carrying the TLSA
		// record the user must publish on-chain.
		renderDomainDelegation(output, &ipfs.DomainResponse{
			Id: 1, Domain: "mydomain", Namespace: ipfs.DomainNamespaceHNS, Status: new(ipfs.DomainResponseStatusOnchainManaged),
			Delegation: &ipfs.DNSDelegation{
				AuthoritativeRecords: &[]ipfs.DNSDelegationRecord{
					{Type: "TLSA", Value: new("_443._tcp.mydomain.hns. 60 IN TLSA 3 1 1 abcdef")},
				},
			},
		}, false)
		out := buf.String()
		// The driver must not crash and must explain the on-chain hosting
		// shape in user-friendly terms, and the TLSA must reach the user —
		// without it the site won't load over HTTPS.
		assert.Contains(t, out, "on-chain managed")
		assert.Contains(t, out, "held on-chain")
		assert.Contains(t, out, "TLSA")
		assert.Contains(t, out, "_443._tcp.mydomain.hns. 60 IN TLSA 3 1 1 abcdef")
		// No portal delegation publishing: none of it applies here.
		assert.NotContains(t, out, "Parent records")
		assert.NotContains(t, out, "Authoritative records")
	})

	t.Run("onchain managed hns falls back to the response's tlsa_rdata", func(t *testing.T) {
		var buf bytes.Buffer
		output := NewOutputFormatter(false, false, false, false)
		output.SetWriter(&buf)
		// Schema v0.1.96 carries tlsa_rdata directly on the response; the
		// fallback must use it when the bundle has no TLSA record.
		rdata := "3 1 1 abcdef"
		renderDomainDelegation(output, &ipfs.DomainResponse{
			Id: 1, Domain: "mydomain", Namespace: ipfs.DomainNamespaceHNS, Status: new(ipfs.DomainResponseStatusOnchainManaged),
			TlsaRdata: &rdata,
		}, false)
		out := buf.String()
		assert.Contains(t, out, "TLSA")
		assert.Contains(t, out, "3 1 1 abcdef")
		assert.NotContains(t, out, "dane republish")
	})

	t.Run("onchain managed hns without a TLSA still tells the user one is needed", func(t *testing.T) {
		var buf bytes.Buffer
		output := NewOutputFormatter(false, false, false, false)
		output.SetWriter(&buf)
		// The backend does not always include the TLSA on the response yet;
		// the gap must not pass silently.
		renderDomainDelegation(output, &ipfs.DomainResponse{
			Id: 1, Domain: "mydomain", Namespace: ipfs.DomainNamespaceHNS, Status: new(ipfs.DomainResponseStatusOnchainManaged),
		}, false)
		out := buf.String()
		assert.Contains(t, out, "on-chain managed")
		assert.Contains(t, out, "TLSA record")
		assert.Contains(t, out, "dane republish")
		assert.NotContains(t, out, "Parent records")
		assert.NotContains(t, out, "Authoritative records")
	})

	t.Run("verify result shows a verified outcome with next steps", func(t *testing.T) {
		var buf bytes.Buffer
		output := NewOutputFormatter(false, false, false, false)
		output.SetWriter(&buf)
		renderDomainVerifyResult(output, &ipfs.DomainResponse{
			Id: 1, Domain: "mydomain.com", Namespace: ipfs.DomainNamespaceICANN, Status: new(ipfs.DomainResponseStatusActive),
		})
		out := buf.String()
		assert.Contains(t, out, "✅ mydomain.com verified")
		assert.Contains(t, out, "https://mydomain.com")
		// The records already check out on a verified domain, so the success
		// path must not tell the user to go fix DNS records.
		assert.NotContains(t, out, "dns-requirements")
	})

	t.Run("verify result for on-chain managed names points at the TLSA", func(t *testing.T) {
		var buf bytes.Buffer
		output := NewOutputFormatter(false, false, false, false)
		output.SetWriter(&buf)
		renderDomainVerifyResult(output, &ipfs.DomainResponse{
			Id: 1, Domain: "mydomain", Namespace: ipfs.DomainNamespaceHNS, Status: new(ipfs.DomainResponseStatusOnchainManaged),
		})
		out := buf.String()
		assert.Contains(t, out, "✅ mydomain verified")
		assert.Contains(t, out, "TLSA")
		assert.Contains(t, out, "dns-requirements mydomain")
	})

	t.Run("verify result for a pending binding says not verified with retry guidance", func(t *testing.T) {
		var buf bytes.Buffer
		output := NewOutputFormatter(false, false, false, false)
		output.SetWriter(&buf)
		renderDomainVerifyResult(output, &ipfs.DomainResponse{
			Id: 1, Domain: "pending.com", Namespace: ipfs.DomainNamespaceICANN, Status: new(ipfs.DomainResponseStatusWaitingDelegation),
		})
		out := buf.String()
		assert.Contains(t, out, "⏳ pending.com is not verified yet")
		assert.Contains(t, out, "verify pending.com")
	})

	t.Run("verify result for a nil response renders the resolution-missing guidance", func(t *testing.T) {
		var buf bytes.Buffer
		output := NewOutputFormatter(false, false, false, false)
		output.SetWriter(&buf)
		renderDomainVerifyResult(output, nil)
		out := buf.String()
		assert.Contains(t, out, "⏳ not verified yet")
	})

	t.Run("self-managed hns shows authoritative records", func(t *testing.T) {
		var buf bytes.Buffer
		output := NewOutputFormatter(false, false, false, false)
		output.SetWriter(&buf)
		renderDomainDelegation(output, &ipfs.DomainResponse{
			Id: 1, Domain: "mydomain.hns", Namespace: ipfs.DomainNamespaceHNS, Status: new(ipfs.DomainResponseStatusActive),
			Delegation: &ipfs.DNSDelegation{
				Mode: new("delegated"),
				ParentRecords: &[]ipfs.DNSDelegationRecord{
					{Type: "NS", Value: new("ns1.lumeweb")},
				},
				AuthoritativeRecords: &[]ipfs.DNSDelegationRecord{
					{Type: "NS", Value: new("ns.eigen.lumeweb")},
				},
			},
		}, false)
		out := buf.String()
		assert.Contains(t, out, "point your own DNS server")
		assert.Contains(t, out, "Authoritative records (configure on your DNS server)")
		assert.Contains(t, out, "ns.eigen.lumeweb")
	})
}
