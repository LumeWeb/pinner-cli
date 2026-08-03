package vault

import (
	"encoding/hex"
	"fmt"
	"strings"

	"go.sia.tech/core/types"
)

// AppID returns the deterministic app ID for pinner-cli.
func AppID() types.Hash256 {
	return types.HashBytes([]byte("pinner-cli"))
}

// DecodeAppKey parses a hex-encoded app key into a types.PrivateKey.
func DecodeAppKey(hexKey string) (types.PrivateKey, error) {
	keyBytes, err := hex.DecodeString(hexKey)
	if err != nil {
		return nil, fmt.Errorf("invalid app key: %w", err)
	}
	return types.PrivateKey(keyBytes), nil
}

// NormalizeShareURL converts an https:// share URL to sia:// scheme.
func NormalizeShareURL(rawURL string) string {
	return strings.Replace(rawURL, "https://", "sia://", 1)
}

// DenormalizeShareURL converts a sia:// share URL back to https://.
func DenormalizeShareURL(sharedURL string) string {
	return strings.Replace(sharedURL, "sia://", "https://", 1)
}
