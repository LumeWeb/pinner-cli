package cli

import (
	"context"
	"fmt"

	"github.com/golang-jwt/jwt/v5"
	"go.lumeweb.com/pinner-cli/pkg/config"
	portalsdk "go.lumeweb.com/portal-sdk"
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

// AuthService provides authentication operations.
// This interface allows mocking in tests.
type AuthService interface {
	// LoginCheck validates credentials and returns login result (including intermediate JWT for 2FA).
	LoginCheck(ctx context.Context, email, password string) (*portalsdk.LoginResult, error)

	// CompleteLogin completes authentication with a final JWT token.
	CompleteLogin(ctx context.Context, token, keyName string, noCreateKey bool) error

	// LoginWithOTP completes 2FA authentication using an intermediate JWT and OTP code.
	LoginWithOTP(ctx context.Context, intermediateJWT, otp, keyName string, noCreateKey bool) error

	// Register creates a new user account.
	Register(ctx context.Context, email, firstName, lastName, password string) error

	// SaveToken saves a JWT token to the config.
	SaveToken(token string) error

	// GetAPIEndpoint returns the configured API endpoint.
	GetAPIEndpoint() string

	// EnableOTP enables two-factor authentication for the account.
	EnableOTP(ctx context.Context, otpCode string) error

	// DisableOTP disables two-factor authentication for the account.
	DisableOTP(ctx context.Context, password string) error

	// Status checks if the current auth token is valid.
	Status(ctx context.Context) error

	// GetAuthenticatedClient returns an authenticated account client.
	// If the stored token is an API key JWT, it exchanges it for a login JWT.
	GetAuthenticatedClient(ctx context.Context) (portalsdk.AccountAPI, error)
}

// AuthServiceOption configures an AuthService.
type AuthServiceOption func(*AuthServiceDefault)

// WithAuthAccountClient sets a custom account client for the auth service (useful for testing).
func WithAuthAccountClient(client portalsdk.AccountAPI) AuthServiceOption {
	return func(s *AuthServiceDefault) {
		s.accountClient = client
	}
}

// WithClientFactory sets a custom client factory for creating authenticated clients.
func WithClientFactory(factory ClientFactory) AuthServiceOption {
	return func(s *AuthServiceDefault) {
		s.clientFactory = factory
	}
}

// WithPrompter sets a custom auth prompter (useful for testing).
func WithPrompter(prompter AuthPrompter) AuthServiceOption {
	return func(s *AuthServiceDefault) {
		s.prompter = prompter
	}
}

// AuthServiceDefault handles authentication business logic.
// It separates CLI concerns from authentication operations.
type AuthServiceDefault struct {
	accountClient portalsdk.AccountAPI
	configMgr     config.Manager
	output        Output
	apiEndpoint   string
	clientFactory ClientFactory
	prompter      AuthPrompter
}

// NewAuthService creates a new AuthService with the given dependencies.
// An account client will be created if not provided via WithAuthAccountClient option.
func NewAuthService(configMgr config.Manager, output Output, apiEndpoint string, opts ...AuthServiceOption) AuthService {
	s := &AuthServiceDefault{
		configMgr:     configMgr,
		output:        output,
		apiEndpoint:   apiEndpoint,
		clientFactory: defaultClientFactory,
		prompter:      &promptuiPrompter{},
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

// LoginCheck validates credentials and returns login result.
// This separates credential validation from authentication completion.
func (s *AuthServiceDefault) LoginCheck(ctx context.Context, email, password string) (*portalsdk.LoginResult, error) {
	s.output.PrintVerbosef("Using API endpoint: %s", s.apiEndpoint)

	loginResult, err := s.accountClient.Login(ctx, email, password)
	if err != nil {
		return nil, fmt.Errorf("login failed: %w", err)
	}

	if loginResult.OTPRequired {
		s.output.Print("Two-factor authentication required.")
	}

	return loginResult, nil
}

// CompleteLogin completes authentication with a final JWT token.
func (s *AuthServiceDefault) CompleteLogin(ctx context.Context, token, keyName string, noCreateKey bool) error {
	if noCreateKey {
		// Save JWT directly without creating API key
		return s.SaveToken(token)
	}

	// Create API key with authenticated client
	authClient := s.clientFactory(s.apiEndpoint, token)
	apiKey, err := authClient.CreateAPIKey(ctx, keyName)
	if err != nil {
		return fmt.Errorf("failed to create API key: %w", err)
	}

	// Save API key to config
	if err := s.configMgr.SetAuthToken(apiKey.Token); err != nil {
		return fmt.Errorf("failed to save API key: %w", err)
	}

	// Output authentication success with JSON support
	printAuthSuccess(s.output, s.configMgr, apiKey.Name, true)
	return nil
}

// LoginWithOTP completes 2FA authentication using an intermediate JWT and OTP code.
func (s *AuthServiceDefault) LoginWithOTP(ctx context.Context, intermediateJWT, otp, keyName string, noCreateKey bool) error {
	s.output.PrintVerbosef("Using API endpoint: %s", s.apiEndpoint)

	finalToken, err := s.accountClient.ValidateOTP(ctx, intermediateJWT, otp)
	if err != nil {
		return fmt.Errorf("OTP validation failed: %w", err)
	}

	if noCreateKey {
		// Save JWT directly without creating API key
		return s.SaveToken(finalToken)
	}

	// Create API key with authenticated client
	authClient := s.clientFactory(s.apiEndpoint, finalToken)
	apiKey, err := authClient.CreateAPIKey(ctx, keyName)
	if err != nil {
		return fmt.Errorf("failed to create API key: %w", err)
	}

	// Save API key to config
	if err := s.configMgr.SetAuthToken(apiKey.Token); err != nil {
		return fmt.Errorf("failed to save API key: %w", err)
	}

	// Output authentication success with JSON support
	printAuthSuccess(s.output, s.configMgr, apiKey.Name, true)
	return nil
}

// Register creates a new user account.
func (s *AuthServiceDefault) Register(ctx context.Context, email, firstName, lastName, password string) error {
	s.output.PrintVerbosef("Using API endpoint: %s", s.apiEndpoint)

	err := s.accountClient.Register(ctx, email, firstName, lastName, password)
	if err != nil {
		return fmt.Errorf("registration failed: %w", err)
	}

	s.output.Print("Registration successful!")
	s.output.Printf("A verification email has been sent to %s", email)
	s.output.Print("Please check your email and confirm your account.")
	return nil
}

// SaveToken saves a JWT token to the config.
func (s *AuthServiceDefault) SaveToken(token string) error {
	if err := s.configMgr.SetAuthToken(token); err != nil {
		return fmt.Errorf("failed to save auth token: %w", err)
	}

	// Output authentication success with JSON support (no API key created)
	printAuthSuccess(s.output, s.configMgr, "", false)
	return nil
}

// GetAPIEndpoint returns the configured API endpoint.
func (s *AuthServiceDefault) GetAPIEndpoint() string {
	return s.apiEndpoint
}

// EnableOTP enables two-factor authentication for the account.
func (s *AuthServiceDefault) EnableOTP(ctx context.Context, otpCode string) error {
	s.output.PrintVerbosef("Using API endpoint: %s", s.apiEndpoint)

	client, err := s.GetAuthenticatedClient(ctx)
	if err != nil {
		return fmt.Errorf("authentication failed: %w", err)
	}

	secret, err := client.GenerateOTP(ctx)
	if err != nil {
		return fmt.Errorf("failed to generate OTP secret: %w", err)
	}

	s.output.Print("Two-factor authentication setup")
	s.output.Printf("Your OTP secret: %s", secret)
	s.output.Print("Add this secret to your authenticator app (e.g., Google Authenticator, Authy)")

	// If OTP code not provided, prompt for it
	if otpCode == "" {
		otpCode, err = s.prompter.PromptOTP()
		if err != nil {
			return fmt.Errorf("failed to read OTP code: %w", err)
		}
	}

	err = client.VerifyOTP(ctx, otpCode)
	if err != nil {
		return fmt.Errorf("failed to verify OTP code: %w", err)
	}

	s.output.Print("Two-factor authentication enabled successfully!")
	return nil
}

// DisableOTP disables two-factor authentication for the account.
func (s *AuthServiceDefault) DisableOTP(ctx context.Context, password string) error {
	s.output.PrintVerbosef("Using API endpoint: %s", s.apiEndpoint)

	// If password not provided, prompt for it
	if password == "" {
		var err error
		password, err = s.prompter.Password("Password")
		if err != nil {
			return fmt.Errorf("failed to read password: %w", err)
		}
	}

	client, err := s.GetAuthenticatedClient(ctx)
	if err != nil {
		return fmt.Errorf("authentication failed: %w", err)
	}

	if err := client.DisableOTP(ctx, password); err != nil {
		return fmt.Errorf("failed to disable 2FA: %w", err)
	}

	s.output.Print("Two-factor authentication disabled successfully.")
	return nil
}

// Status checks if the current auth token is valid.
func (s *AuthServiceDefault) Status(ctx context.Context) error {
	s.output.PrintVerbosef("Using API endpoint: %s", s.apiEndpoint)

	client, err := s.GetAuthenticatedClient(ctx)
	if err != nil {
		return fmt.Errorf("authentication failed: %w", err)
	}

	if err := client.Ping(ctx); err != nil {
		return fmt.Errorf("not authenticated: %w", err)
	}

	cfg := s.configMgr.Config()
	portalURL := cfg.GetBaseEndpointSecure()

	// Output authentication status with portal URL
	printAuthStatus(s.output, portalURL)
	return nil
}

// GetAuthenticatedClient returns an authenticated account client.
// If the stored token is an API key JWT, it uses LoginWithAPIKey to exchange it for a login JWT.
// If the stored token is a login JWT, it uses it directly.
func (s *AuthServiceDefault) GetAuthenticatedClient(ctx context.Context) (portalsdk.AccountAPI, error) {
	cfg := s.configMgr.Config()
	token := cfg.AuthToken

	if token == "" {
		return nil, config.ErrNotAuthenticated
	}

	// Check if the token is an API key JWT by decoding its claims
	purpose, err := GetJWTPurpose(token)
	if err != nil {
		s.output.PrintVerbosef("Could not decode JWT to determine purpose, treating as login token: %v", err)
		return s.clientFactory(s.apiEndpoint, token), nil
	}

	if purpose == "api" {
		s.output.PrintVerbose("Detected API key JWT, exchanging for login token")
		jwtToken, err := s.accountClient.LoginWithAPIKey(ctx, token)
		if err != nil {
			return nil, fmt.Errorf("failed to authenticate with API key: %w", err)
		}
		return s.clientFactory(s.apiEndpoint, jwtToken), nil
	}

	s.output.PrintVerbosef("Using JWT token for authentication (purpose: %s)", purpose)
	return s.clientFactory(s.apiEndpoint, token), nil
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

// printAuthSuccess outputs authentication success message with config path and portal URL.
// Supports both human-readable and JSON output formats.
func printAuthSuccess(output Output, configMgr config.Manager, apiKeyName string, apiKeyCreated bool) {
	configPath := configMgr.ConfigPath()
	portalURL := configMgr.Config().GetBaseEndpointSecure()

	// For JSON output, provide a structured response
	if output.IsJSON() {
		result := map[string]any{
			"status":     "authenticated",
			"configPath": configPath,
			"portalURL":  portalURL,
		}
		if apiKeyCreated {
			result["apiKeyName"] = apiKeyName
			result["message"] = fmt.Sprintf("Authentication successful! API key '%s' created and saved to config.", apiKeyName)
		} else {
			result["message"] = "Authentication successful! Token saved to config."
		}
		_ = output.PrintJSON(result)
		return
	}

	// Human-readable output
	output.Print("Authentication successful!")
	if apiKeyCreated {
		output.Printfln("API key '%s' created and saved to config.", apiKeyName)
	} else {
		output.Print("Token saved to config.")
	}
	output.Printfln("Config file: %s", configPath)
	output.Printfln("Portal URL: %s", portalURL)
}

// printAuthStatus outputs authentication status with portal URL.
// Supports both human-readable and JSON output formats.
func printAuthStatus(output Output, portalURL string) {
	// For JSON output, provide a structured response
	if output.IsJSON() {
		result := map[string]any{
			"status":    "authenticated",
			"portalURL": portalURL,
			"message":   "Authentication status: authenticated",
		}
		_ = output.PrintJSON(result)
		return
	}

	// Human-readable output
	output.Print("Authentication status: authenticated")
	output.Printfln("Portal: %s", portalURL)
}
