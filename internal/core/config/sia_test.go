package config

import (
	"testing"
)

func TestGetSiaIndexerURL(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
		secure   bool
		want     string
	}{
		{"default", "", true, "https://sia.pinner.xyz"},
		{"custom domain", "pinner.xyz", true, "https://sia.pinner.xyz"},
		{"explicit https", "https://pinner.xyz", true, "https://sia.pinner.xyz"},
		{"insecure", "pinner.xyz", false, "http://sia.pinner.xyz"},
		{"localhost", "localhost:8080", false, "http://sia.localhost:8080"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				BaseEndpoint: tt.endpoint,
				Secure:       tt.secure,
			}
			got := cfg.GetSiaIndexerURL()
			if got != tt.want {
				t.Fatalf("GetSiaIndexerURL() = %q, want %q", got, tt.want)
			}
		})
	}
}
