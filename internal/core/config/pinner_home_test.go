package config

import (
	"os"
	"path/filepath"
	"testing"
)

// withEnv runs fn with env set, restoring the prior value afterward.
func withEnv(t *testing.T, key, value string, fn func()) {
	t.Helper()
	old, had := os.LookupEnv(key)
	if err := os.Setenv(key, value); err != nil {
		t.Fatalf("setenv %s: %v", key, err)
	}
	t.Cleanup(func() {
		if had {
			_ = os.Setenv(key, old)
		} else {
			_ = os.Unsetenv(key)
		}
	})
	fn()
}

func TestPinnerHome_DirResolution(t *testing.T) {
	const home = "/data/pinner"

	t.Run("config file path under PINNER_HOME", func(t *testing.T) {
		withEnv(t, PinnerHomeEnv, home, func() {
			got := resolveDefaultConfigPath()
			want := filepath.Join(home, "pinner", "config.yaml")
			if got != want {
				t.Fatalf("resolveDefaultConfigPath() = %q, want %q", got, want)
			}
		})
	})

	t.Run("config dir under PINNER_HOME", func(t *testing.T) {
		withEnv(t, PinnerHomeEnv, home, func() {
			got := PinnerConfigDir()
			want := filepath.Join(home, "pinner")
			if got != want {
				t.Fatalf("PinnerConfigDir() = %q, want %q", got, want)
			}
		})
	})

	t.Run("data dir under PINNER_HOME", func(t *testing.T) {
		withEnv(t, PinnerHomeEnv, home, func() {
			got := PinnerDataDir()
			want := filepath.Join(home, "pinner", "vaults")
			if got != want {
				t.Fatalf("PinnerDataDir() = %q, want %q", got, want)
			}
		})
	})

	t.Run("unset PINNER_HOME returns empty", func(t *testing.T) {
		withEnv(t, PinnerHomeEnv, "", func() {
			if got := PinnerHome(); got != "" {
				t.Fatalf("PinnerHome() = %q, want empty", got)
			}
		})
	})
}
