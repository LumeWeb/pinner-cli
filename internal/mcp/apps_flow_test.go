package mcp

import (
	"go.lumeweb.com/pinner-cli/internal/mcp/auth"
	"strings"
	"testing"
)

// These tests verify the served MCP App documents are wired end-to-end: the
// HTML shell from the .templ bodies plus the self-contained ESM bundle built by
// the JS toolchain (packages/apps) and embedded via mcpapp.AppModule.
//
// The app JS behavioral logic (in-flight guard, handle-presence dead-handle
// predicate, start guard, distinct per-app tool names) is tested by the
// packages/apps vitest suite against the real TS source — not by re-asserting
// rendered JS strings here. This file therefore checks the minimal Go-side
// contract: the right bundle was embedded and inlined, and the app's tool
// names + element ids are present in the served document (so the bundle and
// the templ body agree).

func appModuleFor(t *testing.T, uri string) string {
	t.Helper()
	switch uri {
	case "ui://vault/create.html":
		return renderVaultCreateAppHTML()
	case "ui://vault/restore.html":
		return renderVaultRestoreAppHTML()
	case "ui://auth/sso.html":
		return auth.RenderAuthSSOAppHTML()
	default:
		t.Fatalf("unknown app URI %q", uri)
		return ""
	}
}

func TestAppFlowDocumentsWired(t *testing.T) {
	cases := []struct {
		uri        string
		title      string
		btnID      string
		startTool  string
		statusTool string
		urlField   string // a URL field the app is wired to read, must appear in the bundle
	}{
		{"ui://vault/create.html", "Create Vault", "vault-create-start", "vault_create", "vault_create_status", "create_url"},
		{"ui://vault/restore.html", "Restore Vault", "vault-restore-start", "vault_restore", "vault_restore_status", "restore_url"},
		{"ui://auth/sso.html", "Sign In", "sso-start", "auth_sso", "auth_sso_status", "action_url"},
	}

	for _, c := range cases {
		t.Run(c.title, func(t *testing.T) {
			doc := appModuleFor(t, c.uri)
			// Valid document shell.
			for _, want := range []string{"<!doctype html>", "<script type=\"module\">", "</body></html>"} {
				if !strings.Contains(doc, want) {
					t.Fatalf("doc missing %q", want)
				}
			}
			// The templ body's start button is present and the bundle binds it.
			if !strings.Contains(doc, `id="`+c.btnID+`"`) {
				t.Fatalf("doc missing start button id %q", c.btnID)
			}
			// The embedded bundle targets the right start/status tools and reads
			// the right URL field (so Go-side tool wiring and the JS agree).
			for _, probe := range []string{c.startTool, c.statusTool, c.urlField} {
				if !strings.Contains(doc, probe) {
					t.Fatalf("app document missing tool/field %q (bundle not wired?)", probe)
				}
			}
		})
	}
}

// TestAppFlowDocumentsDistinctToolNames ensures each app's bundle targets its
// OWN status tool and not a sibling's (a config mix-up would break two flows).
func TestAppFlowDocumentsDistinctToolNames(t *testing.T) {
	docs := map[string]string{
		"vault_create":  renderVaultCreateAppHTML(),
		"vault_restore": renderVaultRestoreAppHTML(),
		"auth_sso":      auth.RenderAuthSSOAppHTML(),
	}
	statusTool := map[string]string{
		"vault_create":  "vault_create_status",
		"vault_restore": "vault_restore_status",
		"auth_sso":      "auth_sso_status",
	}
	for own, doc := range docs {
		if !strings.Contains(doc, statusTool[own]) {
			t.Fatalf("app %q missing its own status tool %q", own, statusTool[own])
		}
		for foreign, st := range statusTool {
			if foreign == own {
				continue
			}
			if strings.Contains(doc, st) {
				t.Fatalf("app %q mistakenly references sibling status tool %q", own, st)
			}
		}
	}
}
