// Package catalogops implements the auth domain operations for the operation
// catalog: authentication status, agent-safe token login, and logout. Each
// operation drives the core auth service / config manager directly and returns
// typed data; rendering happens in the frontend wiring layer.
//
// The interactive login flow (email/password/OTP prompts) is NOT a catalog
// operation: it is a human/terminal mechanism. The agent-safe login variant
// accepts a pre-issued auth token, validates it, and saves it. The out-of-band
// SSO flow (auth_sso / auth_resume) stays a custom MCP transport tool. See the
// auth domain package docs for the full audience split.
package catalogops

import (
	"context"
	"fmt"
	"strings"

	"go.lumeweb.com/pinner-cli/internal/catalog"
	"go.lumeweb.com/pinner-cli/internal/core/auth"
	"go.lumeweb.com/pinner-cli/internal/core/config"
)

// AuthDeps are the dependencies the auth operations need at construction
// time. All getters are resolved per invocation (lazy-deps pattern) so
// services always use fresh config and never a package-init snapshot.
type AuthDeps struct {
	// CfgMgr returns a live config manager for the current invocation. When
	// nil, operations that need config fail with a clear error.
	CfgMgr func() config.Manager
	// AuthService builds an auth.AuthService for the current config's
	// endpoint. It is a getter so a test/global override stays live. When
	// nil, operations that need the service fail with a clear error.
	AuthService func(cfgMgr config.Manager) auth.AuthService
	// ResolveAuthToken returns the live auth token from config (the stored
	// credential) for status checks that do not need a per-invocation
	// override. When nil, reads the token from the config manager directly.
	ResolveAuthToken func(cfgMgr config.Manager) string
}

// config returns the live config manager for this invocation, or nil.
func (d AuthDeps) config() config.Manager {
	if d.CfgMgr != nil {
		return d.CfgMgr()
	}
	return nil
}

// AuthOperations returns the catalog operations for the auth domain.
func AuthOperations(d AuthDeps) []catalog.Operation {
	return []catalog.Operation{
		authStatus(d),
		authLogin(d),
		authLogout(d),
	}
}

// --- Auth result types (typed data returned to the frontend) ---

// AuthStatusResult is the data returned by the auth_status operation.
type AuthStatusResult struct {
	Authenticated bool   `json:"authenticated"`
	PortalURL     string `json:"portal_url,omitempty"`
	Email         string `json:"email,omitempty"`
	UserID        int    `json:"user_id,omitempty"`
	Message       string `json:"message,omitempty"`
}

// AuthLoginResult is the data returned by the auth_login operation after the
// supplied token is validated and saved.
type AuthLoginResult struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}

// AuthLogoutResult is the data returned by the auth_logout operation.
type AuthLogoutResult struct {
	Status     string `json:"status"`
	ConfigPath string `json:"config_path,omitempty"`
	Message    string `json:"message"`
}

// authStatus is the `auth status` operation. It verifies the stored auth
// token is valid by making a request to the Pinner.xyz API and returns the
// auth state as typed data.
func authStatus(d AuthDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:             "auth_status",
		Title:            "Check authentication status",
		Summary:          "Verify you are authenticated",
		Description:      "Check whether the stored Pinner.xyz auth token is present and valid, returning the authenticated state, the token subject (user id) and, when available, the account email. Call this before authenticated operations to confirm a valid session.",
		MCPTargets: catalog.MCPTargets(
			catalog.Fallback("Call auth_status to verify the stored Pinner.xyz credential is present and valid before running authenticated operations. Returns {authenticated: bool, email?, user_id?, message?}. When authenticated is false, steer the human to the out-of-band sign-in flow (auth_sso -> auth_resume) rather than asking for a password or OTP on this channel."),
		),
		Category:         "account",
		Safety:           catalog.SafetyRead,
		Interaction:      catalog.InteractionAgentSafe,
		Visibility:       catalog.VisibilityBoth,
		Positional:       "",
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			cfgMgr := d.config()
			if cfgMgr == nil {
				return nil, fmt.Errorf("auth_status: no config manager available")
			}
			// A per-request auth-token override (threaded by the MCP dispatch
			// from a hosted server's CredentialResolver, or the CLI --auth-token
			// flag) takes precedence over the config default, so auth_status
			// reflects the calling principal rather than a shared config token.
			token := authTokenFromInput(input)
			if token == "" {
				if d.ResolveAuthToken != nil {
					token = d.ResolveAuthToken(cfgMgr)
				} else {
					token = cfgMgr.Config().AuthToken
				}
			}
			if token == "" {
				return &AuthStatusResult{Authenticated: false, Message: "Not authenticated: no auth token configured"}, nil
			}
			if d.AuthService == nil {
				return nil, fmt.Errorf("auth_status: no auth service wired")
			}
			svc := d.AuthService(cfgMgr)
			res, err := svc.Status(ctx)
			if err != nil {
				return nil, fmt.Errorf("auth_status: %w", err)
			}
			out := &AuthStatusResult{Authenticated: true}
			if res != nil {
				out.PortalURL = res.PortalURL
			}
			// Surface the token subject and account email the description
			// promises, reusing the account endpoint account_info reads from.
			if acct, aerr := svc.GetAccount(ctx); aerr == nil && acct != nil {
				out.Email = acct.Email
				out.UserID = acct.Id
			}
			return out, nil
		}),
	})
}

