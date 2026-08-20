package wizard

import (
	"fmt"

	"go.lumeweb.com/pinner-cli/internal/core/config"
)

// The setup wizard's base_endpoint / secure config pair is the one step input
// set that genuinely overlaps a persistent CLI surface: the same two keys are
// read and written by the CLI config path (config.go, doctor.go, setup_pterm.go)
// and by the MCP setup config step. They are declared here once so the key
// names and write semantics stay in a single place shared by both surfaces,
// rather than the MCP handler duplicating them ad hoc.
const (
	// configBaseEndpointKey is the configmanager key for the API base endpoint.
	configBaseEndpointKey = "base_endpoint"
	// configSecureKey is the configmanager key for whether HTTPS is enforced.
	configSecureKey = "secure"
)

// applySetupConfig applies a SetupConfigInput decision to the persistent CLI
// config. It is the write half of the setup config step:
//
//   - use_defaults  resets the endpoint and restores HTTPS enforcement
//   - custom_endpoint persists the supplied endpoint + secure flag
//   - skip          leaves existing configuration untouched
//
// The schema contract (SetupConfigInput) stays a step-local form body; this
// helper is the typed write that both the MCP wizard and any future CLI gather
// consumer route through, so the two surfaces cannot drift on key names or
// default behavior.
func applySetupConfig(cfg config.Manager, in SetupConfigInput) error {
	switch in.Choice {
	case ConfigChoiceDefaults:
		if err := cfg.SetBaseEndpoint(""); err != nil {
			return fmt.Errorf("failed to reset endpoint: %w", err)
		}
		if err := cfg.SetSecure(true); err != nil {
			return fmt.Errorf("failed to set secure: %w", err)
		}
	case ConfigChoiceSkip:
		// Skip: preserve existing configuration.
	case ConfigChoiceCustom:
		if in.Endpoint == "" {
			return fmt.Errorf("endpoint is required for custom_endpoint choice")
		}
		if err := cfg.SetBaseEndpoint(in.Endpoint); err != nil {
			return fmt.Errorf("failed to set endpoint: %w", err)
		}
		if err := cfg.SetSecure(in.Secure); err != nil {
			return fmt.Errorf("failed to set secure: %w", err)
		}
	}
	return nil
}
