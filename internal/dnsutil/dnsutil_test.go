package dnsutil

import (
	"strings"
	"testing"
)

func TestIsValidIPv4(t *testing.T) {
	valid := []string{"192.168.1.1", "0.0.0.0", "255.255.255.255", "10.0.0.1"}
	invalid := []string{"::1", "2001:db8::1", "not-an-ip", "", "256.1.1.1"}
	for _, ip := range valid {
		if !IsValidIPv4(ip) {
			t.Errorf("IsValidIPv4(%q) = false, want true", ip)
		}
	}
	for _, ip := range invalid {
		if IsValidIPv4(ip) {
			t.Errorf("IsValidIPv4(%q) = true, want false", ip)
		}
	}
}

func TestIsValidIPv6(t *testing.T) {
	valid := []string{"::1", "2001:db8::1", "fe80::1"}
	invalid := []string{"192.168.1.1", "not-an-ip", ""}
	for _, ip := range valid {
		if !IsValidIPv6(ip) {
			t.Errorf("IsValidIPv6(%q) = false, want true", ip)
		}
	}
	for _, ip := range invalid {
		if IsValidIPv6(ip) {
			t.Errorf("IsValidIPv6(%q) = true, want false", ip)
		}
	}
}

func TestIsValidDomain(t *testing.T) {
	valid := []string{"example.com", "example.com.", "sub.example.com", "a.b.c.d"}
	invalid := []string{"", "single", ".com", "example.", "."}
	for _, d := range valid {
		if !IsValidDomain(d) {
			t.Errorf("IsValidDomain(%q) = false, want true", d)
		}
	}
	for _, d := range invalid {
		if IsValidDomain(d) {
			t.Errorf("IsValidDomain(%q) = true, want false", d)
		}
	}
}

func TestValidateDNSRecordExtendedAndBase(t *testing.T) {
	valid := []struct{ typ, content string }{
		{"A", "1.2.3.4"},
		{"AAAA", "::1"},
		{"CNAME", "example.com"},
		{"MX", "mail.example.com."},
		{"MX", "10 mail.example.com."}, // priority + host form
		{"MX", "0 mail.example.com"},   // priority 0 is valid
		{"NS", "ns1.example.com."},
		{"TXT", "some text"},
		{"PTR", "host.example.com"},
		{"SRV", "10 60 5060 sip.example.com"},
		{"CAA", "0 issue letsencrypt.org"},
		{"CAA", "0 issue"}, // RFC 8659 empty-value form: blocks all issuance
		{"SOA", "ns1.example.com hostmaster.example.com 2024010101 7200 3600 1209600 3600"},
		{"a", "1.2.3.4"}, // validator accepts lowercase (dispatch uppercases elsewhere)
		// TXT values are chunked by the backend, so DKIM1/SPF-length values are valid.
		{"TXT", string(make([]byte, 256))},
		{"TXT", string(make([]byte, 1024))},
	}
	for _, tc := range valid {
		if err := ValidateDNSRecord(tc.typ, tc.content); err != nil {
			t.Errorf("ValidateDNSRecord(%s, %q) unexpected error: %v", tc.typ, tc.content, err)
		}
	}

	invalid := []struct{ typ, content string }{
		{"A", "not-an-ip"},
		{"AAAA", "1.2.3.4"},
		{"CNAME", "single"},
		{"MX", "abc 10 mail.example.com"}, // too many fields
		{"MX", "notanumber mail.example.com"}, // non-integer priority
		{"MX", "70000 mail.example.com"},      // priority out of range
		{"MX", "10 not-a-domain"},             // invalid host
		{"TXT", string(make([]byte, 65536))},  // beyond the sanity cap
		{"SRV", "10 60 5060"},
		{"CAA", "issue"}, // missing flags
		{"CAA", "0 bogustag example.com"},
		{"SOA", "ns1.example.com hostmaster.example.com 1"},
		{"BOGUS", "whatever"},
	}
	for _, tc := range invalid {
		if err := ValidateDNSRecord(tc.typ, tc.content); err == nil {
			t.Errorf("ValidateDNSRecord(%s, %q) expected error, got nil", tc.typ, tc.content)
		}
	}
}

func TestNormalizeMXContent(t *testing.T) {
	p := func(v int) *int { return &v }
	cases := []struct {
		name    string
		in      string
		prio    *int
		want    string
	}{
		{"bare host gets default priority", "mail.example.com", nil, "10 mail.example.com"},
		{"bare host with trailing dot kept", "mail.example.com.", nil, "10 mail.example.com."},
		{"existing priority is preserved", "10 mail.example.com", nil, "10 mail.example.com"},
		{"non-default priority preserved", "20 mail.example.com", nil, "20 mail.example.com"},
		{"priority zero preserved", "0 mail.example.com", nil, "0 mail.example.com"},
		{"explicit flag overrides embedded", "10 mail.example.com", p(30), "30 mail.example.com"},
		{"explicit flag applies to bare host", "mail.example.com", p(25), "25 mail.example.com"},
		{"explicit priority zero is honored", "mail.example.com", p(0), "0 mail.example.com"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := NormalizeMXContent(tc.in, tc.prio); got != tc.want {
				t.Errorf("NormalizeMXContent(%q, %v) = %q, want %q", tc.in, tc.prio, got, tc.want)
			}
		})
	}
}

func TestParseCommaSeparated(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"ns1.example.com", []string{"ns1.example.com"}},
		{"a,b", []string{"a", "b"}},
		{" a , b ", []string{"a", "b"}},
		{"a,b,", []string{"a", "b"}},
		{",,,", []string{}},
	}
	for _, c := range cases {
		got := ParseCommaSeparated(c.in)
		if len(got) != len(c.want) {
			t.Errorf("ParseCommaSeparated(%q) = %v, want %v", c.in, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("ParseCommaSeparated(%q) = %v, want %v", c.in, got, c.want)
				break
			}
		}
	}
}

func TestValidateDNSRecordName(t *testing.T) {
	if err := ValidateDNSRecordName("www"); err != nil {
		t.Errorf("ValidateDNSRecordName(www) unexpected error: %v", err)
	}
	if err := ValidateDNSRecordName(""); err != nil {
		t.Errorf("ValidateDNSRecordName(empty) unexpected error: %v", err)
	}
	if err := ValidateDNSRecordName("@"); err != nil {
		t.Errorf("ValidateDNSRecordName(@) unexpected error: %v", err)
	}
	if err := ValidateDNSRecordName("a@b"); err == nil || !strings.Contains(err.Error(), "@") {
		t.Errorf("ValidateDNSRecordName(a@b) expected error mentioning @, got %v", err)
	}
}
