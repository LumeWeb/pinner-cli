package mcp

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// ResolveCredential returns the first non-empty value produced by the given
// source providers, applied in order. Each provider is a thunk so a caller can
// model an ordered detection chain (flag -> env -> provider config file ->
// config manager) without evaluating sources that are more costly or more
// privileged than needed. It mirrors the precedence used by the SDKs and keeps
// a single definition of "what is the value" across runtime, wizard, and
// service install.
//
// A provider returning a non-empty string stops the chain; the value is
// returned already trimmed.
func ResolveCredential(providers ...func() string) string {
	for _, p := range providers {
		if p == nil {
			continue
		}
		if v := strings.TrimSpace(p()); v != "" {
			return v
		}
	}
	return ""
}

// hasProviderConfig reports whether the named tunnel provider has a config file
// that the provider (or its SDK) reads on startup to authenticate. We only
// probe for the file's existence -- the provider performs the actual parsing --
// so this is the existence signal used to decide whether a token prompt is
// needed.
//
// Currently only ngrok has a provider-owned config file in this tree. It honors
// NGROK_CONFIG when set; otherwise it uses the per-OS default:
//
//	Windows: %LocalAppData%\ngrok\ngrok.yml   (NOT %AppData%\Roaming)
//	macOS:   ~/Library/Application Support/ngrok/ngrok.yml
//	Linux:   ~/.config/ngrok/ngrok.yml
//
// The Windows LOCALAPPDATA (not the os.UserConfigDir Roaming path) is what
// `ngrok config add-authtoken` actually writes, so it must be probed to match.
func hasProviderConfig(provider string) bool {
	if provider != "ngrok" {
		return false
	}
	if path := os.Getenv("NGROK_CONFIG"); path != "" {
		_, err := os.Stat(path)
		return err == nil
	}
	switch runtime.GOOS {
	case "windows":
		if base := os.Getenv("LOCALAPPDATA"); base != "" {
			_, err := os.Stat(filepath.Join(base, "ngrok", "ngrok.yml"))
			return err == nil
		}
		return false
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return false
		}
		_, err = os.Stat(filepath.Join(home, "Library", "Application Support", "ngrok", "ngrok.yml"))
		return err == nil
	default:
		home, err := os.UserHomeDir()
		if err != nil {
			return false
		}
		_, err = os.Stat(filepath.Join(home, ".config", "ngrok", "ngrok.yml"))
		return err == nil
	}
}
