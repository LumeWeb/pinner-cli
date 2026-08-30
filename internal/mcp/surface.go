package mcp

import (
	"context"
	"errors"

	"go.lumeweb.com/pinner-cli/internal/mcp/hostenv"
)

// Surface is the internal/mcp alias for the host-platform surface model. It
// declares which Pinner operation domains and tool families a server exposes;
// the zero value is the full surface.
type Surface = hostenv.Surface

// FullSurface enables every domain/tool family (the CLI / local MCP server).
var FullSurface = hostenv.FullSurface

// HostedSurface is the restricted surface for a Portal-embedded ("hosted")
// MCP server: account/subscription and IPFS/websites/DNS/IPNS/ENS/operations,
// with the Sia vault and portal admin excluded.
var HostedSurface = hostenv.HostedSurface

// surfaceVar is the active server construction surface. It mirrors the
// SetTransportFlags / SetDevTools pattern: the server construction path
// (buildCatalog) records the surface once at startup, and per-request
// profile-aware resolution (agent_guide), startup profile derivation, and
// capabilities overlay it so the whole surface agrees on what is exposed.
// Defaults to the full surface so callers that never opt into a restricted
// surface keep full behaviour.
var surfaceVar = FullSurface

// SetSurface records the active server surface. It MUST be called during
// server construction (before serving) and is written once, mirroring
// SetTransportFlags/SetDevTools.
func SetSurface(s Surface) {
	surfaceVar = s
}

// activeSurface returns the surface currently active for this server,
// normalizing the zero value to the full surface.
func activeSurface() Surface {
	if surfaceVar.IsZero() {
		return FullSurface
	}
	return surfaceVar
}

// hostedVar records whether the active server is a hosted (Portal-embedded)
// assembly. It is the deployment-mode counterpart to surfaceVar: recorded once
// at server construction (buildCatalog) and overlaid on the resolved platform
// profile so the prompt/guide DSL can gate hosted-specific copy. Hosted is
// orthogonal to the surface — see activeHosted.
var hostedVar bool

// SetHosted records whether the active server is a hosted (Portal-embedded)
// assembly. It MUST be called during server construction alongside SetSurface
// and is written once (mirroring SetSurface/SetTransportFlags).
func SetHosted(hosted bool) {
	hostedVar = hosted
}

// activeHosted returns whether the active server is a hosted assembly.
func activeHosted() bool {
	return hostedVar
}

// ErrNotAuthenticated is returned by a CredentialResolver when the current
// request has no usable Portal API credential. Handlers surface it as a
// structured needs_auth hand-off rather than a bare error.
var ErrNotAuthenticated = errors.New("not authenticated")

// CredentialResolver resolves the Portal API token for the authenticated
// principal of the current request. It is the seam that lets a hosted
// (Portal-embedded) server route the MCP OAuth backend to Portal's own OAuth
// library/IdP (which has already validated the caller and established a
// user) instead of forcing the CLI's config-token assumptions.
//
// The CLI/local MCP server uses ConfigCredentialResolver (reads the bearer
// token from the pinner config). A hosted server supplies an implementation
// that maps the Portal-authenticated user onto a Portal API JWT.
type CredentialResolver interface {
	// TokenForRequest returns the Portal API token for the currently
	// authenticated request, or ErrNotAuthenticated when there is none.
	TokenForRequest(ctx context.Context) (string, error)
}

// configCredentialResolver resolves the token from the pinner config manager,
// mirroring how the CLI/local MCP server has always obtained its bearer token.
type configCredentialResolver struct {
	cfgMgr func() (string, error)
}

// ConfigCredentialResolver adapts a config-manager accessor into a
// CredentialResolver that reads the stored bearer token. The accessor is
// resolved per invocation so a token edit stays live. When the accessor yields
// an empty token it returns ErrNotAuthenticated.
type ConfigCredentialResolver struct {
	AuthToken func() (string, error)
}

// TokenForRequest implements CredentialResolver.
func (r ConfigCredentialResolver) TokenForRequest(ctx context.Context) (string, error) {
	token := ""
	var err error
	if r.AuthToken != nil {
		token, err = r.AuthToken()
		if err != nil {
			return "", err
		}
	}
	if token == "" {
		return "", ErrNotAuthenticated
	}
	return token, nil
}

var _ CredentialResolver = ConfigCredentialResolver{}
