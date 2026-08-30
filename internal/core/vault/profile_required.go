package vault

import (
	"fmt"
	"strings"
)

// ProfileRequiredError is returned when a multi-profile server receives a vault
// write/mint without an explicit profile. It is a structured error (code
// "profile_required"), not the silent/active-vault default and not a misleading
// later HTTP 500. Both the catalog vault ops and the vault_put_file descriptor
// surface it consistently so no surface mints a one-shot upload URL it cannot
// use.
type ProfileRequiredError struct {
	Code     string   `json:"code"` // "profile_required"
	Profiles []string `json:"profiles,omitempty"`
	Message  string   `json:"message"`
}

func (e *ProfileRequiredError) Error() string {
	return e.Message
}

// ProfileRequired reports whether a profile-scoped vault op must be given an
// explicit profile. It returns a *ProfileRequiredError when profileArg is empty
// AND more than one provisioned (app-key readable) profile is unlocked;
// otherwise nil. It is the shared guard used across the catalog vault ops and
// the vault_put_file mint/relay path so every surface agrees on the unlocked
// set and the required-profile rule.
func ProfileRequired(profileArg string) *ProfileRequiredError {
	if profileArg != "" {
		return nil
	}
	profiles := ProvisionedProfileNames()
	if len(profiles) > 1 {
		return &ProfileRequiredError{
			Code:     "profile_required",
			Profiles: profiles,
			Message:  fmt.Sprintf("more than one vault profile is unlocked (%s); pass profile=<name>", strings.Join(profiles, ", ")),
		}
	}
	return nil
}

// ProvisionedProfileNames returns every provisioned (app-key readable) vault
// profile name the server can access. It mirrors the source used by the CLI
// wiring and the MCP sync loop so vault_profiles, the profile-required rule,
// and the per-profile flush manager all agree on the unlocked set.
func ProvisionedProfileNames() []string {
	reg, err := LoadRegistry()
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(reg.Profiles))
	for name := range reg.Profiles {
		// Guard against a hand-edited registry carrying a path traversal name
		// (the same check ResolveProfile applies).
		if err := ValidateProfileName(name); err != nil {
			continue
		}
		// Access = a provisioned profile with a readable app key.
		if _, ok := ProfileVaultID(name); ok {
			out = append(out, name)
		}
	}
	return out
}
