// Package auth provides the authentication domain for pinner-cli.
//
// It is deliberately free of CLI presentation coupling: AuthService returns
// typed results (rather than printing to an Output formatter) and logs debug
// information through an injected *zap.Logger. Callers — the CLI command
// handlers, the MCP adapter, or any future consumer — depend on the
// AuthService interface and are responsible for rendering the returned
// results.
package auth

import (
	"context"
	"fmt"

	"github.com/golang-jwt/jwt/v5"
	"go.lumeweb.com/pinner-cli/internal/core/config"
	coreerrors "go.lumeweb.com/pinner-cli/internal/core/errors"
	portalsdk "go.lumeweb.com/portal-sdk"
	"go.uber.org/zap"
)

// ClientFactory creates an account client with the given endpoint and JWT token.
type ClientFactory func(endpoint, jwt string) portalsdk.AccountAPI

// defaultClientFactory creates an account client using portal-sdk options.
func defaultClientFactory(endpoint, jwt string) portalsdk.AccountAPI {
	opts := []portalsdk.ClientOption{portalsdk.WithEndpoint(endpoint)}
	if jwt != "" {
		opts = append(opts, portalsdk.WithJWT(jwt))
	}
	return portalsdk.NewClient(opts...)
}

// APIKeyStatus describes what happened with the API key during auth.
type APIKeyStatus int

const (
	APIKeyNone    APIKeyStatus = iota // no API key (token saved directly)
	APIKeyCreated                     // new API key created
	APIKeyReused                      // existing API key reused
)

// LoginCompleteResult is returned by CompleteLogin and LoginWithOTP after a
// successful authentication. It carries the data the CLI needs to render a
// success message; core never formats or prints it.
type LoginCompleteResult struct {
	APIKeyName   string
	APIKeyStatus APIKeyStatus
	ConfigPath   string
	PortalURL    string
}

// RegisterResult is returned by Register after a successful registration.
// Email is preserved so callers can render a "verification email sent"
// message without the core needing to format presentation prose.
type RegisterResult struct {
	Email string
}

// SaveTokenResult is returned by SaveToken after a token has been persisted.
type SaveTokenResult struct {
	ConfigPath string
	PortalURL  string
}

// OTPSecretResult is returned by GenerateOTPSecret. The caller renders the
// secret for the user (e.g. "add this to your authenticator app") before
// prompting for the verification code.
type OTPSecretResult struct {
	Secret string
}

// DisableOTPResult is returned by DisableOTP after 2FA has been disabled.
type DisableOTPResult struct {
	PortalURL string
}

// StatusResult is returned by Status when the current token is valid.
type StatusResult struct {
	PortalURL string
}

// AuthService provides authentication operations.
// This interface allows mocking in tests.
type AuthService interface {
	// LoginCheck validates credentials and returns login result (including intermediate JWT for 2FA).
	LoginCheck(ctx context.Context, email, password string) (*portalsdk.LoginResult, error)

	// CompleteLogin completes authentication with a final JWT token.
	CompleteLogin(ctx context.Context, token, keyName string, noCreateKey bool) (*LoginCompleteResult, error)

	// LoginWithOTP completes 2FA authentication using an intermediate JWT and OTP code.
	LoginWithOTP(ctx context.Context, intermediateJWT, otp, keyName string, noCreateKey bool) (*LoginCompleteResult, error)

	// Register creates a new user account.
	Register(ctx context.Context, email, firstName, lastName, password string) (*RegisterResult, error)

	// SaveToken saves a JWT token to the config.
	SaveToken(token string) (*SaveTokenResult, error)

	// GetAPIEndpoint returns the configured API endpoint.
	GetAPIEndpoint() string

	// GenerateOTPSecret generates an OTP secret for enabling 2FA.
	GenerateOTPSecret(ctx context.Context) (*OTPSecretResult, error)

	// VerifyOTP verifies an OTP code against the newly generated secret.
	VerifyOTP(ctx context.Context, otpCode string) error

	// DisableOTP disables two-factor authentication for the account.
	DisableOTP(ctx context.Context, password string) (*DisableOTPResult, error)

	// Status checks if the current auth token is valid.
	Status(ctx context.Context) (*StatusResult, error)

	// GetAuthenticatedClient returns an authenticated account client.
	// If the stored token is an API key JWT, it exchanges it for a login JWT.
	GetAuthenticatedClient(ctx context.Context) (portalsdk.AccountAPI, error)

	// GetLoginToken returns a login JWT token, exchanging an API key if needed.
	GetLoginToken(ctx context.Context) (string, error)
}

