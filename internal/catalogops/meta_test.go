package catalogops

import (
	"reflect"
	"testing"
)

func TestSplitMetaPairs(t *testing.T) {
	tests := []struct {
		name    string
		in      []string
		want    []string
		wantErr bool
	}{
		{
			name: "single pair",
			in:   []string{"a=1"},
			want: []string{"a", "1"},
		},
		{
			name: "multiple pairs in input order",
			in:   []string{"alpha=1", "beta=two", "gamma=true"},
			want: []string{"alpha", "1", "beta", "two", "gamma", "true"},
		},
		{
			name: "value contains equals sign",
			in:   []string{"url=https://example.com/x=1"},
			want: []string{"url", "https://example.com/x=1"},
		},
		{
			name: "empty value allowed",
			in:   []string{"flag="},
			want: []string{"flag", ""},
		},
		{
			name: "empty input",
			in:   nil,
			want: nil,
		},
		{
			name:    "missing equals sign",
			in:      []string{"nokey"},
			wantErr: true,
		},
		{
			name:    "empty key",
			in:      []string{"=value"},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := splitMetaPairs(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("splitMetaPairs(%v): expected error, got nil", tt.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("splitMetaPairs(%v): unexpected error: %v", tt.in, err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("splitMetaPairs(%v) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}
