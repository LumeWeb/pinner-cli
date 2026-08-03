package vault

import "encoding/json"

// FileMetadata is attached to Sia objects as object metadata.
// It is encrypted by the SDK (sealed object) and also cached locally.
// ID is the stable per-file identity: the vault cache keys files by this UUID,
// so distinct objects with the same name are not conflated and sync never
// drops a file on a name collision.
type FileMetadata struct {
	ID            string         `json:"id"`            // stable per-file UUID
	Name          string         `json:"name"`
	MediaType     string         `json:"media_type"`
	Size          int64          `json:"size"`
	CreatedAt     string         `json:"created_at"` // RFC3339
	ContentDigest string         `json:"content_digest"` // sha256 hex
	Metadata      map[string]any `json:"metadata,omitempty"`
}

func (m FileMetadata) JSON() (json.RawMessage, error) {
	return json.Marshal(m)
}

func ParseFileMetadata(raw json.RawMessage) (FileMetadata, error) {
	var m FileMetadata
	err := json.Unmarshal(raw, &m)
	return m, err
}
