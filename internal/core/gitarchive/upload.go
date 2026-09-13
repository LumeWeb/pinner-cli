package gitarchive

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"go.sia.tech/core/types"
	"go.sia.tech/siastorage"

	"go.lumeweb.com/pinner-cli/internal/core/objmeta"
)

// UploadedObject describes a successfully uploaded+pinned git archive object.
type UploadedObject struct {
	// Key is the object's content-addressed object key (hex of types.Hash256).
	Key types.Hash256
	// Size is the plaintext payload size in bytes.
	Size int64
	// Digest is the SHA-256 hex digest of the plaintext payload.
	Digest string
	// Card is the sealed card metadata attached to the object.
	Card objmeta.Card
}

// UploadObject uploads a git-archive card. It reads the plaintext payload
// from r, computes its SHA-256 digest and size, seals a pinner.obj/v1 card
// (≤1 KiB, enforced by objmeta.Encode), uploads+pins the object through the
// mockable SDK, and records the resulting content-addressed object into the
// session's git_objects table.
//
// The card body is caller-supplied and kind-specific (PackBody for git.pack,
// TipBody for git.tip, ShareBody for git.share); it carries only the small
// routing/index fields, while the full payload (packfile bytes, tip/share
// document) is the object data.
func UploadObject(ctx context.Context, s *Session, kind objmeta.Kind, body json.RawMessage, r io.Reader) (*UploadedObject, error) {
	if !kind.Valid() {
		return nil, fmt.Errorf("gitarchive: invalid card kind %q", kind)
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("gitarchive: read %s payload: %w", kind, err)
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(data))

	card := objmeta.New(kind, int64(len(data)), digest, time.Now().Unix(), body)
	raw, err := objmeta.Encode(card)
	if err != nil {
		return nil, fmt.Errorf("gitarchive: encode %s card: %w", kind, err)
	}

	obj := siastorage.NewEmptyObject()
	obj.UpdateMetadata(raw)

	sdk, err := s.ensureSDK()
	if err != nil {
		return nil, err
	}
	if err := sdk.Upload(ctx, &obj, bytes.NewReader(data)); err != nil {
		return nil, fmt.Errorf("gitarchive: upload %s object: %w", kind, err)
	}
	if err := sdk.PinObject(ctx, obj); err != nil {
		return nil, fmt.Errorf("gitarchive: pin %s object: %w", kind, err)
	}

	key := obj.ID()
	if _, err := IngestCard(s.DB, key.String(), raw); err != nil {
		return nil, fmt.Errorf("gitarchive: record %s object: %w", kind, err)
	}
	return &UploadedObject{Key: key, Size: int64(len(data)), Digest: digest, Card: *card}, nil
}

// UploadPack uploads a git packfile as a git.pack card. packID is the object's
// bridging identifier recorded in the card body; the authoritative object key
// for later download lives in the tip's packs list and the git_objects table.
func UploadPack(ctx context.Context, s *Session, locker, packID string, full bool, r io.Reader) (*UploadedObject, error) {
	body, err := json.Marshal(objmeta.PackBody{Locker: locker, Pack: packID, Full: full})
	if err != nil {
		return nil, fmt.Errorf("gitarchive: marshal pack body: %w", err)
	}
	return UploadObject(ctx, s, objmeta.KindGitPack, body, r)
}

// UploadTip uploads a tip document as a git.tip card.
func UploadTip(ctx context.Context, s *Session, locker string, gen int, prev string, r io.Reader) (*UploadedObject, error) {
	body, err := json.Marshal(objmeta.TipBody{Locker: locker, Gen: gen, Prev: prev})
	if err != nil {
		return nil, fmt.Errorf("gitarchive: marshal tip body: %w", err)
	}
	return UploadObject(ctx, s, objmeta.KindGitTip, body, r)
}
