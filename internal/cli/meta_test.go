package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseMetaPairs(t *testing.T) {
	tests := []struct {
		name        string
		input       []string
		want        map[string]string
		wantErr     bool
		errContains string
	}{
		{
			name:  "valid single pair",
			input: []string{"owner=alice"},
			want:  map[string]string{"owner": "alice"},
		},
		{
			name:  "valid multiple pairs",
			input: []string{"owner=alice", "env=prod"},
			want:  map[string]string{"owner": "alice", "env": "prod"},
		},
		{
			name:  "value with equals sign",
			input: []string{"url=https://example.com?a=1"},
			want:  map[string]string{"url": "https://example.com?a=1"},
		},
		{
			name:  "empty value",
			input: []string{"key="},
			want:  map[string]string{"key": ""},
		},
		{
			name:        "missing equals sign",
			input:       []string{"noequals"},
			wantErr:     true,
			errContains: "expected key=value format",
		},
		{
			name:        "empty key",
			input:       []string{"=value"},
			wantErr:     true,
			errContains: "expected key=value format",
		},
		{
			name:  "empty input",
			input: []string{},
			want:  map[string]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseMetaPairs(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestMetaMapToSlice(t *testing.T) {
	result := metaMapToSlice(map[string]string{"owner": "alice", "env": "prod"})
	require.Len(t, result, 4)

	m := map[string]string{}
	for i := 0; i < len(result); i += 2 {
		m[result[i]] = result[i+1]
	}
	assert.Equal(t, "alice", m["owner"])
	assert.Equal(t, "prod", m["env"])
}
