package cli

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDNSSelfServiceGuidance(t *testing.T) {
	tests := []struct {
		name      string
		msg       string
		wantLines int
		contains  []string
	}{
		{
			name:      "dnssec error",
			msg:       "enable dnssec for zone 42: no active signing key",
			wantLines: 3,
			contains:  []string{"self-heals DNSSEC"},
		},
		{
			name:      "propagation not yet live",
			msg:       "verify website domain failed with status 500: DNS validation failed: parent NS/DS not live",
			wantLines: 3,
			contains:  []string{"have not propagated"},
		},
		{
			name:      "resolver unreachable",
			msg:       "HNS resolver not configured (DnsConfig.HNSResolver)",
			wantLines: 2,
			contains:  []string{"DNS resolver could not be reached"},
		},
		{
			name:      "no matching guidance",
			msg:       "some unrelated failure",
			wantLines: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := dnsGuidance(tt.msg)
			require.Len(t, got, tt.wantLines)
			for _, c := range tt.contains {
				found := false
				for _, line := range got {
					if strings.Contains(strings.ToLower(line), strings.ToLower(c)) {
						found = true
					}
				}
				assert.True(t, found, "guidance should contain %q", c)
			}
		})
	}
}
