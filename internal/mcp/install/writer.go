package install

import (
	"fmt"
	"os"
	"strings"

	"github.com/BurntSushi/toml"
	"gopkg.in/yaml.v3"
)

// WriteServerConfig merges the server entry into the agent config at path and
// writes it back. It creates the file/dir if missing. Format dispatch is by the
// agent's Format. The server entry is written under the agent's server key
// (ServerKey(true) for a local config, ServerKey(false) otherwise).
//
// JSON/JSONC configs are edited surgically in place (via sjson) so the user's
// existing comments, formatting, and unrelated fields are preserved; YAML and
// TOML configs are re-serialized wholesale.
func WriteServerConfig(agent Agent, path string, serverName string, serverCfg McpServerConfig, local bool) error {
	key := agent.ServerKey(local)
	entry := agent.Transform(serverName, serverCfg, local)

	if isJSONFormat(agent.Format()) {
		return writeJSONCEntry(agent.Key(), path, key, serverName, entry)
	}

	root, err := readRoot(agent.Format(), path)
	if err != nil {
		return fmt.Errorf("%s: read %s: %w", agent.Key(), path, err)
	}

	servers, err := getOrCreateServers(root, key)
	if err != nil {
		return fmt.Errorf("%s: %w", agent.Key(), err)
	}
	servers[serverName] = entry

	if err := writeRoot(agent.Format(), path, root); err != nil {
		return fmt.Errorf("%s: write %s: %w", agent.Key(), path, err)
	}
	return nil
}

// RemoveServer removes a server entry by name from the config at path. It is a
// no-op (returning nil) when the config file does not exist or the entry is not
// present. JSON/JSONC configs are edited surgically; YAML/TOML are re-serialized.
func RemoveServer(agent Agent, path string, serverName string) error {
	key := agent.ServerKey(false)

	if isJSONFormat(agent.Format()) {
		return removeJSONCEntry(agent.Key(), path, key, serverName)
	}

	root, err := readRoot(agent.Format(), path)
	if err != nil {
		return fmt.Errorf("%s: read %s: %w", agent.Key(), path, err)
	}
	// A missing file yields an empty map — nothing to remove.
	if len(root) == 0 {
		return nil
	}

	servers := resolveServers(root, key)
	if servers == nil {
		return nil
	}
	if _, ok := servers[serverName]; !ok {
		return nil
	}
	delete(servers, serverName)
	return writeRoot(agent.Format(), path, root)
}

// DetectGlobal reports whether a global install of the agent is present (its
// config file exists).
func DetectGlobal(agent Agent) bool {
	path := agent.GlobalConfigPath()
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

// readRoot loads a YAML or TOML config file into a map[string]any tree. JSON
// and JSONC configs are never read as maps in production — writes go through the
// surgical sjson path in jsonc_edit.go instead.
func readRoot(format ConfigFormat, path string) (map[string]any, error) {
	switch format {
	case FormatYAML:
		return readYAMLFile(path)
	case FormatTOML:
		return readTOMLFile(path)
	default:
		return nil, fmt.Errorf("readRoot: format %q does not use map read", format)
	}
}

// writeRoot serializes a YAML or TOML map[string]any tree to path. JSON and
// JSONC configs are edited surgically via the sjson path in jsonc_edit.go.
func writeRoot(format ConfigFormat, path string, root map[string]any) error {
	switch format {
	case FormatYAML:
		return writeYAMLFile(path, root)
	case FormatTOML:
		return writeTOMLFile(path, root)
	default:
		return fmt.Errorf("writeRoot: format %q does not use map write", format)
	}
}

// getOrCreateServers resolves configKey (dot-notation allowed) and returns the
// servers sub-map, creating the path if missing. It returns an error rather
// than overwrite an existing non-map value at any segment of the config key, so
// a malformed config (e.g. mcpServers holding a string) is never silently
// replaced with an empty map and the user's data lost.
func getOrCreateServers(root map[string]any, configKey string) (map[string]any, error) {
	if servers := resolveServers(root, configKey); servers != nil {
		return servers, nil
	}
	parent := root
	keys := strings.Split(configKey, ".")
	last := keys[len(keys)-1]
	for _, k := range keys[:len(keys)-1] {
		next, ok := parent[k].(map[string]any)
		if !ok {
			if _, exists := parent[k]; exists {
				return nil, fmt.Errorf("config key %q is not an object; refusing to overwrite", configKey)
			}
			next = map[string]any{}
			parent[k] = next
		}
		parent = next
	}
	if existing, ok := parent[last]; ok {
		if _, isMap := existing.(map[string]any); !isMap {
			return nil, fmt.Errorf("config key %q is not an object; refusing to overwrite", configKey)
		}
	}
	servers := map[string]any{}
	parent[last] = servers
	return servers, nil
}

// resolveServers walks configKey (dot-notation) and returns the servers map, or
// nil if the path does not exist or is not a map.
func resolveServers(root map[string]any, configKey string) map[string]any {
	keys := strings.Split(configKey, ".")
	cur := root
	for _, k := range keys {
		next, ok := cur[k].(map[string]any)
		if !ok {
			return nil
		}
		cur = next
	}
	return cur
}

func readYAMLFile(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]any{}, nil
		}
		return nil, err
	}
	if len(bytesTrimSpace(data)) == 0 {
		return map[string]any{}, nil
	}
	var root map[string]any
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, err
	}
	return root, nil
}

func writeYAMLFile(path string, root map[string]any) error {
	data, err := yaml.Marshal(root)
	if err != nil {
		return err
	}
	return writeFileAtomic(path, data)
}

func readTOMLFile(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]any{}, nil
		}
		return nil, err
	}
	if len(bytesTrimSpace(data)) == 0 {
		return map[string]any{}, nil
	}
	var root map[string]any
	if err := toml.Unmarshal(data, &root); err != nil {
		return nil, err
	}
	return root, nil
}

func writeTOMLFile(path string, root map[string]any) error {
	var buf strings.Builder
	if err := toml.NewEncoder(&buf).Encode(root); err != nil {
		return err
	}
	return writeFileAtomic(path, []byte(buf.String()))
}
