package install

import (
	"os"
	"path/filepath"
)

// DetectProjectAgents returns the agents that have a detectable project
// (local) config present in dir.
func DetectProjectAgents(dir string) []AgentKey {
	var found []AgentKey
	for _, key := range AllAgents {
		agent, ok := Agent(key)
		if !ok || agent.LocalConfigPath == "" {
			continue
		}
		if projectConfigExists(dir, agent) {
			found = append(found, key)
		}
	}
	return found
}

// DetectGlobalAgents returns the agents that are globally installed (their
// global config file exists).
func DetectGlobalAgents() []AgentKey {
	var found []AgentKey
	for _, key := range AllAgents {
		agent, ok := Agent(key)
		if !ok {
			continue
		}
		if DetectGlobal(agent) {
			found = append(found, key)
		}
	}
	return found
}

// projectConfigExists reports whether any of the agent's ProjectDetectPaths
// exists under dir.
func projectConfigExists(dir string, agent AgentConfig) bool {
	paths := agent.ProjectDetectPaths
	if len(paths) == 0 {
		// Fall back to the local config path when no explicit detect paths set.
		if agent.LocalConfigPath != "" {
			paths = []string{agent.LocalConfigPath}
		}
	}
	for _, rel := range paths {
		full := filepath.Join(dir, rel)
		if _, err := os.Stat(full); err == nil {
			return true
		}
	}
	return false
}
