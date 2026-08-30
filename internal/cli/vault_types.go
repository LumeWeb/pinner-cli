package cli

// Typed JSON response structs for vault commands.

// vaultCpResponse is the JSON output for `vault cp` (upload direction).
type vaultCpResponse struct {
	Path          string `json:"path"`
	ObjectID      string `json:"object_id"`
	Size          int64  `json:"size"`
	ContentDigest string `json:"content_digest"`
	// Status reflects durability: "pending" means the bytes are staged locally
	// (readable, but not yet uploaded/pinned to Sia); "ok" means durable.
	// Pending status arises when the CLI put staged a buffer without --flush.
	Status string `json:"status,omitempty"`
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

// vaultFlushResponse is the JSON output for `vault flush`.
type vaultFlushResponse struct {
	Flushed int `json:"flushed"`
}
