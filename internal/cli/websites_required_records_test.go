package cli

import (
	"testing"

	"github.com/stretchr/testify/require"
	ipfs "go.lumeweb.com/ipfs-sdk"
)

func TestBuildRequiredRecords(t *testing.T) {
	tests := []struct {
		name        string
		website     *ipfs.WebsiteItem
		nameservers []string
		want        []map[string]string
	}{
		{
			name:        "nil website returns nil",
			website:     nil,
			nameservers: []string{"ns1.example.com"},
			want:        nil,
		},
		{
			name: "DNS hosting enabled with two nameservers",
			website: &ipfs.WebsiteItem{
				Domain:            "example.com",
				DnsHostingEnabled: true,
			},
			nameservers: []string{"ns1.pinner.xyz", "ns2.pinner.xyz"},
			want: []map[string]string{
				{"name": "example.com", "type": "NS", "value": "ns1.pinner.xyz"},
				{"name": "example.com", "type": "NS", "value": "ns2.pinner.xyz"},
			},
		},
		{
			name: "DNS hosting enabled with empty nameservers",
			website: &ipfs.WebsiteItem{
				Domain:            "example.com",
				DnsHostingEnabled: true,
			},
			nameservers: []string{},
			want:        []map[string]string{},
		},
		{
			name: "self-managed DNS with all fields present",
			website: &ipfs.WebsiteItem{
				Domain:            "example.com",
				DnsHostingEnabled: false,
				ValidationToken:   "token123",
				TargetType:        "ipfs",
				TargetHash:        "QmXxx",
				GatewayDomain:     strPtr("gateway.pinner.xyz"),
			},
			want: []map[string]string{
				{"name": "example.com", "type": "TXT", "value": "token123"},
				{"name": "_dnslink.example.com", "type": "TXT", "value": "dnslink=/ipfs/QmXxx"},
				{"name": "example.com", "type": "CNAME", "value": "gateway.pinner.xyz"},
			},
		},
		{
			name: "self-managed DNS without gateway domain",
			website: &ipfs.WebsiteItem{
				Domain:            "example.com",
				DnsHostingEnabled: false,
				ValidationToken:   "token123",
				TargetType:        "ipfs",
				TargetHash:        "QmXxx",
			},
			want: []map[string]string{
				{"name": "example.com", "type": "TXT", "value": "token123"},
				{"name": "_dnslink.example.com", "type": "TXT", "value": "dnslink=/ipfs/QmXxx"},
			},
		},
		{
			name: "self-managed DNS with custom validation host",
			website: &ipfs.WebsiteItem{
				Domain:               "example.com",
				DnsHostingEnabled:    false,
				ValidationToken:      "token123",
				ValidationRecordHost: strPtr("_validation.example.com"),
				TargetType:           "ipfs",
				TargetHash:           "QmXxx",
			},
			want: []map[string]string{
				{"name": "_validation.example.com", "type": "TXT", "value": "token123"},
				{"name": "_dnslink.example.com", "type": "TXT", "value": "dnslink=/ipfs/QmXxx"},
			},
		},
		{
			name: "self-managed DNS with IPNS target type",
			website: &ipfs.WebsiteItem{
				Domain:            "example.com",
				DnsHostingEnabled: false,
				ValidationToken:   "token123",
				TargetType:        "ipns",
				TargetHash:        "k51qzi5uqu5dg4vh...",
			},
			want: []map[string]string{
				{"name": "example.com", "type": "TXT", "value": "token123"},
				{"name": "_dnslink.example.com", "type": "TXT", "value": "dnslink=/ipns/k51qzi5uqu5dg4vh..."},
			},
		},
		{
			name: "self-managed DNS with empty gateway domain pointer",
			website: &ipfs.WebsiteItem{
				Domain:            "example.com",
				DnsHostingEnabled: false,
				ValidationToken:   "token123",
				TargetType:        "ipfs",
				TargetHash:        "QmXxx",
				GatewayDomain:     strPtr(""),
			},
			want: []map[string]string{
				{"name": "example.com", "type": "TXT", "value": "token123"},
				{"name": "_dnslink.example.com", "type": "TXT", "value": "dnslink=/ipfs/QmXxx"},
			},
		},
		{
			name: "self-managed DNS with empty validation record host pointer",
			website: &ipfs.WebsiteItem{
				Domain:               "example.com",
				DnsHostingEnabled:    false,
				ValidationToken:      "token123",
				ValidationRecordHost: strPtr(""),
				TargetType:           "ipfs",
				TargetHash:           "QmXxx",
			},
			want: []map[string]string{
				{"name": "example.com", "type": "TXT", "value": "token123"},
				{"name": "_dnslink.example.com", "type": "TXT", "value": "dnslink=/ipfs/QmXxx"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildRequiredRecords(tt.website, tt.nameservers)
			require.Equal(t, tt.want, got)
		})
	}
}

func strPtr(s string) *string {
	return &s
}
