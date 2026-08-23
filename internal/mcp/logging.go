package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
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

// sensitiveArgKeys are map keys whose values must never be logged verbatim.
// They cover both the wizard/SSO tools (password, token) and the CLI arg-map
// keys the official SDK delivers (auth_token, api_key, private_key, ...). The
// set is deliberately conservative: when in doubt about a value that could be
// credential material, mask it.
var sensitiveArgKeys = map[string]bool{
	"password": true, "pass": true, "token": true, "auth_token": true,
	"api_key": true, "apikey": true, "secret": true, "client_secret": true,
	"key": true, "private_key": true, "privatekey": true, "passphrase": true,
	"mnemonic": true, "seed": true, "recovery_seed": true, "code": true,
	"otp": true, "csrf": true,
}

// maskArgs returns a copy of the tool arguments safe for logging: values whose
// key is credential-like are replaced with a redaction marker, and unchanged
// scalars/arrays are passed through. It never mutates the caller's map.
func maskArgs(args map[string]any) map[string]any {
	if len(args) == 0 {
		return nil
	}
	out := make(map[string]any, len(args))
	for k, v := range args {
		if sensitiveArgKeys[k] {
			out[k] = "****"
			continue
		}
		out[k] = v
	}
	return out
}

// logToolCallStart logs the beginning of a tool invocation with its
// credential-masked arguments. Returns nothing; the caller keeps startedAt to
// report duration on completion.
func logToolCallStart(logger *zap.Logger, name string, args map[string]any) {
	if logger == nil {
		logger = log
	}
	masked := maskArgs(args)
	fields := []zap.Field{zap.String("tool", name)}
	if len(masked) > 0 {
		fields = append(fields, zap.Any("args", masked))
	}
	if field, ok := openaiFileParamField(args); ok {
		fields = append(fields, field)
	}
	logger.Info("tool call", fields...)
}

// openaiFileParamField captures the exact value a host sent for a top-level
// `file` argument (the OpenAI openai/fileParams handoff target) so a diagnostic
// can decide, from the actual tools/call, whether the host rewrote the value
// into a provided-file object. A decisive handoff arrives as an OBJECT with
// download_url + file_id; a failed/missing rewrite arrives as a local PATH
// STRING. Seeing `file: string` in the model-facing schema is normal and is NOT
// proof of failure — only the raw argument above is decisive. It returns no
// field when there is no top-level `file` argument.
func openaiFileParamField(args map[string]any) (zap.Field, bool) {
	if len(args) == 0 {
		return zap.Skip(), false
	}
	v, ok := args["file"]
	if !ok {
		return zap.Skip(), false
	}
	raw, err := json.Marshal(v)
	if err != nil {
		raw = []byte(`"<unserializable>"`)
	}
	return zap.String("openai_file_param", string(raw)), true
}

// logToolCallEnd reports the outcome of a tool invocation: duration and, on
// failure, the error. Success is logged at info; failures at warn so they are
// surfaced without drowning normal traffic.
func logToolCallEnd(logger *zap.Logger, name string, startedAt time.Time, result model.ToolResult, err error) {
	if logger == nil {
		logger = log
	}
	outcome := "ok"
	if err != nil || result.IsError {
		outcome = "error"
	}
	fields := []zap.Field{
		zap.String("tool", name),
		zap.String("outcome", outcome),
		zap.Bool("is_error", result.IsError),
		zap.Int64("duration_ms", time.Since(startedAt).Milliseconds()),
	}
	if result.IsError && result.Text != "" {
		fields = append(fields, zap.String("error", redactForLog(result.Text)))
	} else if err != nil {
		fields = append(fields, zap.String("error", redactForLog(err.Error())))
	}
	if outcome == "ok" {
		logger.Info("tool call result", fields...)
	} else {
		logger.Warn("tool call failed", fields...)
	}
}

// redactRegex matches sensitive material embedded in free text so a tool that
// echoes a configured secret back in its error/output cannot leak it into the
// production log. It covers, for a credential-like label (password, token,
// key, seed, mnemonic, ...), the value that follows it whether the separator is
// "=", ":" or plain whitespace, and likewise for " --flag value" / "--flag=value".
var redactRegex = regexp.MustCompile(`(?i)\b(?:` + redactLabelAlt + `)(?:[=:]\s*|\s+)([^,;()\s]+)`)

// redactLabelAlt enumerates the whitespace-delimited sensitive labels plus the
// "--flag" spellings, so both "--api-key supersecret" and "token: supersecret"
// and "password supersecret" redact the value.
const redactLabelAlt = `(--)?(password|pass|token|secret|key|api[_-]?key|private[_-]?key|auth[_-]?token|bearer|passphrase|mnemonic|seed|recovery[_-]?seed|code|otp)`

// redactForLog masks known secret patterns in a string and caps its length.
// It never logs the credential material; both the value after a sensitive
// label and long hex/base64 runs are replaced with a redaction marker.
func redactForLog(s string) string {
	redacted := redactRegex.ReplaceAllString(s, "${1}${2}****")
	// Also redact bare long hex/base64 runs (>=32 chars), the hashes and
	// encodings secrets are typically rendered as.
	redacted = longSecretRE.ReplaceAllString(redacted, "****")
	return truncateForLog(redacted)
}

// longSecretRE matches runs of hex or base64 of length >=32, the hashes and
// encodings secrets are typically rendered as.
var longSecretRE = regexp.MustCompile(`\b[0-9a-fA-F]{32,}\b|[A-Za-z0-9+/]{40,}={0,2}\b`)

// truncateForLog caps a single log field so a runaway command error or output
// snippet cannot balloon a log line.
func truncateForLog(s string) string {
	const max = 512
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
