package mcp

import (
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
