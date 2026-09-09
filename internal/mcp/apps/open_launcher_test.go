package apps

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.lumeweb.com/mcpplane/model"
)

// TestNewOpenLauncherDescriptorRejectsUnmarshalableMeta pins the Stage-5
// de-fork behavior: NewOpenLauncherDescriptor PROPAGATES the _meta.ui marshal
// failure (an empty ResourceURI) instead of silently registering a launcher
// with nil metadata — a nil-meta launcher would pass registration and then
// fail to render its app view with no error. Characterization introduced with
// the error-return signature; every launcher spec must carry its ui:// URI.
func TestNewOpenLauncherDescriptorRejectsUnmarshalableMeta(t *testing.T) {
	desc, err := NewOpenLauncherDescriptor(OpenLauncherSpec{
		Name: "open_missing_uri",
	})
	require.Error(t, err, "an empty ResourceURI must fail the descriptor build")
	require.Contains(t, err.Error(), "open_missing_uri")
	require.Contains(t, err.Error(), "ui:// resourceUri")
	// Mirror the module contract (mcpplane/apps TestNewOpenLauncherDescriptor
	// EmptyResourceURIFails): no half-built descriptor may leak to the caller.
	require.Equal(t, model.ToolDescriptor{}, desc)

	// A well-formed spec succeeds and carries the marshaled app meta.
	desc, err = NewOpenLauncherDescriptor(OpenLauncherSpec{
		Name:        "open_ok",
		ResourceURI: "ui://test/ok.html",
	})
	require.NoError(t, err)
	require.NotNil(t, desc.Meta)
}
