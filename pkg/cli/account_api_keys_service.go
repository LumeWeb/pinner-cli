package cli

import (
	"context"
	"fmt"

	"github.com/golang-jwt/jwt/v5"
	portalsdk "go.lumeweb.com/portal-sdk"
)

// APIKeyService provides API key management operations.
type APIKeyService interface {
	ListAPIKeys(ctx context.Context, search string) ([]*portalsdk.APIKey, int, error)
	CreateAPIKey(ctx context.Context, name string) (*portalsdk.APIKey, error)

	// DeleteAPIKey deletes an API key by UUID or name.
	// Blocked if the key is the one currently used for authentication, unless force is true.
	DeleteAPIKey(ctx context.Context, idOrName string, force bool) error

	// GetCurrentAPIKeyUUID returns the UUID from the auth token's JWT subject claim,
	// or empty string if not an API key JWT.
	GetCurrentAPIKeyUUID() string
	RequireAuthenticated() error
}

type apiKeyService struct {
	authService AuthService
	authToken   string
}

type APIKeyServiceFactory func(authService AuthService, authToken string) APIKeyService

func defaultAPIKeyServiceFactory(authService AuthService, authToken string) APIKeyService {
	return NewAPIKeyService(authService, authToken)
}

func NewAPIKeyService(authService AuthService, authToken string) APIKeyService {
	return &apiKeyService{
		authService: authService,
		authToken:   authToken,
	}
}

func (s *apiKeyService) ListAPIKeys(ctx context.Context, search string) ([]*portalsdk.APIKey, int, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, 0, err
	}

	client, err := s.authService.GetAuthenticatedClient(ctx)
	if err != nil {
		return nil, 0, err
	}

	opts := []portalsdk.ListOption{}
	if search != "" {
		opts = append(opts, portalsdk.WithSearch(search))
	}

	return client.ListAPIKeys(ctx, opts...)
}

func (s *apiKeyService) CreateAPIKey(ctx context.Context, name string) (*portalsdk.APIKey, error) {
	if err := s.RequireAuthenticated(); err != nil {
		return nil, err
	}

	client, err := s.authService.GetAuthenticatedClient(ctx)
	if err != nil {
		return nil, err
	}

	return client.CreateAPIKey(ctx, name)
}

func (s *apiKeyService) DeleteAPIKey(ctx context.Context, idOrName string, force bool) error {
	if err := s.RequireAuthenticated(); err != nil {
		return err
	}

	uuid, err := s.resolveAPIKeyID(ctx, idOrName)
	if err != nil {
		return err
	}

	currentUUID := s.GetCurrentAPIKeyUUID()
	if currentUUID != "" && currentUUID == uuid && !force {
		return fmt.Errorf("cannot delete API key %q: it is the key currently used for authentication. Use --force to override", idOrName)
	}

	client, err := s.authService.GetAuthenticatedClient(ctx)
	if err != nil {
		return err
	}

	if err := client.DeleteAPIKey(ctx, uuid); err != nil {
		return fmt.Errorf("failed to delete API key: %w", err)
	}

	return nil
}

func (s *apiKeyService) GetCurrentAPIKeyUUID() string {
	token := s.authToken
	if token == "" {
		return ""
	}

	purpose, err := GetJWTPurpose(token)
	if err != nil || purpose != "api" {
		return ""
	}

	// Subject claim holds the API key UUID
	parser := jwt.NewParser(jwt.WithoutClaimsValidation())
	parsedToken, _, err := parser.ParseUnverified(token, &jwt.RegisteredClaims{})
	if err != nil {
		return ""
	}

	claims, ok := parsedToken.Claims.(*jwt.RegisteredClaims)
	if !ok {
		return ""
	}

	return claims.Subject
}

func (s *apiKeyService) RequireAuthenticated() error {
	if s.authToken == "" {
		return ErrNotAuthenticated
	}
	return nil
}

// resolveAPIKeyID resolves a name or UUID to a UUID string.
// UUID-formatted strings pass through; names are resolved via ListAPIKeys.
func (s *apiKeyService) resolveAPIKeyID(ctx context.Context, idOrName string) (string, error) {
	if isUUIDString(idOrName) {
		return idOrName, nil
	}

	keys, _, err := s.ListAPIKeys(ctx, idOrName)
	if err != nil {
		return "", fmt.Errorf("failed to look up API key by name: %w", err)
	}

	for _, key := range keys {
		if key.Name == idOrName {
			return key.Uuid.String(), nil
		}
	}

	return "", fmt.Errorf("API key not found for name %q", idOrName)
}

func isUUIDString(s string) bool {
	return len(s) == 36 && s[8] == '-' && s[13] == '-' && s[18] == '-' && s[23] == '-'
}