// authLogin is the `auth login` operation (agent-safe token variant). It
// accepts a pre-issued auth token, validates its JWT structure, saves it to
// config, and returns the resulting auth state. It NEVER prompts for a
// password or OTP: those are human/terminal and SSO/OOB mechanisms, not
// agent-safe inputs on this channel.
func authLogin(d AuthDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:             "auth_login",
		Title:            "Save an auth token",
		Summary:          "Authenticate by saving a provided auth token",
		Description:      "Save a provided Pinner.xyz auth token (JWT) as the stored credential and confirm it is valid. This is the agent-safe login variant; it does not and must not collect a password or OTP. For interactive or out-of-band sign-in, use the SSO flow (auth_sso) so the human authenticates in a browser.",
		MCPTargets: catalog.MCPTargets(
			catalog.Fallback("Call auth_login with a pre-issued auth token (JWT) to store it as the active Pinner.xyz credential. The token argument is sensitive and must be redacted from logs. Returns {status, user_id?, message}. Do NOT ask the human for a password or OTP on this channel; use auth_sso for interactive sign-in."),
		),
		Category:         "account",
		Safety:           catalog.SafetyMutate,
		Interaction:      catalog.InteractionAgentSafe,
		Visibility:       catalog.VisibilityBoth,
		Positional:       "",
		Args: []catalog.OperationArg{
			{Name: "token", Type: catalog.ArgTypeString, Required: true, Sensitive: true, Help: "Pinner.xyz auth token (JWT) to save", AgentHelp: "The Pinner.xyz auth token (JWT) to store as the active credential. Sensitive: never echo it back."},
		},
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			cfgMgr := d.config()
			if cfgMgr == nil {
				return nil, fmt.Errorf("auth_login: no config manager available")
			}
			token := catalog.StrArg(input, "token", "")
			if token == "" {
				return nil, fmt.Errorf("auth_login: missing required argument token")
			}
			// Validate the JWT structure before persisting so a bogus string
			// (e.g. a subcommand name) is not written to config.
			if !validJWTFormat(token) {
				return nil, fmt.Errorf("auth_login: token must be a JWT with 3 dot-separated parts")
			}
			if err := cfgMgr.SetAuthToken(token); err != nil {
				return nil, fmt.Errorf("auth_login: failed to save auth token: %w", err)
			}
			return &AuthLoginResult{Status: "logged_in", Message: "Auth token saved"}, nil
		}),
	})
}

// authLogout is the `auth logout` operation. It clears the stored auth token
// from local config without revoking any API keys on the server.
func authLogout(d AuthDeps) catalog.Operation {
	return catalog.NewOperation(catalog.OperationSpec{
		Name:             "auth_logout",
		Title:            "Log out",
		Summary:          "Clear the stored auth token",
		Description:      "Remove the stored Pinner.xyz auth token from local config so the CLI / MCP server no longer authenticates. Does not revoke API keys on the server.",
		MCPTargets: catalog.MCPTargets(
			catalog.Fallback("Call auth_logout to clear the locally stored Pinner.xyz credential. Returns {status: logged_out | not_authenticated, config_path?, message}. Note this only clears the local token; it does not revoke server-side API keys."),
		),
		Category:         "account",
		Safety:           catalog.SafetyMutate,
		Interaction:      catalog.InteractionAgentSafe,
		Visibility:       catalog.VisibilityBoth,
		Positional:       "",
		Handler: handler(func(ctx context.Context, input map[string]any) (any, error) {
			cfgMgr := d.config()
			if cfgMgr == nil {
				return nil, fmt.Errorf("auth_logout: no config manager available")
			}
			if !cfgMgr.Config().IsAuthenticated() {
				return &AuthLogoutResult{Status: "not_authenticated", Message: "Not authenticated: no auth token configured"}, nil
			}
			configPath := cfgMgr.ConfigPath()
			if err := cfgMgr.SetAuthToken(""); err != nil {
				return nil, fmt.Errorf("auth_logout: failed to clear auth token: %w", err)
			}
			return &AuthLogoutResult{Status: "logged_out", ConfigPath: configPath, Message: "Logged out: auth token cleared"}, nil
		}),
	})
}

// validJWTFormat performs a basic structural check on a JWT token (three
// base64url segments separated by dots). It does not verify the signature; it
// only rejects obviously non-JWT strings.
func validJWTFormat(token string) bool {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return false
	}
	for _, p := range parts {
		if p == "" {
			return false
		}
	}
	return true
}
