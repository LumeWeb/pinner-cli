package config

import "testing"

func TestConfigGetMaxMCPUploadSize(t *testing.T) {
	t.Run("default is 1 GiB", func(t *testing.T) {
		if got := NewConfig().GetMaxMCPUploadSize(); got != 1<<30 {
			t.Fatalf("expected default max MCP upload size of 1 GiB, got %d", got)
		}
	})

	t.Run("explicit value honored", func(t *testing.T) {
		c := &Config{MaxMCPUploadSize: 5 << 20}
		if got := c.GetMaxMCPUploadSize(); got != 5<<20 {
			t.Fatalf("expected 5 MiB, got %d", got)
		}
	})
}