// AuthServiceOption configures an AuthService.
type AuthServiceOption func(*AuthServiceDefault)

// WithAuthAccountClient sets a custom account client for the auth service (useful for testing).
func WithAuthAccountClient(client portalsdk.AccountAPI) AuthServiceOption {
	return func(s *AuthServiceDefault) {
		s.accountClient = client
	}
}

// WithAuthToken pins the auth service to an explicit auth token, taking
// precedence over the config-derived token in GetAuthenticatedClient. This
// lets the per-invocation --auth-token flag override the stored config token.
func WithAuthToken(token string) AuthServiceOption {
	return func(s *AuthServiceDefault) {
		s.authToken = token
	}
}

// WithClientFactory sets a custom client factory for creating authenticated clients.
func WithClientFactory(factory ClientFactory) AuthServiceOption {
	return func(s *AuthServiceDefault) {
		s.clientFactory = factory
	}
}

// WithLogger sets a custom zap logger. A nil logger falls back to zap.NewNop().
func WithLogger(logger *zap.Logger) AuthServiceOption {
	return func(s *AuthServiceDefault) {
		if logger != nil {
			s.log = logger
		}
	}
}

// AuthServiceDefault handles authentication business logic.
// It separates CLI concerns from authentication operations.
type AuthServiceDefault struct {
	accountClient portalsdk.AccountAPI
	configMgr     config.Manager
	log           *zap.Logger
	apiEndpoint   string
	clientFactory ClientFactory
	// authToken, when set via WithAuthToken, overrides the config token used
	// to build an authenticated account client.
	authToken string
}

// NewAuthService creates a new AuthService with the given dependencies.
// An account client will be created if not provided via WithAuthAccountClient option.
// A nil logger is treated as a no-op logger.
func NewAuthService(configMgr config.Manager, apiEndpoint string, logger *zap.Logger, opts ...AuthServiceOption) AuthService {
	s := &AuthServiceDefault{
		configMgr:     configMgr,
		log:           logger,
		apiEndpoint:   apiEndpoint,
		clientFactory: defaultClientFactory,
	}
	if s.log == nil {
		s.log = zap.NewNop()
	}

	// Apply options
	for _, opt := range opts {
		opt(s)
	}

	// Create default account client if not provided
	if s.accountClient == nil {
		s.accountClient = portalsdk.NewClient(portalsdk.WithEndpoint(apiEndpoint))
	}

	return s
}

// DefaultAuthServiceFactory creates an auth service using the default
// dependencies. A nil logger yields a no-op logger.
func DefaultAuthServiceFactory(configMgr config.Manager, apiEndpoint string) AuthService {
	return NewAuthService(configMgr, apiEndpoint, nil)
}

// LoginCheck validates credentials and returns login result.
// This separates credential validation from authentication completion.
func (s *AuthServiceDefault) LoginCheck(ctx context.Context, email, password string) (*portalsdk.LoginResult, error) {
	s.log.Debug("using api endpoint", zap.String("endpoint", s.apiEndpoint))

	loginResult, err := s.accountClient.Login(ctx, email, password)
	if err != nil {
		return nil, fmt.Errorf("login failed: %w", err)
	}

	return loginResult, nil
}

