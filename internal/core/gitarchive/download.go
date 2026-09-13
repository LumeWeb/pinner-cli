package gitarchive

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"

	"go.sia.tech/core/types"

	"go.lumeweb.com/pinner-cli/internal/core/objmeta"
)

// DownloadedObject is a fetched git archive object: its card metadata plus a
// reader over the plaintext payload. The caller owns the reader and must Close
// it.
type DownloadedObject struct {
	// Key is the object's content-addressed object key.
	Key types.Hash256
	// Card is the card metadata that routed/validated this object.
	Card objmeta.Card
	// Reader streams the object's plaintext payload.
	Reader io.ReadCloser
}

// FetchObject retrieves a git archive object (pack, tip, or share card) by its
// content-addressed key. It decrypts/validates the object through the SDK,
// routes its card metadata, and returns a reader over the plaintext payload.
func FetchObject(ctx context.Context, s *Session, key types.Hash256) (*DownloadedObject, error) {
	sdk, err := s.ensureSDK()
	if err != nil {
		return nil, err
	}
	obj, err := sdk.Object(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("gitarchive: get object %s: %w", key, err)
	}
	card, err := objmeta.Decode(obj.Metadata())
	if err != nil {
		return nil, fmt.Errorf("gitarchive: decode object %s metadata: %w", key, err)
	}
	rc, err := sdk.Download(obj)
	if err != nil {
		return nil, fmt.Errorf("gitarchive: download object %s: %w", key, err)
	}
	return &DownloadedObject{Key: key, Card: *card, Reader: rc}, nil
}

// FetchObjectBytes is FetchObject followed by reading the whole payload into
// memory. The payload is the raw packfile (for git.pack) or the tip/share
// document (for git.tip / git.share), so it is fully read to be parsed.
//
// It verifies the payload against the card's SHA-256 digest, so every fully
// downloaded object (packs, tips, share documents) is integrity-checked
// against the digest recorded in its sealed card at publish time. A digest
// mismatch is an error — a corrupted/tampered object is never returned.
func FetchObjectBytes(ctx context.Context, s *Session, key types.Hash256) (*DownloadedObject, []byte, error) {
	downloaded, err := FetchObject(ctx, s, key)
	if err != nil {
		return nil, nil, err
	}
	defer downloaded.Reader.Close()
	data, err := io.ReadAll(downloaded.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("gitarchive: read object %s: %w", key, err)
	}
	if downloaded.Card.Digest != "" {
		if got := fmt.Sprintf("%x", sha256.Sum256(data)); got != downloaded.Card.Digest {
			return nil, nil, fmt.Errorf(
				"gitarchive: object %s digest mismatch: card declares %s, payload computes %s",
				key, downloaded.Card.Digest, got)
		}
	}
	downloaded.Reader = io.NopCloser(nil)
	return downloaded, data, nil
}
