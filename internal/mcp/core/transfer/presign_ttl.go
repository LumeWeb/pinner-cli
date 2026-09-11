package transfer

import (
	"fmt"
	"strings"
	"time"
)

// Canonical presigned-PUT TTL parsing for EVERY presigned-upload surface
// (upload_file remote mint, vault_put_file mint, the IPFS/Vault upload App
// helpers, the open_upload_manager / open_vault_manager launchers). This is
// the ONE parser each of those paths must use so the accepted wire format,
// the default, the non-positive handling, and the error wording cannot drift
// between the tool, launcher, and app-only helper surfaces:
//
//   - empty (or whitespace) input yields DefaultHTTPUploadTTL;
//   - unparseable input yields a wrapped time.ParseDuration error with the
//     stable wording `invalid ttl "..."` (callers surface it as-is);
//   - a parsed non-positive duration is NOT an error: it falls back to
//     DefaultHTTPUploadTTL, matching the coordinator-side clamping (an
//     explicit "0s" or "-1m" gets the documented default lifetime rather than
//     an unusable endpoint).
//
// Callers keep their downstream `ttl <= 0 → default` clamps as defense in
// depth only; they must not re-implement the parsing.
func ParsePresignTTL(raw string) (time.Duration, error) {
	if strings.TrimSpace(raw) == "" {
		return DefaultHTTPUploadTTL, nil
	}
	d, err := time.ParseDuration(strings.TrimSpace(raw))
	if err != nil {
		return 0, fmt.Errorf("invalid ttl %q: %w", raw, err)
	}
	if d <= 0 {
		return DefaultHTTPUploadTTL, nil
	}
	return d, nil
}
