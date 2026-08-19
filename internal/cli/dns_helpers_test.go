package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsValidIPv4(t *testing.T) {
	tests := []struct {
		ip       string
		expected bool
	}{
		{"192.168.1.1", true},
		{"0.0.0.0", true},
		{"255.255.255.255", true},
		{"10.0.0.1", true},
		{"::1", false},
		{"2001:db8::1", false},
		{"not-an-ip", false},
		{"", false},
		{"256.1.1.1", false},
	}

	for _, tt := range tests {
		t.Run(tt.ip, func(t *testing.T) {
			assert.Equal(t, tt.expected, isValidIPv4(tt.ip))
		})
	}
}

func TestIsValidIPv6(t *testing.T) {
	tests := []struct {
		ip       string
		expected bool
	}{
		{"::1", true},
		{"2001:db8::1", true},
		{"fe80::1", true},
		{"192.168.1.1", false},
		{"not-an-ip", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.ip, func(t *testing.T) {
			assert.Equal(t, tt.expected, isValidIPv6(tt.ip))
		})
	}
}

func TestIsValidDomain(t *testing.T) {
	tests := []struct {
		domain   string
		expected bool
	}{
		{"example.com", true},
		{"example.com.", true}, // trailing dot = absolute FQDN, valid
		{"sub.example.com", true},
		{"sub.example.com.", true},
		{"a.b.c.d", true},
		{"", false},
		{"single", false},
		{".com", false},
		{"example.", false}, // bare label + dot, not a valid FQDN
		{".", false},        // bare root, not valid record content
	}

	for _, tt := range tests {
		t.Run(tt.domain, func(t *testing.T) {
			assert.Equal(t, tt.expected, isValidDomain(tt.domain))
		})
	}
}

func TestValidateDomain(t *testing.T) {
	t.Run("valid domain", func(t *testing.T) {
		err := validateDomain("example.com")
		require.NoError(t, err)
	})

	t.Run("empty domain", func(t *testing.T) {
		err := validateDomain("")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cannot be empty")
	})
}

func TestValidateDNSRecord(t *testing.T) {
	tests := []struct {
		name     string
		rtype    string
		content  string
		wantErr  bool
		errMatch string
	}{
		{"valid A record", "A", "1.2.3.4", false, ""},
		{"invalid A record", "A", "not-an-ip", true, "invalid IPv4"},
		{"valid AAAA record", "AAAA", "::1", false, ""},
		{"invalid AAAA record", "AAAA", "1.2.3.4", true, "invalid IPv6"},
		{"valid CNAME record", "CNAME", "example.com", false, ""},
		{"valid CNAME trailing dot", "CNAME", "example.com.", false, ""},
		{"invalid CNAME record", "CNAME", "single", true, "invalid domain"},
		{"valid MX record", "MX", "mail.example.com", false, ""},
		{"valid MX trailing dot", "MX", "mail.example.com.", false, ""},
		{"valid NS record", "NS", "ns1.example.com", false, ""},
		{"valid NS trailing dot", "NS", "ns1.example.com.", false, ""},
		{"valid TXT record", "TXT", "some text", false, ""},
		// TXT values are chunked by the backend (PowerDNS auto-splits RFC 1035
		// 255-octet strings), so a DKIM1/SPF-length value is valid, not an error.
		{"valid long TXT (DKIM1 length)", "TXT", string(make([]byte, 256)), false, ""},
		{"valid 1KB TXT (SPF length)", "TXT", string(make([]byte, 1024)), false, ""},
		{"TXT beyond sanity cap", "TXT", string(make([]byte, 65536)), true, "too long"},
		{"unsupported type", "BOGUS", "whatever", true, "unsupported record type"},
		{"valid SRV", "SRV", "10 60 5060 sip.example.com", false, ""},
		{"malformed SRV", "SRV", "10 60 5060", true, "SRV record content"},
		{"lowercase type accepted by validator", "a", "1.2.3.4", false, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateDNSRecord(tt.rtype, tt.content)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMatch)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestParseCommaSeparated(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{"empty string", "", nil},
		{"single value", "ns1.example.com", []string{"ns1.example.com"}},
		{"two values", "ns1.example.com,ns2.example.com", []string{"ns1.example.com", "ns2.example.com"}},
		{"with spaces", " ns1.example.com , ns2.example.com ", []string{"ns1.example.com", "ns2.example.com"}},
		{"trailing comma", "a,b,", []string{"a", "b"}},
		{"only commas", ",,,", []string{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseCommaSeparated(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestResolveZoneID_NumericArg(t *testing.T) {
	id, err := resolveZoneID(nil, nil, "42")
	require.NoError(t, err)
	assert.Equal(t, "42", id)
}
