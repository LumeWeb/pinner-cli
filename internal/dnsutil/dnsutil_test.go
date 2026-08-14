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
		{"NS", "ns1.example.com."},
		{"TXT", "some text"},
		{"PTR", "host.example.com"},
		{"SRV", "10 60 5060 sip.example.com"},
		{"CAA", "0 issue letsencrypt.org"},
		{"CAA", "0 issue"}, // RFC 8659 empty-value form: blocks all issuance
		{"SOA", "ns1.example.com hostmaster.example.com 2024010101 7200 3600 1209600 3600"},
		{"a", "1.2.3.4"}, // lowercase type is normalized
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
		{"TXT", string(make([]byte, 256))},
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
