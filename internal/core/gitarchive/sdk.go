package gitarchive

import (
	"context"
	"io"
	"time"

	"go.sia.tech/core/types"
	"go.sia.tech/siastorage"
)

// SDKClient is the subset of *siastorage.SDK that git archive needs. It exists
// so the git archive domain can be exercised against a fake SDK in tests
// without a live indexer or a network round-trip. The concrete realSDK wraps
// *siastorage.SDK and satisfies it directly.
//
// Share URLs are self-contained bearer credentials: CreateSharedObjectURL
// returns a pre-signed URL that embeds the object's encryption key, so anyone
// holding the URL can read the object without any profile/app key. The
// profile-less helper share path relies on this — it will carry the full
// pre-signed URL rather than an account-scoped key.
type SDKClient interface {
	// Upload streams r into obj (which already carries its card metadata via
	// UpdateMetadata) and records the uploaded slabs on obj.
	Upload(ctx context.Context, obj *siastorage.Object, r io.Reader, opts ...siastorage.UploadOption) error
	// PinObject persists obj's sealed metadata/indexer record after Upload.
	PinObject(ctx context.Context, obj siastorage.Object) error
	// Object retrieves an account object by its content-addressed key.
	Object(ctx context.Context, objectKey types.Hash256) (siastorage.Object, error)
	// Download streams an account object's plaintext.
	Download(obj siastorage.Object, opts ...siastorage.DownloadOption) (io.ReadCloser, error)
	// CreateSharedObjectURL builds a self-contained bearer share URL for an
	// object valid until the given time.
	CreateSharedObjectURL(ctx context.Context, objectKey types.Hash256, validUntil time.Time) (string, error)
	// DownloadSharedObject streams a shared object's plaintext from a
	// self-contained bearer share URL (no app key needed for the read).
	DownloadSharedObject(ctx context.Context, sharedURL string, opts ...siastorage.DownloadOption) (io.ReadCloser, error)
	// Close releases SDK-held resources (host connections, thread group).
	Close() error
}

// realSDK adapts the concrete *siastorage.SDK to the SDKClient interface. The
// embedded *siastorage.SDK provides every method in SDKClient unchanged, so
// the adapter is a thin type that makes the production client satisfy the
// mockable interface.
type realSDK struct {
	*siastorage.SDK
}