// CompleteLogin completes authentication with a final JWT token.
func (s *AuthServiceDefault) CompleteLogin(ctx context.Context, token, keyName string, noCreateKey bool) (*LoginCompleteResult, error) {
	if noCreateKey {
		res, err := s.SaveToken(token)
		if err != nil {
			return nil, err
		}
		return &LoginCompleteResult{
			APIKeyStatus: APIKeyNone,
			ConfigPath:   res.ConfigPath,
			PortalURL:    res.PortalURL,
		}, nil
	}

	// Check if we can reuse an existing API key for the same account
	if s.canReuseAPIKey(ctx, token) {
		return s.buildCompleteResult("", APIKeyReused), nil
	}

	authClient := s.clientFactory(s.apiEndpoint, token)
	apiKey, err := s.createOrReplaceAPIKey(ctx, authClient, keyName)
	if err != nil {
		return nil, err
	}

	if err := s.configMgr.SetAuthToken(apiKey.Token); err != nil {
		return nil, fmt.Errorf("failed to save API key: %w", err)
	}

	return s.buildCompleteResult(apiKey.Name, APIKeyCreated), nil
}

// LoginWithOTP completes 2FA authentication using an intermediate JWT and OTP code.
func (s *AuthServiceDefault) LoginWithOTP(ctx context.Context, intermediateJWT, otp, keyName string, noCreateKey bool) (*LoginCompleteResult, error) {
	s.log.Debug("using api endpoint", zap.String("endpoint", s.apiEndpoint))

	finalToken, err := s.accountClient.ValidateOTP(ctx, intermediateJWT, otp)
	if err != nil {
		return nil, fmt.Errorf("OTP validation failed: %w", err)
	}

	if noCreateKey {
		res, err := s.SaveToken(finalToken)
		if err != nil {
			return nil, err
		}
		return &LoginCompleteResult{
			APIKeyStatus: APIKeyNone,
			ConfigPath:   res.ConfigPath,
			PortalURL:    res.PortalURL,
		}, nil
	}

	// Check if we can reuse an existing API key for the same account
	if s.canReuseAPIKey(ctx, finalToken) {
		return s.buildCompleteResult("", APIKeyReused), nil
	}

	authClient := s.clientFactory(s.apiEndpoint, finalToken)
	apiKey, err := s.createOrReplaceAPIKey(ctx, authClient, keyName)
	if err != nil {
		return nil, err
	}

	if err := s.configMgr.SetAuthToken(apiKey.Token); err != nil {
		return nil, fmt.Errorf("failed to save API key: %w", err)
	}

	return s.buildCompleteResult(apiKey.Name, APIKeyCreated), nil
}

// buildCompleteResult assembles the LoginCompleteResult from config state.
func (s *AuthServiceDefault) buildCompleteResult(apiKeyName string, status APIKeyStatus) *LoginCompleteResult {
	return &LoginCompleteResult{
		APIKeyName:   apiKeyName,
		APIKeyStatus: status,
		ConfigPath:   s.configMgr.ConfigPath(),
		PortalURL:    s.configMgr.Config().GetBaseEndpointSecure(),
	}
}

// canReuseAPIKey checks whether the stored API key belongs to the same account
// as the given login JWT and is still valid. Returns true if the existing key
// can be reused (same user ID + ping succeeds), false otherwise.
func (s *AuthServiceDefault) canReuseAPIKey(ctx context.Context, loginJWT string) bool {
	cfg := s.configMgr.Config()
	storedToken := cfg.AuthToken
	if storedToken == "" {
		return false
	}

	// The stored token must be an API key JWT
	storedPurpose, err := GetJWTPurpose(storedToken)
	if err != nil || storedPurpose != "api" {
		return false
	}

	// Extract user ID (subject) from both tokens
	loginSub, err := GetJWTSubject(loginJWT)
	if err != nil || loginSub == "" {
		s.log.Debug("could not extract user id from login jwt", zap.Error(err))
		return false
	}

	storedSub, err := GetJWTSubject(storedToken)
	if err != nil || storedSub == "" {
		s.log.Debug("could not extract user id from stored api key jwt", zap.Error(err))
		return false
	}

	// Different user ID means different account; cannot reuse
	if loginSub != storedSub {
		s.log.Debug("stored api key belongs to different account, creating new key",
			zap.String("storedSub", storedSub), zap.String("loginSub", loginSub))
		return false
	}

	// Same account; verify the key still works by pinging the API
	authClient := s.clientFactory(s.apiEndpoint, storedToken)
	if err := authClient.Ping(ctx); err != nil {
		s.log.Debug("stored api key is no longer valid", zap.Error(err))
		return false
	}

	return true
}

