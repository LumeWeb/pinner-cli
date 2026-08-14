package mcp

import (
	"strings"
	"testing"
)

// These tests assert on the rendered app-flow ESM module. The flow JS is not
// executed by Go tests (it runs in the host iframe), so these pin the static
// wiring the view depends on: the correct start/status tool names, the shared
// handle-presence dead-handle predicate, the start guard against a handle-less
// not-configured hand-off, and the in-flight guard. Any of these regressing
// silently breaks the browser view, so they are asserted here.

func TestAppFlowModulesWired(t *testing.T) {
	b64 := "dGVzdA==" // dummy base64; content is not parsed
	mods := map[string]string{
		"vault_create":  vaultCreateAppModule(b64),
		"vault_restore": vaultRestoreAppModule(b64),
		"auth_sso":      authSSOAppModule(b64),
	}

	wantTool := map[string]string{
		"vault_create":  "vault_create_status",
		"vault_restore": "vault_restore_status",
		"auth_sso":      "auth_sso_status",
	}
	wantStart := map[string]string{
		"vault_create":  "vault_create",
		"vault_restore": "vault_restore",
		"auth_sso":      "auth_sso",
	}
	wantBtn := map[string]string{
		"vault_create":  "vault-create-start",
		"vault_restore": "vault-restore-start",
		"auth_sso":      "sso-start",
	}

	for name, mod := range mods {
		t.Run(name, func(t *testing.T) {
			// Client base64 injected and bootstrap pulled in.
			if !strings.Contains(mod, `CLIENT_B64 = "`) {
				t.Fatalf("module missing CLIENT_B64 injection")
			}
			// Start + status tools wired.
			if !strings.Contains(mod, `name: "`+wantStart[name]+`"`) {
				t.Fatalf("missing start tool %q", wantStart[name])
			}
			if !strings.Contains(mod, `name: "`+wantTool[name]+`"`) {
				t.Fatalf("missing status tool %q", wantTool[name])
			}
			// Start button bound.
			if !strings.Contains(mod, `$("#`+wantBtn[name]+`")`) {
				t.Fatalf("missing start button %q", wantBtn[name])
			}
			// Shared guards that every flow must have.
			for _, guard := range []string{
				// in-flight guard: no concurrent runs
				`if (startBtn.disabled) return;`,
				// start guard: stop when the hand-off carries no handle
				`if (!sc.handle) {`,
				// dead-handle predicate: stop polling when the handle is gone
				`status === "needs_human" && !sc.handle`,
			} {
				if !strings.Contains(mod, guard) {
					t.Fatalf("module missing guard %q", guard)
				}
			}
		})
	}
}

// TestAppFlowModulesDistinctToolNames ensures the three apps are not collapsed
// onto one another's status tool (a config mix-up would break two flows).
func TestAppFlowModulesDistinctToolNames(t *testing.T) {
	b64 := "dGVzdA=="
	for _, own := range []string{"vault_create_status", "vault_restore_status", "auth_sso_status"} {
		for _, foreign := range []string{"vault_create_status", "vault_restore_status", "auth_sso_status"} {
			if own == foreign {
				continue
			}
			var mod string
			switch {
			case strings.HasPrefix(own, "vault_create"):
				mod = vaultCreateAppModule(b64)
			case strings.HasPrefix(own, "vault_restore"):
				mod = vaultRestoreAppModule(b64)
			default:
				mod = authSSOAppModule(b64)
			}
			if strings.Contains(mod, `name: "`+foreign+`"`) {
				t.Fatalf("module %q mistakenly references foreign status tool %q", own, foreign)
			}
		}
	}
}
