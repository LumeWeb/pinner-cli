package vault

// Reserved metadata keys that are auto-stamped by the environment (never by
// the caller). They are stored as siblings of the "tags" array inside the
// object's opaque metadata map, conceptually separate from tags: tags are a
// normalized []string searchable set, while these keys record write context
// (which frontend, which host, which profile handled the write).
const (
	// MetaKeySrc is the frontend that performed the write: "mcp" or "cli".
	// It separates agent dumps from a person running `pinner vault cp`.
	MetaKeySrc = "src"
	// MetaKeyHost is the MCP platform profile host type (e.g.
	// "claude-desktop", "cursor", "generic") detected for the request, or
	// omitted for CLI writes. It distinguishes a host with no disk from one
	// that could mint-and-PUT.
	MetaKeyHost = "host"
	// MetaKeyProfile is the vault profile name that actually received the
	// object. It matters once more than one vault exists on a machine.
	MetaKeyProfile = "profile"
)

// StampedMetadata builds the auto-stamped metadata map for a vault write.
// Reserved keys (src, host, profile) always take the environment value; caller
// supplied KV (kind, project, agent, role, ...) is merged for everything else.
// An empty value for a reserved key omits that key rather than writing an empty
// string. The returned map is nil when nothing is provided.
func StampedMetadata(src, host, profile string, callerKV map[string]any) map[string]any {
	stamp := map[string]any{}
	if src != "" {
		stamp[MetaKeySrc] = src
	}
	if host != "" {
		stamp[MetaKeyHost] = host
	}
	if profile != "" {
		stamp[MetaKeyProfile] = profile
	}
	for k, v := range callerKV {
		// Reserved keys are never overridable by the caller.
		if _, reserved := stamp[k]; reserved {
			continue
		}
		stamp[k] = v
	}
	if len(stamp) == 0 {
		return nil
	}
	return stamp
}
