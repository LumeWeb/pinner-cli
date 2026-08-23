package mcp

import (
	"strings"
	"testing"

	"go.uber.org/zap/zapcore"
)

func TestLogLevelFromString(t *testing.T) {
	tests := []struct {
		in      string
		want    zapcore.Level
		wantErr bool
	}{
		{"", zapcore.InfoLevel, false},
		{"info", zapcore.InfoLevel, false},
		{"debug", zapcore.DebugLevel, false},
		{"warn", zapcore.WarnLevel, false},
		{"warning", zapcore.WarnLevel, false},
		{"error", zapcore.ErrorLevel, false},
		{"fatal", zapcore.FatalLevel, false},
		{"bogus", 0, true},
	}
	for _, tc := range tests {
		got, err := logLevelFromString(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("logLevelFromString(%q): expected error, got %v", tc.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("logLevelFromString(%q): unexpected error: %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("logLevelFromString(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestNewZapLoggerFormats(t *testing.T) {
	for _, format := range []string{"", "json", "console"} {
		l, err := newZapLogger(zapcore.ErrorLevel, format)
		if err != nil {
			t.Errorf("newZapLogger(%q): unexpected error: %v", format, err)
			continue
		}
		if l == nil {
			t.Errorf("newZapLogger(%q): nil logger", format)
		}
	}
	if _, err := newZapLogger(zapcore.InfoLevel, "bogus"); err == nil {
		t.Error("newZapLogger with bad format: expected error, got nil")
	}
}

func TestMaskArgsRedactsCredentials(t *testing.T) {
	args := map[string]any{
		"email":       "agent@example.com",
		"password":    "s3cret",
		"api_key":     "key-123",
		"session_id":  "sess-1",
		"gnarly_file": "/tmp/x.tar",
	}
	got := maskArgs(args)
	// Original map is never mutated.
	if args["password"] != "s3cret" {
		t.Fatal("maskArgs mutated the caller's map")
	}
	if got["password"] != "****" {
		t.Errorf("password not redacted: %v", got["password"])
	}
	if got["api_key"] != "****" {
		t.Errorf("api_key not redacted: %v", got["api_key"])
	}
	if got["email"] != "agent@example.com" {
		t.Errorf("email should not be redacted: %v", got["email"])
	}
	if got["session_id"] != "sess-1" {
		t.Errorf("session_id should not be redacted: %v", got["session_id"])
	}
	if got["gnarly_file"] != "/tmp/x.tar" {
		t.Errorf("non-secret arg changed: %v", got["gnarly_file"])
	}
	if maskArgs(nil) != nil {
		t.Error("maskArgs(nil) should return nil")
	}
}

func TestTruncateForLog(t *testing.T) {
	short := "ok"
	if got := truncateForLog(short); got != short {
		t.Errorf("short string truncated: %q", got)
	}
	long := make([]byte, 600)
	for i := range long {
		long[i] = 'a'
	}
	got := truncateForLog(string(long))
	if len(got) > 512+3 {
		t.Errorf("long string not bounded: %d", len(got))
	}
}

func TestRedactForLogMasksEmbeddedSecrets(t *testing.T) {
	secret := "super-secret-value-1234567890abcdef"
	hexSecret := "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"
	cases := []string{
		// A tool echoing a configured secret under a sensitive label.
		"auth failed: password " + secret,
		"token: " + secret,
		"--api-key " + secret,
		"--api-key=" + secret,
		"password=" + secret,
		"recovery seed: " + hexSecret,
		"error with bearer " + secret + " in the middle",
	}
	for _, in := range cases {
		got := redactForLog(in)
		if strings.Contains(got, secret) {
			t.Errorf("redactForLog leaked secret %q in %q -> %q", secret, in, got)
		}
	}
	// A bare long hex run (a key/seed/token echoed standalone) is redacted.
	if got := redactForLog("saw token " + hexSecret); strings.Contains(got, hexSecret) {
		t.Errorf("redactForLog leaked bare hex run: %q", got)
	}
	// Ordinary text is preserved.
	plain := "operation failed: resource not found"
	if got := redactForLog(plain); got != plain {
		t.Errorf("redactForLog altered plain text: %q", got)
	}
}

func TestOpenAIFileParamField(t *testing.T) {
	// No `file` arg -> no field.
	if _, ok := openaiFileParamField(map[string]any{"name": "x"}); ok {
		t.Error("expected no field without a file argument")
	}
	if _, ok := openaiFileParamField(nil); ok {
		t.Error("expected no field for nil args")
	}

	// DECISIVE SUCCESS: host rewrote the path into a provided-file object.
	// This is what a working OpenAI handoff must deliver.
	okField, ok := openaiFileParamField(map[string]any{
		"file": map[string]any{
			"download_url": "https://files.oaiusercontent.com/file_123",
			"file_id":      "file_123",
			"file_name":    "example.zip",
			"mime_type":    "application/zip",
		},
	})
	if !ok {
		t.Fatal("expected a field for a provided-file object")
	}
	if got := okField.String; !strings.Contains(got, `"download_url"`) || !strings.Contains(got, `"file_id"`) {
		t.Errorf("provided-file object not captured verbatim: %s", got)
	}

	// DECISIVE FAILURE: local path string -> rewrite did NOT happen.
	failField, ok := openaiFileParamField(map[string]any{
		"file": "/mnt/data/example.zip",
	})
	if !ok {
		t.Fatal("expected a field for a local path string")
	}
	if !strings.Contains(failField.String, `/mnt/data/example.zip`) {
		t.Errorf("local path string not captured verbatim: %s", failField.String)
	}
}
