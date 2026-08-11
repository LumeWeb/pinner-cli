package catalogops

// AuthTokenInputKey is the reserved input-map key through which the CLI wiring
// threads the per-invocation --auth-token flag override into an operation's
// service construction. It is the only channel by which the per-invocation
// command flag (held in the pkg/cli wiring layer) can reach the canonical
// deps closures in internal/catalogops, which by architectural invariant may
// not import pkg/cli.
//
// When present and non-empty it takes precedence over the deps.GetAuthToken()
// config fallback, mirroring the legacy GetAuthToken(c, cfgMgr) precedence of
// flag -> config.
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
