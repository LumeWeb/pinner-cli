package mcpapp

import (
	"encoding/base64"
	"strings"
	"testing"
)

// Tests for the SDK-neutral MCP Apps render/asset/flow layer extracted from
// internal/mcp. These cover the standalone package's public surface: the
// shared document shell, the per-app flow module, and the embedded client
// asset.

func TestRenderMcpAppDoc(t *testing.T) {
	body := PinCreateAppForm()
	doc := RenderMcpAppDoc("Test", body, "/* module */")
	for _, want := range []string{
		"<!doctype html>",
		"<title>Test</title>",
		"<script type=\"module\">",
		"/* module */",
		"</body></html>",
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("render doc missing %q", want)
		}
	}
}

func TestExtAppsClientBase64(t *testing.T) {
	b := ExtAppsClientBase64()
	if b == "" {
		t.Fatal("ext-apps client base64 is empty")
	}
	// Decodable base64 of non-empty JS.
	raw, err := base64.StdEncoding.DecodeString(b)
	if err != nil {
		t.Fatalf("ext-apps client base64 not decodable: %v", err)
	}
	if len(raw) == 0 || !strings.Contains(string(raw), "App") {
		t.Fatalf("ext-apps client does not look like the bundle (len=%d)", len(raw))
	}
}

// TestRenderAppFlowModuleWired pins the shared flow module: it pulls in the
// bootstrap and carries the start/status tool and the handle-presence
// dead-handle guard every flow depends on.
func TestRenderAppFlowModuleWired(t *testing.T) {
	mod := RenderAppFlowModule("dGVzdA==", AppFlowSpec{
		Name:        "TestFlow",
		Version:     "1.0.0",
		StartTool:   "test_start",
		StatusTool:  "test_status",
		StartBtnID:  "test-start",
		UrlElID:     "test-url",
		StatusElID:  "test-status",
		URLFields:   []string{"action_url"},
		ActionLabel: "test",
		RetryWord:   "start",
	})
	for _, want := range []string{
		`CLIENT_B64 = "`,
		`name: "test_start"`,
		`name: "test_status"`,
		`$("#test-start")`,
		`if (startBtn.disabled) return;`,         // in-flight guard
		`if (!sc.handle) {`,                      // start guard
		`status === "needs_human" && !sc.handle`, // dead-handle predicate
		`function finishFlow()`,
	} {
		if !strings.Contains(mod, want) {
			t.Errorf("flow module missing %q", want)
		}
	}
}
