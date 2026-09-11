package apps

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestOpenLauncherDescriptionPinned pins the rendered launcher-description
// skeleton so the shared boilerplate stays byte-stable across every open_*
// launcher registration (the DRY fix routes all registrations through
// OpenLauncherDescription / OpenLauncherDescriptionBody instead of each file
// hand-copying the wording).
func TestOpenLauncherDescriptionPinned(t *testing.T) {
	// Default skeleton (no mid-description body) — the shape used by the
	// per-app launcher registrations in package mcp.
	require.Equal(t,
		"Open the interactive Create a Pin app. This is a UI launcher: it renders an iframe for a human to enter a CID and pin it. "+
			"It is not a headless primitive; the headless equivalent is pins_add for autonomous pin creation without a rendered form.",
		OpenLauncherDescription("Create a Pin app", "enter a CID and pin it",
			"pins_add for autonomous pin creation without a rendered form"))

	// With-context variant (the upload-package launchers insert app-specific
	// prose between the headless sentence and the tail).
	require.Equal(t,
		"Open the interactive Upload to Vault file picker. This is a UI launcher: it renders an iframe for a human to pick a file. "+
			"It is not a headless primitive. Returns a presigned PUT URL plus the vault_path. "+
			"The headless equivalent is vault_put_file for autonomous uploads without a rendered file picker.",
		OpenLauncherDescriptionBody("Upload to Vault file picker", "pick a file",
			"Returns a presigned PUT URL plus the vault_path.",
			"vault_put_file for autonomous uploads without a rendered file picker"))
}
