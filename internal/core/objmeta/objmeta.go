// Package objmeta owns the pinner object-metadata wire format for objects that
// are NOT vault files.
//
// The vault domain stamps object metadata as a vault.FileMetadata (RFC3339
// created_at etc.). Git archive hosting reuses the same Sia object store but
// for a different kind of object: git pack / tip / share cards. These live in
// the same object namespace, so they must be distinguishable from vault files
// by ANY consumer of object metadata — including the library's vaultService.Sync,
// which we must not modify.
//
// # Vault-compatibility guard (kind routing)
//
// vault.Sync calls vault.ParseFileMetadata on object metadata. Because Go's
// json.Unmarshal ignores unknown fields, a card containing only card-specific
// fields would parse "successfully" into an empty FileMetadata, and Sync would
// create empty-named vault File rows for git objects. To prevent that without
// touching the library, every card carries a creation timestamp as an INTEGER
// unix-seconds value serialized under the key `created_at` — the SAME key
// vault.FileMetadata expects an RFC3339 STRING for. ParseFileMetadata therefore
// returns a type error (json: cannot unmarshal number into ... created_at of
// type string), vault.Sync skips the object, and no File row is created. This
// integer-`created_at` collision is the kind-routing mechanism: git cards are
// rejected by the vault parser by construction.
//
// Card wire format (≤1024 bytes after marshal):
//
//	{"schema":"pinner.obj/v1","kind":"git.pack|git.tip|git.share",
//	 "size":N,"digest":"<sha256>","created_at":<UNIX-SECONDS-INT>,"body":{...}}
//
// Legacy vault FileMetadata (no schema/kind, has id+name) routes to
// KindVaultFile.
package objmeta

import (
	"encoding/json"
	"errors"
	"fmt"
)

// Schema is the fixed schema identifier for pinner object cards.
const Schema = "pinner.obj/v1"

// MaxCardSize is the hard upper bound on the serialized card (after marshal).
// Cards larger than this are rejected before upload so a git card can never
// silently exceed the sealed-metadata budget the vault stack is tuned for.
const MaxCardSize = 1024

// Kind identifies the object schema that a piece of metadata carries.
type Kind string

const (
	// KindGitPack is a git packfile card.
	KindGitPack Kind = "git.pack"
	// KindGitTip is a git tip/refs card.
	KindGitTip Kind = "git.tip"
	// KindGitShare is a git share URL card.
	KindGitShare Kind = "git.share"
	// KindVaultFile is the legacy vault FileMetadata schema (no schema/kind).
	KindVaultFile Kind = "vault.file"
)

// Valid reports whether k is a card kind (a pinner.obj/v1 schema kind). It is
// false for KindVaultFile, which is not a card.
func (k Kind) Valid() bool {
	switch k {
	case KindGitPack, KindGitTip, KindGitShare:
		return true
	default:
		return false
	}
}

// Sentinel errors for metadata routing.
var (
	// ErrNotCard is returned when the payload is not a pinner.obj/v1 card.
	ErrNotCard = errors.New("objmeta: not a pinner.obj/v1 card")
	// ErrOversize is returned when a card exceeds MaxCardSize.
	ErrOversize = errors.New("objmeta: card exceeds maximum size")
	// ErrUnknownKind is returned when a card carries an unknown kind.
	ErrUnknownKind = errors.New("objmeta: unknown card kind")
	// ErrNilCard is returned when encoding a nil card.
	ErrNilCard = errors.New("objmeta: nil card")
)

// Card is the pinner object card. Created is the unix-seconds creation
// timestamp; it marshals under the key `created_at` (an integer) which is the
// vault-compatibility guard described in the package comment.
type Card struct {
	Schema  string          `json:"schema"`
	Kind    Kind            `json:"kind"`
	Size    int64           `json:"size"`
	Digest  string          `json:"digest"`
	Created int64           `json:"created_at"` // unix seconds (int) — vault guard
	Body    json.RawMessage `json:"body"`
}

// PackBody is the git.pack card body.
type PackBody struct {
	Locker string `json:"locker"`
	Pack   string `json:"pack"`
	Full   bool   `json:"full"`
}

// TipBody is the git.tip card body.
type TipBody struct {
	Locker string `json:"locker"`
	Gen    int    `json:"gen"`
	Prev   string `json:"prev,omitempty"`
}

// ShareBody is the git.share card body.
type ShareBody struct {
	Locker string `json:"locker"`
	Gen    int    `json:"gen"`
	Until  int64  `json:"until,omitempty"`
}

// New builds a card with the fixed schema set.
func New(kind Kind, size int64, digest string, created int64, body json.RawMessage) *Card {
	return &Card{
		Schema:  Schema,
		Kind:    kind,
		Size:    size,
		Digest:  digest,
		Created: created,
		Body:    body,
	}
}

// Encode serializes a card and enforces the 1024-byte cap.
func Encode(c *Card) (json.RawMessage, error) {
	if c == nil {
		return nil, ErrNilCard
	}
	raw, err := json.Marshal(c)
	if err != nil {
		return nil, fmt.Errorf("objmeta: encode card: %w", err)
	}
	if len(raw) > MaxCardSize {
		return nil, fmt.Errorf("%w: %d bytes", ErrOversize, len(raw))
	}
	return raw, nil
}

// Decode parses and validates a card, enforcing the 1024-byte cap.
func Decode(raw []byte) (*Card, error) {
	if len(raw) == 0 {
		return nil, ErrNotCard
	}
	if len(raw) > MaxCardSize {
		return nil, fmt.Errorf("%w: %d bytes", ErrOversize, len(raw))
	}
	var c Card
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("objmeta: decode card: %w", err)
	}
	if c.Schema != Schema {
		return nil, fmt.Errorf("%w: schema %q", ErrNotCard, c.Schema)
	}
	if !c.Kind.Valid() {
		return nil, fmt.Errorf("%w: kind %q", ErrUnknownKind, c.Kind)
	}
	return &c, nil
}

// Route probes a piece of object metadata and returns its kind. It returns a
// git.* kind for a valid card, KindVaultFile for a legacy vault FileMetadata
// (no schema/kind, carries id+name), or an error otherwise.
func Route(raw []byte) (Kind, error) {
	if len(raw) == 0 {
		return "", ErrNotCard
	}
	var probe struct {
		Schema string `json:"schema"`
		Kind   Kind   `json:"kind"`
		ID     string `json:"id"`
		Name   string `json:"name"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return "", fmt.Errorf("objmeta: route: %w", err)
	}
	if probe.Schema == Schema {
		if !probe.Kind.Valid() {
			return "", fmt.Errorf("%w: kind %q", ErrUnknownKind, probe.Kind)
		}
		return probe.Kind, nil
	}
	// Legacy vault FileMetadata probe: no schema, carries a stable id + name.
	if probe.ID != "" && probe.Name != "" {
		return KindVaultFile, nil
	}
	return "", ErrNotCard
}

// IsVaultFile reports whether raw is a legacy vault FileMetadata (routed to
// KindVaultFile).
func IsVaultFile(raw []byte) bool {
	k, err := Route(raw)
	return err == nil && k == KindVaultFile
}

// IsCard reports whether raw is a valid pinner.obj/v1 card (any git kind).
func IsCard(raw []byte) bool {
	k, err := Route(raw)
	return err == nil && k.Valid()
}
