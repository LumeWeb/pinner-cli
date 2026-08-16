package install

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// jsoncSetServer surgically sets the server entry at configKey.serverName in the
// raw JSON/JSONC document, preserving all unrelated content (comments, formatting,
// ordering, other fields). It returns an error without modifying the input when
// any segment of configKey from the root to the servers map is present but is not
// an object, so a malformed config (e.g. mcpServers holding a string) is never
// silently replaced with an object and the user's data lost.
//
// The same refusal semantics as getOrCreateServers apply: a missing segment is
// created as an object; an existing non-object segment is a hard error.
func jsoncSetServer(raw []byte, configKey, serverName string, entry any) ([]byte, error) {
	doc := string(raw)
	if strings.TrimSpace(doc) == "" {
		doc = "{}"
	}
	if err := requireObjectSegments(doc, configKey); err != nil {
		return nil, err
	}
	entryRaw, err := json.Marshal(entry)
	if err != nil {
		return nil, fmt.Errorf("marshal server entry: %w", err)
	}
	path := configKey + "." + escapePathSegment(serverName)
	out, err := sjson.SetRaw(doc, path, string(entryRaw))
	if err != nil {
		return nil, err
	}
	return []byte(out), nil
}

// jsoncRemoveServer surgically removes configKey.serverName from the raw JSON/JSONC
// document. It is a no-op (returning the input unchanged) when the entry or the
// servers map does not exist.
func jsoncRemoveServer(raw []byte, configKey, serverName string) ([]byte, error) {
	doc := string(raw)
	if strings.TrimSpace(doc) == "" {
		return raw, nil
	}
	target := configKey + "." + escapePathSegment(serverName)
	if !gjson.Get(doc, configKey).Exists() || !gjson.Get(doc, target).Exists() {
		return raw, nil
	}
	out, err := sjson.Delete(doc, target)
	if err != nil {
		return nil, err
	}
	return []byte(out), nil
}

// requireObjectSegments verifies that, for a dot-notation config key, every
// segment from the root down to (and including) the servers map is either absent
// or an object. This mirrors getOrCreateServers' refusal to overwrite non-map
// values at any point of the config key path.
func requireObjectSegments(doc, configKey string) error {
	segments := strings.Split(configKey, ".")
	acc := segments[0]
	for i := 0; i < len(segments); i++ {
		if i > 0 {
			acc += "." + segments[i]
		}
		res := gjson.Get(doc, acc)
		if res.Exists() && !res.IsObject() {
			return fmt.Errorf("config key %q is not an object; refusing to overwrite", acc)
		}
	}
	return nil
}

// escapePathSegment escapes a single gjson/sjson path segment so arbitrary
// server names (which may contain '.', '[', '#', '(', ')', quotes, etc.) are
// treated as a literal key rather than parsed as path operators. Every
// gjson/sjson metacharacter is escaped with a backslash.
func escapePathSegment(segment string) string {
	var b strings.Builder
	for _, r := range segment {
		switch r {
		case '.', '*', '?', '|', '\\', '[', ']', '(', ')', '#', '"', '\'':
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// isJSONFormat reports whether the format is JSON or JSONC (both share the
// JSON/JSONC read+write path and the FormatJSON constant).
func isJSONFormat(format ConfigFormat) bool {
	return format == FormatJSON
}

// writeJSONCEntry reads the JSON/JSONC config at path, surgically inserts the
// server entry at configKey.<name>, and writes it back atomically. A missing
// file is created. The user's existing comments and formatting are preserved.
func writeJSONCEntry(agentKey AgentKey, path, configKey, serverName string, entry any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("%s: read %s: %w", agentKey, path, err)
		}
		raw = nil // missing file → string writer creates it
	}
	out, err := jsoncSetServer(raw, configKey, serverName, entry)
	if err != nil {
		return fmt.Errorf("%s: %w", agentKey, err)
	}
	// Append a trailing newline for POSIX-friendly files (matching clean writes).
	out = append(out, '\n')
	return writeFileAtomic(path, out)
}

// removeJSONCEntry reads the JSON/JSONC config at path and surgically removes
// configKey.<name>. It is a no-op when the file is missing or the entry absent.
func removeJSONCEntry(agentKey AgentKey, path, configKey, serverName string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // nothing to remove
		}
		return fmt.Errorf("%s: read %s: %w", agentKey, path, err)
	}
	out, err := jsoncRemoveServer(raw, configKey, serverName)
	if err != nil {
		return fmt.Errorf("%s: %w", agentKey, err)
	}
	if bytes.Equal(out, raw) {
		return nil // nothing changed
	}
	out = append(out, '\n')
	return writeFileAtomic(path, out)
}
