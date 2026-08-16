package install

import (
	"encoding/json"
	"os"
)

// readJSONFile reads a JSON (or JSONC-with-comments) config file into a
// map[string]any tree. A missing file yields an empty map (no error).
//
// Go's encoding/json does not accept // or /* */ comments, so JSONC comments
// are stripped before parsing. The file is then written back as clean JSON
// (see writeJSONFile); comment preservation is intentionally deferred to a
// later version (plan A allows a clean rewrite).
func readJSONFile(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]any{}, nil
		}
		return nil, err
	}
	trimmed := bytesTrimSpace(data)
	if len(trimmed) == 0 {
		return map[string]any{}, nil
	}
	var root map[string]any
	if err := json.Unmarshal(stripJSONComments(data), &root); err != nil {
		return nil, err
	}
	return root, nil
}

// stripJSONComments removes // line comments and /* */ block comments while
// leaving string literals intact. It is sufficient for the plain map[string]any
// config files this package handles.
func stripJSONComments(data []byte) []byte {
	out := make([]byte, 0, len(data))
	inString := false
	escaped := false
	for i := 0; i < len(data); {
		c := data[i]
		if inString {
			out = append(out, c)
			if escaped {
				escaped = false
			} else if c == '\\' {
				escaped = true
			} else if c == '"' {
				inString = false
			}
			i++
			continue
		}
		switch {
		case c == '"':
			inString = true
			out = append(out, c)
			i++
		case c == '/' && i+1 < len(data) && data[i+1] == '/':
			// line comment
			for i < len(data) && data[i] != '\n' {
				i++
			}
			out = append(out, '\n')
		case c == '/' && i+1 < len(data) && data[i+1] == '*':
			// block comment
			i += 2
			for i+1 < len(data) && !(data[i] == '*' && data[i+1] == '/') {
				if data[i] == '\n' {
					out = append(out, '\n')
				}
				i++
			}
			if i+1 < len(data) {
				i += 2
			}
		default:
			out = append(out, c)
			i++
		}
	}
	return out
}

func bytesTrimSpace(b []byte) []byte {
	start := 0
	for start < len(b) {
		c := b[start]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			start++
			continue
		}
		break
	}
	end := len(b)
	for end > start {
		c := b[end-1]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			end--
			continue
		}
		break
	}
	return b[start:end]
}

// writeJSONFile writes a map[string]any tree to path with 2-space indentation.
//
// v1 uses a clean JSON rewrite (the plan explicitly permits this instead of
// full comment preservation). The output is deterministic and valid JSON.
func writeJSONFile(path string, root map[string]any) error {
	data, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return writeFileAtomic(path, data)
}