// GetJWTSubject extracts the subject (user ID) from a JWT token.
func GetJWTSubject(token string) (string, error) {
	parser := jwt.NewParser(jwt.WithoutClaimsValidation())
	parsedToken, _, err := parser.ParseUnverified(token, &jwt.RegisteredClaims{})
	if err != nil {
		return "", err
	}

	claims, ok := parsedToken.Claims.(*jwt.RegisteredClaims)
	if !ok {
		return "", fmt.Errorf("invalid claims type")
	}

	return claims.Subject, nil
}

// createOrReplaceAPIKey deletes any existing API key with the same name, then creates a new one.
func (s *AuthServiceDefault) createOrReplaceAPIKey(ctx context.Context, client portalsdk.AccountAPI, keyName string) (*portalsdk.APIKey, error) {
	keys, _, err := client.ListAPIKeys(ctx, portalsdk.WithSearch(keyName))
	if err != nil {
		return nil, fmt.Errorf("failed to list existing API keys: %w", err)
	}

	for _, key := range keys {
		if key.Name == keyName {
			if delErr := client.DeleteAPIKey(ctx, key.Uuid.String()); delErr != nil {
				return nil, fmt.Errorf("failed to delete existing API key '%s': %w", key.Name, delErr)
			}
			s.log.Debug("deleted existing api key", zap.String("name", key.Name), zap.String("uuid", key.Uuid.String()))
		}
	}

	apiKey, err := client.CreateAPIKey(ctx, keyName)
	if err != nil {
		return nil, fmt.Errorf("failed to create API key: %w", err)
	}

	return apiKey, nil
}

// Register creates a new user account.
func (s *AuthServiceDefault) Register(ctx context.Context, email, firstName, lastName, password string) (*RegisterResult, error) {
	s.log.Debug("using api endpoint", zap.String("endpoint", s.apiEndpoint))

	err := s.accountClient.Register(ctx, email, firstName, lastName, password)
	if err != nil {
		return nil, fmt.Errorf("registration failed: %w", err)
	}

	return &RegisterResult{Email: email}, nil
}

// SaveToken saves a JWT token to the config.
func (s *AuthServiceDefault) SaveToken(token string) (*SaveTokenResult, error) {
	if err := s.configMgr.SetAuthToken(token); err != nil {
		return nil, fmt.Errorf("failed to save auth token: %w", err)
	}

	return &SaveTokenResult{
		ConfigPath: s.configMgr.ConfigPath(),
		PortalURL:  s.configMgr.Config().GetBaseEndpointSecure(),
	}, nil
}

// GetAPIEndpoint returns the configured API endpoint.
func (s *AuthServiceDefault) GetAPIEndpoint() string {
	return s.apiEndpoint
}

// GenerateOTPSecret generates an OTP secret and returns it to the caller so
// the caller can display it before prompting for a verification code.
func (s *AuthServiceDefault) GenerateOTPSecret(ctx context.Context) (*OTPSecretResult, error) {
	s.log.Debug("using api endpoint", zap.String("endpoint", s.apiEndpoint))

	client, err := s.GetAuthenticatedClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("authentication failed: %w", err)
	}

	secret, err := client.GenerateOTP(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to generate OTP secret: %w", err)
	}

	return &OTPSecretResult{Secret: secret}, nil
}

// VerifyOTP verifies an OTP code against the newly generated secret to enable 2FA.
func (s *AuthServiceDefault) VerifyOTP(ctx context.Context, otpCode string) error {
	client, err := s.GetAuthenticatedClient(ctx)
	if err != nil {
		return fmt.Errorf("authentication failed: %w", err)
	}

	if err := client.VerifyOTP(ctx, otpCode); err != nil {
		return fmt.Errorf("failed to verify OTP code: %w", err)
	}

	return nil
}

