package transfer

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestParsePresignTTL pins the ONE canonical presigned-PUT TTL parser every
// presigned surface (upload_file remote mint, vault_put_file mint, the app
// helpers, the open_upload_manager / open_vault_manager launchers) must use:
//   - empty/whitespace input → DefaultHTTPUploadTTL;
//   - unparseable input → wrapped error with the stable wording
//     `invalid ttl "..."`;
//   - non-positive input (0s, negative) → DefaultHTTPUploadTTL (NOT an error),
//     matching the coordinator-side clamping.
func TestParsePresignTTL(t *testing.T) {
	t.Run("empty defaults", func(t *testing.T) {
		d, err := ParsePresignTTL("")
		require.NoError(t, err)
		require.Equal(t, DefaultHTTPUploadTTL, d)
	})

	t.Run("whitespace defaults", func(t *testing.T) {
		d, err := ParsePresignTTL("   ")
		require.NoError(t, err)
		require.Equal(t, DefaultHTTPUploadTTL, d)
	})

	t.Run("valid duration passes through", func(t *testing.T) {
		d, err := ParsePresignTTL("5m")
		require.NoError(t, err)
		require.Equal(t, 5*time.Minute, d)
	})

	t.Run("padded duration passes through", func(t *testing.T) {
		// The doc claims whitespace tolerance: non-empty padded input must be
		// trimmed before parsing, not rejected.
		d, err := ParsePresignTTL(" 5m ")
		require.NoError(t, err)
		require.Equal(t, 5*time.Minute, d)
	})

	t.Run("fractional duration passes through", func(t *testing.T) {
		d, err := ParsePresignTTL("90s")
		require.NoError(t, err)
		require.Equal(t, 90*time.Second, d)
	})

	t.Run("zero falls back to default", func(t *testing.T) {
		d, err := ParsePresignTTL("0s")
		require.NoError(t, err)
		require.Equal(t, DefaultHTTPUploadTTL, d)
	})

	t.Run("negative falls back to default", func(t *testing.T) {
		d, err := ParsePresignTTL("-1m")
		require.NoError(t, err)
		require.Equal(t, DefaultHTTPUploadTTL, d)
	})

	t.Run("unparseable yields stable invalid ttl wording", func(t *testing.T) {
		_, err := ParsePresignTTL("soon")
		require.Error(t, err)
		require.Contains(t, err.Error(), `invalid ttl "soon"`)
	})
}
