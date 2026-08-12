package catalogops

// AuthTokenInputKey is the reserved input-map key through which the CLI wiring
// threads the per-invocation --auth-token flag override into an operation's
// service construction.
//
// When present and non-empty it takes precedence over the deps.GetAuthToken()
// config fallback (flag takes precedence over config).
const AuthTokenInputKey = "auth_token"

// authTokenFromInput returns the --auth-token flag override threaded through
// the input map, or "" when none was provided.
func authTokenFromInput(input map[string]any) string {
	if v, ok := input[AuthTokenInputKey]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}
