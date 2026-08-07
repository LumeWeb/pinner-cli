package cli

// Typed JSON response structs for vault commands.

// vaultCpResponse is the JSON output for `vault cp` (upload direction).
type vaultCpResponse struct {
	Path          string `json:"path"`
	ObjectID      string `json:"object_id"`
	Size          int64  `json:"size"`
	ContentDigest string `json:"content_digest"`
}

// vaultCreateApprovalResponse is the JSON output for `vault create` in agent mode.
//
// The mnemonic is NOT included in the JSON. It is written to a 0600 file at
// SeedPath so it never appears in stdout, logs, or the agent's context window.
// The agent should instruct the user to read the file and pipe it to restore:
//
//	pinner vault restore --profile <name> --seed-stdin < <seed_path>
//
// approval_url is intentionally absent: restore issues its own connection
// request and owns the single browser approval. Create's only job in agent
// mode is to generate the seed.
type vaultCreateApprovalResponse struct {
	Profile  string `json:"profile"`
	SeedPath string `json:"seed_path"`
	NextStep string `json:"next_step"`
}

// vaultProfileEntry is a single profile in the `vault profile list` JSON output.
type vaultProfileEntry struct {
	Name      string `json:"name"`
	VaultID   string `json:"vault_id"`
	Device    string `json:"device"`
	IsDefault bool   `json:"default"`
	Cache     string `json:"cache"`
}

// vaultProfileListResponse is the JSON output for `vault profile list`.
type vaultProfileListResponse struct {
	Profiles []vaultProfileEntry `json:"profiles"`
}

// vaultRestoreApprovalResponse is the JSON output for `vault restore` in agent mode.
// It carries no approval URL: the connection request is deferred to the
// --seed-stdin re-run, which issues the single browser approval.
type vaultRestoreApprovalResponse struct {
	Profile  string `json:"profile"`
	NextStep string `json:"next_step"`
}

// vaultDeviceInfo is the device sub-object in restore/status JSON output.
type vaultDeviceInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// vaultRestoreResponse is the JSON output for `vault restore` on success.
type vaultRestoreResponse struct {
	Profile string          `json:"profile"`
	VaultID string          `json:"vault_id"`
	Device  vaultDeviceInfo `json:"device"`
	Cache   vaultCacheState `json:"cache"`
}

// vaultCacheState is the cache sub-object in restore/status JSON output.
type vaultCacheState struct {
	State string `json:"state"`
}

// vaultRmResponse is the JSON output for `vault rm`.
type vaultRmResponse struct {
	Deleted string `json:"deleted"`
}

// vaultShareResponse is the JSON output for `vault share`.
type vaultShareResponse struct {
	ShareURL string `json:"share_url"`
	Expires  string `json:"expires"`
}

// vaultStatusState is the state sub-object in `vault status` JSON output.
type vaultStatusState struct {
	Unlocked string `json:"unlocked"`
}

// vaultStatusRemote is the remote sub-object in `vault status` JSON output.
type vaultStatusRemote struct {
	Reachable bool `json:"reachable"`
}

// vaultStatusCache is the cache sub-object in `vault status` JSON output.
type vaultStatusCache struct {
	State          string `json:"state"`
	ObjectsIndexed int64  `json:"objects_indexed"`
}

// vaultStatusResponse is the JSON output for `vault status`.
type vaultStatusResponse struct {
	Profile string            `json:"profile"`
	VaultID string            `json:"vault_id"`
	State   vaultStatusState  `json:"state"`
	Device  vaultDeviceInfo   `json:"device"`
	Remote  vaultStatusRemote `json:"remote"`
	Cache   vaultStatusCache  `json:"cache"`
}

// vaultSyncResponse is the JSON output for `vault sync`.
type vaultSyncResponse struct {
	EventsProcessed int `json:"events_processed"`
}
