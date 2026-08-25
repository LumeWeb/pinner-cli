package websites

import "strings"

// StripValidationPrefix strips the "key=" prefix from a validation token value.
// Some server builds return the full DNS TXT record value (e.g.
// "pinner-verify=abc123"); display wants only the portion after the "=".
func StripValidationPrefix(token string) string {
	if idx := strings.Index(token, "="); idx >= 0 {
		return token[idx+1:]
	}
	return token
}
