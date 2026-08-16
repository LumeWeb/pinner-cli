package install

import (
	"fmt"
	"os"
	"strings"

	"github.com/BurntSushi/toml"
	"gopkg.in/yaml.v3"
)

// WriteServerConfig merges the server entry into the agent config at path and
// writes it back. It creates the file/dir if missing. Format dispatch is by
// agent.Format. The server entry is written under the agent's config key
// (LocalKey for a local config, ConfigKey otherwise).
func WriteServerConfig(agent AgentConfig, path string, serverName string, serverCfg McpServerConfig, local bool) error {
	key := agent.ConfigKey
	if local {
		key = agent.LocalKey()
	}

	if agent.Transform == nil {
		return fmt.Errorf("%s: no transform defined", agent.Key)
	}
	entry := agent.Transform(serverName, serverCfg, local)

	root, err := readRoot(agent.Format, path)
	if err != nil {
		return fmt.Errorf("%s: read %s: %w", agent.Key, path, err)
	}

	servers := getOrCreateServers(root, key)
	servers[serverName] = entry

	if err := writeRoot(agent.Format, path, root); err != nil {
		return fmt.Errorf("%s: write %s: %w", agent.Key, path, err)
	}
	return nil
}

// RemoveServer removes a server entry by name from the config at path. It is a
// no-op (returning nil) when the config file does not exist.
func RemoveServer(agent AgentConfig, path string, serverName string) error {
	key := agent.ConfigKey

	root, err := readRoot(agent.Format, path)
	if err != nil {
		return fmt.Errorf("%s: read %s: %w", agent.Key, path, err)
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
	return writeRoot(agent.Format, path, root)
}

// DetectGlobal reports whether a global install of the agent is present (its
// config file exists).
func DetectGlobal(agent AgentConfig) bool {
	if agent.ConfigPath == nil {
		return false
	}
	path := agent.ConfigPath()
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

// readRoot loads a config file into a map[string]any tree by format.
func readRoot(format ConfigFormat, path string) (map[string]any, error) {
	switch format {
	case FormatYAML:
		return readYAMLFile(path)
	case FormatTOML:
		return readTOMLFile(path)
	default: // FormatJSON and FormatJSONC share the JSON path
		return readJSONFile(path)
	}
}

// writeRoot serializes a map[string]any tree to path by format.
func writeRoot(format ConfigFormat, path string, root map[string]any) error {
	switch format {
	case FormatYAML:
		return writeYAMLFile(path, root)
	case FormatTOML:
		return writeTOMLFile(path, root)
	default:
		return writeJSONFile(path, root)
	}
}

// getOrCreateServers resolves configKey (dot-notation allowed) and returns the
// servers sub-map, creating it if missing.
func getOrCreateServers(root map[string]any, configKey string) map[string]any {
	if servers := resolveServers(root, configKey); servers != nil {
		return servers
	}
	parent := root
	keys := strings.Split(configKey, ".")
	last := keys[len(keys)-1]
	for _, k := range keys[:len(keys)-1] {
		next, ok := parent[k].(map[string]any)
		if !ok {
			next = map[string]any{}
			parent[k] = next
		}
		parent = next
	}
	servers := map[string]any{}
	parent[last] = servers
	return servers
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
