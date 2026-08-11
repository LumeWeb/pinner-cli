package cli

import (
	"fmt"

	"go.lumeweb.com/pinner-cli/internal/core/auth"
)

// renderLoginComplete renders a successful authentication result. It supports
// both human-readable and JSON output formats.
func renderLoginComplete(output Output, result *auth.LoginCompleteResult) {
	configPath := result.ConfigPath
	portalURL := result.PortalURL
	apiKeyName := result.APIKeyName

	// For JSON output, provide a structured response
	if output.IsJSON() {
		out := map[string]any{
			"status":     "authenticated",
			"configPath": configPath,
			"portalURL":  portalURL,
		}
		switch result.APIKeyStatus {
		case auth.APIKeyCreated:
			out["apiKeyName"] = apiKeyName
			out["message"] = fmt.Sprintf("Authentication successful! API key '%s' created and saved to config.", apiKeyName)
		case auth.APIKeyReused:
			if apiKeyName != "" {
				out["apiKeyName"] = apiKeyName
				out["message"] = fmt.Sprintf("Authentication successful! Reusing existing API key '%s'.", apiKeyName)
			} else {
				out["message"] = "Authentication successful! Reusing existing API key."
			}
		default:
			out["message"] = "Authentication successful! Token saved to config."
		}
		_ = output.PrintJSON(out)
		return
	}

	// Human-readable output
	output.Print("Authentication successful!")
	switch result.APIKeyStatus {
	case auth.APIKeyCreated:
		output.Printfln("API key '%s' created and saved to config.", apiKeyName)
	case auth.APIKeyReused:
		if apiKeyName != "" {
			output.Printfln("Reusing existing API key '%s'.", apiKeyName)
		} else {
			output.Print("Reusing existing API key.")
		}
	default:
		output.Print("Token saved to config.")
	}
	output.Printfln("Config file: %s", configPath)
	output.Printfln("Portal URL: %s", portalURL)
}

// renderSaveToken renders a token-save result (no API key created).
func renderSaveToken(output Output, result *auth.SaveTokenResult) {
	renderLoginComplete(output, &auth.LoginCompleteResult{
		APIKeyName:   "",
		APIKeyStatus: auth.APIKeyNone,
		ConfigPath:   result.ConfigPath,
		PortalURL:    result.PortalURL,
	})
}

// renderRegister renders the registration success messages.
func renderRegister(output Output, result *auth.RegisterResult) {
	output.Print("Registration successful!")
	output.Printfln("A verification email has been sent to %s", result.Email)
	output.Print("Please check your email and confirm your account.")
}

// renderAuthStatus renders the auth status result.
func renderAuthStatus(output Output, result *auth.StatusResult) {
	portalURL := result.PortalURL

	// For JSON output, provide a structured response
	if output.IsJSON() {
		out := map[string]any{
			"status":    "authenticated",
			"portalURL": portalURL,
			"message":   "Authentication status: authenticated",
		}
		_ = output.PrintJSON(out)
		return
	}

	// Human-readable output
	output.Print("Authentication status: authenticated")
	output.Printfln("Portal: %s", portalURL)
}

// renderOTPSecret renders the 2FA setup instructions with the OTP secret.
func renderOTPSecret(output Output, secret string) {
	output.Print("Two-factor authentication setup")
	output.Printfln("Your OTP secret: %s", secret)
	output.Print("Add this secret to your authenticator app (e.g., Google Authenticator, Authy)")
}

// renderOTPEnabled renders the 2FA-enabled success message.
func renderOTPEnabled(output Output) {
	output.Print("Two-factor authentication enabled successfully!")
}

// renderOTPDisabled renders the 2FA-disabled success message.
func renderOTPDisabled(output Output) {
	output.Print("Two-factor authentication disabled successfully.")
}
