package mcp

import (
	"fmt"
	"os"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// logLevelFromString parses a human level name into a zapcore.Level. It
// accepts the standard names plus common abbreviations.
func logLevelFromString(s string) (zapcore.Level, error) {
	switch s {
	case "", "info":
		return zapcore.InfoLevel, nil
	case "debug":
		return zapcore.DebugLevel, nil
	case "warn", "warning":
		return zapcore.WarnLevel, nil
	case "error":
		return zapcore.ErrorLevel, nil
	case "fatal":
		return zapcore.FatalLevel, nil
	}
	var l zapcore.Level
	if err := l.UnmarshalText([]byte(s)); err != nil {
		return 0, fmt.Errorf("invalid log level %q (want debug, info, warn, error): %w", s, err)
	}
	return l, nil
}

// newZapLogger builds a zap logger from a level and format. The default
// output is stderr so it never corrupts the stdio JSON-RPC transport (which
// owns stdout). JSON is the default for machine consumption; "console" prints
// human-readable lines for terminal operation.
func newZapLogger(level zapcore.Level, format string) (*zap.Logger, error) {
	encCfg := zap.NewProductionEncoderConfig()
	var enc zapcore.Encoder
	switch format {
	case "", "json":
		enc = zapcore.NewJSONEncoder(encCfg)
	case "console":
		encCfg.EncodeTime = zapcore.ISO8601TimeEncoder
		enc = zapcore.NewConsoleEncoder(encCfg)
	default:
		return nil, fmt.Errorf("invalid log format %q (want json or console)", format)
	}
	core := zapcore.NewCore(enc, zapcore.Lock(os.Stderr), level)
	return zap.New(core), nil
}
