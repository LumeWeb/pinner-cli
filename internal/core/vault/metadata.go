package vault

import "encoding/json"

// FileMetadata is attached to Sia objects as object metadata.
// It is encrypted by the SDK (sealed object) and also cached locally.
// ID is the stable per-file identity: the vault cache keys files by this UUID,
// so distinct objects with the same name are not conflated and sync never
// drops a file on a name collision.
//
// Status/LostReason/createdBy/agentID/sessionID are carried through this same
// encode/decode path so lifecycle state and provenance survive cache rebuilds
// and sync to every device like every other field. They are omitempty so a
// healthy file ("ok", no provenance) does not consume the 1024-byte sealed
// metadata budget.
type FileMetadata struct {
	ID            string         `json:"id"`            // stable per-file UUID
	VersionID     string         `json:"version_id,omitempty"` // opaque version handle (syncs)
	Seq           uint           `json:"seq,omitempty"`       // monotonic per-UUID version ordering
	Name          string         `json:"name"`
	Directory     string         `json:"directory,omitempty"` // vault dir path, e.g. "/reports/2024"
	MediaType     string         `json:"media_type"`
	Size          int64          `json:"size"`
	CreatedAt     string         `json:"created_at"` // RFC3339
	ContentDigest string         `json:"content_digest"` // sha256 hex
	Metadata      map[string]any `json:"metadata,omitempty"`
	// Lifecycle: "ok" (default, omitted) | "pending" | "lost". LostReason is
	// the terminal detail (e.g. slab-unavailable error) when Status == "lost".
	Status      string `json:"status,omitempty"`
	LostReason  string `json:"lost_reason,omitempty"`
	// Provenance: best-effort, user-attested audit fields.
	CreatedBy string `json:"created_by,omitempty"`
	AgentID   string `json:"agent_id,omitempty"`
	SessionID string `json:"session_id,omitempty"`
}

func (m FileMetadata) JSON() (json.RawMessage, error) {
	return json.Marshal(m)
}

func ParseFileMetadata(raw json.RawMessage) (FileMetadata, error) {
	var m FileMetadata
	err := json.Unmarshal(raw, &m)
	return m, err
}
