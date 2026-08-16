package install

// DetectProjectAgents returns the agents that have a detectable project
// (local) config present in dir, using the default registry.
func DetectProjectAgents(dir string) []AgentKey {
	return Default.DetectProject(dir)
}

// DetectGlobalAgents returns the agents that are globally installed (their
// global config file exists), using the default registry.
func DetectGlobalAgents() []AgentKey {
	return Default.DetectGlobal()
}