// DisableOTP disables two-factor authentication for the account.
func (s *AuthServiceDefault) DisableOTP(ctx context.Context, password string) (*DisableOTPResult, error) {
	s.log.Debug("using api endpoint", zap.String("endpoint", s.apiEndpoint))

	client, err := s.GetAuthenticatedClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("authentication failed: %w", err)
	}

	if err := client.DisableOTP(ctx, password); err != nil {
		return nil, fmt.Errorf("failed to disable 2FA: %w", err)
	}

	return &DisableOTPResult{
		PortalURL: s.configMgr.Config().GetBaseEndpointSecure(),
	}, nil
}

// Status checks if the current auth token is valid.
func (s *AuthServiceDefault) Status(ctx context.Context) (*StatusResult, error) {
	s.log.Debug("using api endpoint", zap.String("endpoint", s.apiEndpoint))

	client, err := s.GetAuthenticatedClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("authentication failed: %w", err)
	}

	if err := client.Ping(ctx); err != nil {
		return nil, fmt.Errorf("not authenticated: %w", err)
	}

	cfg := s.configMgr.Config()
	portalURL := cfg.GetBaseEndpointSecure()

	return &StatusResult{PortalURL: portalURL}, nil
}

// GetAuthenticatedClient returns an authenticated account client.
// If the stored token is an API key JWT, it uses LoginWithAPIKey to exchange it for a login JWT.
// If the stored token is a login JWT, it uses it directly.
func (s *AuthServiceDefault) GetAuthenticatedClient(ctx context.Context) (portalsdk.AccountAPI, error) {
	cfg := s.configMgr.Config()
	// The per-invocation --auth-token override (WithAuthToken) takes precedence
	// over the config-stored token.
	token := cfg.AuthToken
	if s.authToken != "" {
		token = s.authToken
	}

	if token == "" {
		return nil, coreerrors.ErrNotAuthenticated
	}

	// Check if the token is an API key JWT by decoding its claims
	purpose, err := GetJWTPurpose(token)
	if err != nil {
		s.log.Debug("could not decode jwt to determine purpose, treating as login token", zap.Error(err))
		return s.clientFactory(s.apiEndpoint, token), nil
	}

	if purpose == "api" {
		s.log.Debug("detected api key jwt, exchanging for login token")
		jwtToken, err := s.accountClient.LoginWithAPIKey(ctx, token)
		if err != nil {
			return nil, fmt.Errorf("failed to authenticate with API key: %w", err)
		}
		return s.clientFactory(s.apiEndpoint, jwtToken), nil
	}

	s.log.Debug("using jwt token for authentication", zap.String("purpose", purpose))
	return s.clientFactory(s.apiEndpoint, token), nil
}

// GetLoginToken returns a login JWT token, exchanging an API key JWT if needed.
func (s *AuthServiceDefault) GetLoginToken(ctx context.Context) (string, error) {
	cfg := s.configMgr.Config()
	token := cfg.AuthToken

	if token == "" {
		return "", coreerrors.ErrNotAuthenticated
	}

	purpose, err := GetJWTPurpose(token)
	if err != nil {
		s.log.Debug("could not decode jwt to determine purpose, treating as login token", zap.Error(err))
		return token, nil
	}

	if purpose == "api" {
		s.log.Debug("detected api key jwt, exchanging for login token")
		jwtToken, err := s.accountClient.LoginWithAPIKey(ctx, token)
		if err != nil {
			return "", fmt.Errorf("failed to authenticate with API key: %w", err)
		}
		return jwtToken, nil
	}

	return token, nil
}

// GetJWTPurpose extracts the purpose from a JWT token's audience claim.
// Returns the audience value or empty string if decoding fails.
// API keys have audience="api", login tokens have audience="login".
func GetJWTPurpose(token string) (string, error) {
	// Parse without verification to just read the claims
	parser := jwt.NewParser(jwt.WithoutClaimsValidation())
	parsedToken, _, err := parser.ParseUnverified(token, &jwt.RegisteredClaims{})
	if err != nil {
		return "", err
	}

	claims, ok := parsedToken.Claims.(*jwt.RegisteredClaims)
	if !ok {
		return "", fmt.Errorf("invalid claims type")
	}

	// Return the first audience value (we typically only have one)
	if len(claims.Audience) > 0 {
		return claims.Audience[0], nil
	}

	return "", nil
}
