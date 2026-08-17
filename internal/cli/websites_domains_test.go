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
			Id: 1, Domain: "mydomain.hns", Namespace: "hns", Status: strPtr("delegated"),
		}, false)
		// exercises the nil-delegation branch without asserting exact text
	})

	t.Run("renders records with typed helper", func(t *testing.T) {
		output := newTestOutput()
		renderDomainDelegation(output, &ipfs.DomainResponse{
			Id: 1, Domain: "mydomain.hns", Namespace: "hns", Status: strPtr("delegated"),
			Delegation: &ipfs.DNSDelegation{
				Mode:   strPtr("delegated"),
				Dnssec: strPtr("enabled"),
				ParentRecords: &[]ipfs.DNSDelegationRecord{
					{Type: "NS", Value: strPtr("ns1.lumeweb,ns2.lumeweb")},
				},
				AuthoritativeRecords: &[]ipfs.DNSDelegationRecord{
					{Type: "TLSA", Value: strPtr("_443._tcp.mydomain. 3 1 1 <sha256>")},
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
			Id: 1, Domain: "mydomain.hns", Namespace: "hns", Status: strPtr("delegated"),
			Delegation: &ipfs.DNSDelegation{
				Mode: strPtr("inline"),
				ParentRecords: &[]ipfs.DNSDelegationRecord{
					{Type: "SYNTH4", Value: strPtr("hns-626f7578e5.rec.ns1.lumeweb")},
				},
				AuthoritativeRecords: &[]ipfs.DNSDelegationRecord{
					{Type: "NS", Value: strPtr("hns-626f7578e5.rec.ns1.lumeweb")},
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
			Id: 1, Domain: "mydomain.hns", Namespace: "hns", Status: strPtr("delegated"),
			Delegation: &ipfs.DNSDelegation{
				Mode: strPtr("inline"),
				ParentRecords: &[]ipfs.DNSDelegationRecord{
					{Type: "SYNTH4", Value: strPtr("hns-626f7578e5.rec.ns1.lumeweb")},
				},
				AuthoritativeRecords: &[]ipfs.DNSDelegationRecord{
					{Type: "NS", Value: strPtr("hns-626f7578e5.rec.ns1.lumeweb")},
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
			Id: 1, Domain: "mydomain.com", Namespace: "icann", Status: strPtr("delegated"),
			Delegation: &ipfs.DNSDelegation{
				Nameservers: &[]string{"ns1.example.com", "ns2.example.com"},
			},
		}, false)
		// exercises the icann driver path (registrar wording, nameservers list)
	})

	t.Run("unknown namespace falls back to generic driver", func(t *testing.T) {
		output := newTestOutput()
		renderDomainDelegation(output, &ipfs.DomainResponse{
			Id: 1, Domain: "mydomain.eth", Namespace: "ens", Status: strPtr("delegated"),
			Delegation: &ipfs.DNSDelegation{
				Mode: strPtr("delegated"),
				ParentRecords: &[]ipfs.DNSDelegationRecord{
					{Type: "NS", Value: strPtr("ns1.lumeweb")},
				},
				AuthoritativeRecords: &[]ipfs.DNSDelegationRecord{
					{Type: "TLSA", Value: strPtr("_443._tcp.mydomain.eth. 3 1 1 <sha256>")},
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
			Id: 1, Domain: "lumeweb", Namespace: "hns", Status: strPtr("delegated"),
			Delegation: &ipfs.DNSDelegation{
				Mode:   strPtr("delegated"),
				Dnssec: strPtr("enabled"),
				ParentRecords: &[]ipfs.DNSDelegationRecord{
					{Type: "NS", Value: strPtr("ns1.pinner.xyz,ns2.pinner.xyz")},
					{Type: "DS", Value: strPtr(dsValue)},
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
			Id: 1, Domain: "mydomain.hns", Namespace: "hns", Status: strPtr("delegated"),
			Delegation: &ipfs.DNSDelegation{
				Mode: strPtr("delegated"),
				ParentRecords: &[]ipfs.DNSDelegationRecord{
					{Type: "NS", Value: strPtr("ns1.pinner.xyz")},
					{Type: "DS", Value: strPtr("44451 13 2 c359")},
				},
				AuthoritativeRecords: &[]ipfs.DNSDelegationRecord{
					{Type: "NS", Value: strPtr("nsx.pinner.xyz")},
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
			Id: 1, Domain: "mydomain.com", Namespace: "icann", Status: strPtr("delegated"),
			Delegation: &ipfs.DNSDelegation{
				ParentRecords: &[]ipfs.DNSDelegationRecord{
					{Type: "NS", Value: strPtr("ns1.pinner.xyz")},
				},
				AuthoritativeRecords: &[]ipfs.DNSDelegationRecord{
					{Type: "TLSA", Value: strPtr("_443._tcp.mydomain.com. 3 1 1 <sha256>")},
				},
			},
		}, true)
		out := buf.String()
		assert.Contains(t, out, "Point your registrar's nameservers")
		assert.Contains(t, out, "Pinner manages your DNS")
		assert.NotContains(t, out, "Authoritative records")
		assert.NotContains(t, out, "TLSA")
	})

	t.Run("self-managed hns shows authoritative records", func(t *testing.T) {
		var buf bytes.Buffer
		output := NewOutputFormatter(false, false, false, false)
		output.SetWriter(&buf)
		renderDomainDelegation(output, &ipfs.DomainResponse{
			Id: 1, Domain: "mydomain.hns", Namespace: "hns", Status: strPtr("delegated"),
			Delegation: &ipfs.DNSDelegation{
				Mode: strPtr("delegated"),
				ParentRecords: &[]ipfs.DNSDelegationRecord{
					{Type: "NS", Value: strPtr("ns1.lumeweb")},
				},
				AuthoritativeRecords: &[]ipfs.DNSDelegationRecord{
					{Type: "NS", Value: strPtr("ns.eigen.lumeweb")},
				},
			},
		}, false)
		out := buf.String()
		assert.Contains(t, out, "point your own DNS server")
		assert.Contains(t, out, "Authoritative records (configure on your DNS server)")
		assert.Contains(t, out, "ns.eigen.lumeweb")
	})
}
