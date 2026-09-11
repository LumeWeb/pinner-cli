package upload

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"go.lumeweb.com/mcpplane/model"

	corevault "go.lumeweb.com/pinner/core/vault"

	"go.lumeweb.com/pinner-cli/internal/mcp/core/transfer"
)

// swapProfileRequired installs a stubbed vault profile guard and restores the
// registry-backed default on test teardown.
func swapProfileRequired(t *testing.T, fn func(string) *corevault.ProfileRequiredError) {
	t.Helper()
	prev := vaultProfileRequired
	vaultProfileRequired = fn
	t.Cleanup(func() { vaultProfileRequired = prev })
}

// twoProfileRequired stubs the vault profile registry so a mint without an
// explicit profile fails with a structured profile_required error, mirroring
// corevault.ProfileRequired on a multi-profile server.
func twoProfileRequired() func(string) *corevault.ProfileRequiredError {
	return func(profile string) *corevault.ProfileRequiredError {
		if profile != "" {
			return nil
		}
		return &corevault.ProfileRequiredError{
			Code:     "profile_required",
			Profiles: []string{"personal", "work"},
			Message:  "more than one vault profile is unlocked (personal, work); pass profile=<name>",
		}
	}
}

// TestOpenVaultManagerMintCarriesProfileIdentity pins the HIGH correctness fix:
// on a multi-profile server the launcher's mint WITHOUT an explicit profile is
// rejected up front with the structured profile_required error (never a silent
// active-profile default), and WITH a profile the minted endpoint's sealed
// metadata names the destination profile (plus the src=mcp stamp).
func TestOpenVaultManagerMintCarriesProfileIdentity(t *testing.T) {
	vu := transfer.NewVaultHTTPUpload(func(ctx context.Context, r io.Reader, size int64, vaultPath string, meta map[string]any) (any, error) {
		require.Equal(t, "work", meta[corevault.MetaKeyProfile], "vault write must receive the requested profile")
		require.Equal(t, "mcp", meta[corevault.MetaKeySrc])
		return map[string]any{"status": "staged"}, nil
	}, 1<<20)
	defer vu.Stop(context.Background())

	desc := NewOpenVaultManagerDescriptor(vu)

	t.Run("missing profile on multi-profile server errors", func(t *testing.T) {
		swapProfileRequired(t, twoProfileRequired())
		res, err := desc.Handler(context.Background(), model.ToolRequest{
			Arguments: map[string]any{"vault_path": "vault:/uploads/report.pdf"},
		})
		require.NoError(t, err)
		require.True(t, res.IsError)
		sc, ok := res.StructuredContent.(map[string]any)
		require.True(t, ok)
		require.Equal(t, "profile_required", sc["error"])
		require.ElementsMatch(t, []string{"personal", "work"}, sc["profiles"])
	})

	t.Run("explicit profile is minted through", func(t *testing.T) {
		swapProfileRequired(t, corevault.ProfileRequired)
		res, err := desc.Handler(context.Background(), model.ToolRequest{
			Arguments: map[string]any{"vault_path": "vault:/uploads/report.pdf", "profile": "work"},
		})
		require.NoError(t, err)
		require.False(t, res.IsError)
		sc, ok := res.StructuredContent.(map[string]any)
		require.True(t, ok)
		require.NotEmpty(t, sc["presigned_url"])

		putSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mux := http.NewServeMux()
			vu.RegisterHandlers(mux)
			mux.ServeHTTP(w, r)
		}))
		defer putSrv.Close()
		u, err := url.Parse(sc["presigned_url"].(string))
		require.NoError(t, err)
		req, err := http.NewRequest(http.MethodPut, putSrv.URL+u.Path, strings.NewReader("picked-file-bytes"))
		require.NoError(t, err)
		resp, err := putSrv.Client().Do(req)
		require.NoError(t, err)
		resp.Body.Close()
		require.Equal(t, http.StatusOK, resp.StatusCode)
	})
}

// TestVaultMintPinsSingleProfileAcrossRegistryChange is the determinism
// regression for finding: mint a vault presigned PUT while EXACTLY ONE
// profile is unlocked (so no explicit profile is required), then unlock a
// second profile BEFORE the browser does the presigned PUT. The mint-time
// pin (transfer.ResolveMintProfile) seals the resolved single profile into
// the minted metadata, so the PUT must land on the pinned profile (HTTP 200,
// no ambiguity re-resolution) — a put that re-resolves against the changed
// registry fails with the generic ambiguity error instead.
//
//   - the put closure mirrors the production CLI write closure (the profile
//     is stamped in metadata; an unstamped profile with more than one
//     unlocked profile fails the write) — pinning the profile at mint time
//     (open_vault_manager AND the vault_upload_submit app helper) is what
//     keeps the PUT succeeding against the profile that was unambiguous at
//     mint time;
//   - no put may succeed against a different or unknown profile.
func TestVaultMintPinsSingleProfileAcrossRegistryChange(t *testing.T) {
	// The registry as it stands when the caller mints: exactly one profile.
	unlocked := []string{"home"}
	prev := transfer.SetProvisionedVaultProfilesForTest(func() []string { return unlocked })
	t.Cleanup(func() { transfer.SetProvisionedVaultProfilesForTest(prev) })

	var sealedProfile string
	putSrv := func() *transfer.VaultHTTPUpload {
		return transfer.NewVaultHTTPUpload(func(ctx context.Context, r io.Reader, size int64, vaultPath string, meta map[string]any) (any, error) {
			p, _ := meta[corevault.MetaKeyProfile].(string)
			// Mirror the production write closure: an unstamped profile on
			// a registry that NOW has two unlocked profiles fails the PUT
			// after the bytes were staged (the generic, non-structured
			// failure this invariant removes).
			if p == "" && len(unlocked) > 1 {
				return nil, fmt.Errorf("profile_required: more than one vault profile is unlocked; list them with vault_profiles and pass profile=<name>")
			}
			sealedProfile = p
			return map[string]any{"status": "staged"}, nil
		}, 1<<20)
	}

	put := func(t *testing.T, vu *transfer.VaultHTTPUpload, presigned string) int {
		t.Helper()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mux := http.NewServeMux()
			vu.RegisterHandlers(mux)
			mux.ServeHTTP(w, r)
		}))
		defer srv.Close()
		u, err := url.Parse(presigned)
		require.NoError(t, err)
		req, err := http.NewRequest(http.MethodPut, srv.URL+u.Path, strings.NewReader("picked-file-bytes"))
		require.NoError(t, err)
		resp, err := srv.Client().Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		return resp.StatusCode
	}

	// The mint guard mirrors the SAME registry the mint-profile pin reads
	// (a single unlocked profile never requires an explicit profile).
	unambiguousGuard := func() func(string) *corevault.ProfileRequiredError {
		return func(string) *corevault.ProfileRequiredError {
			if len(unlocked) > 1 {
				return &corevault.ProfileRequiredError{
					Code:     "profile_required",
					Profiles: unlocked,
					Message:  "more than one vault profile is unlocked; pass profile=<name>",
				}
			}
			return nil
		}
	}

	t.Run("open_vault_manager launcher", func(t *testing.T) {
		vu := putSrv()
		defer vu.Stop(context.Background())
		desc := NewOpenVaultManagerDescriptor(vu)
		swapProfileRequired(t, unambiguousGuard())
		res, err := desc.Handler(context.Background(), model.ToolRequest{
			Arguments: map[string]any{"vault_path": "vault:/uploads/report.pdf"},
		})
		require.NoError(t, err)
		require.False(t, res.IsError)
		sc, ok := res.StructuredContent.(map[string]any)
		require.True(t, ok)
		// A second profile unlocks before the browser PUTs.
		unlocked = []string{"home", "work"}
		status := put(t, vu, sc["presigned_url"].(string))
		require.Equal(t, http.StatusOK, status,
			"the presigned PUT must succeed against the profile pinned at mint time")
		require.Equal(t, "home", sealedProfile,
			"the write must land on the single profile that was unlocked at mint time")
	})

	t.Run("vault_upload_submit app helper", func(t *testing.T) {
		// Reset the registry to the single unlocked profile the mint sees.
		unlocked = []string{"home"}
		vu := putSrv()
		defer vu.Stop(context.Background())
		desc := vaultUploadSubmitDescriptor(vu)
		swapProfileRequired(t, unambiguousGuard())
		res, err := desc.Handler(context.Background(), model.ToolRequest{
			Arguments: map[string]any{"vault_path": "vault:/uploads/report.pdf"},
		})
		require.NoError(t, err)
		require.False(t, res.IsError)
		sc, ok := res.StructuredContent.(map[string]any)
		require.True(t, ok)
		unlocked = []string{"home", "work"}
		status := put(t, vu, sc["url"].(string))
		require.Equal(t, http.StatusOK, status,
			"the presigned PUT must succeed against the profile pinned at mint time")
		require.Equal(t, "home", sealedProfile,
			"the write must land on the single profile that was unlocked at mint time")
	})
}

// TestVaultUploadSubmitCarriesProfileIdentity pins the same multi-profile
// discipline on the app-only vault_upload_submit mint helper.
func TestVaultUploadSubmitCarriesProfileIdentity(t *testing.T) {
	vu := transfer.NewVaultHTTPUpload(func(ctx context.Context, r io.Reader, size int64, vaultPath string, meta map[string]any) (any, error) {
		require.Equal(t, "work", meta[corevault.MetaKeyProfile])
		return map[string]any{"status": "staged"}, nil
	}, 1<<20)
	defer vu.Stop(context.Background())

	desc := vaultUploadSubmitDescriptor(vu)

	t.Run("missing profile on multi-profile server errors", func(t *testing.T) {
		swapProfileRequired(t, twoProfileRequired())
		res, err := desc.Handler(context.Background(), model.ToolRequest{
			Arguments: map[string]any{"vault_path": "vault:/uploads/report.pdf"},
		})
		require.NoError(t, err)
		require.True(t, res.IsError)
		sc, ok := res.StructuredContent.(map[string]any)
		require.True(t, ok)
		require.Equal(t, "profile_required", sc["error"])
	})

	t.Run("explicit profile is minted through", func(t *testing.T) {
		swapProfileRequired(t, corevault.ProfileRequired)
		res, err := desc.Handler(context.Background(), model.ToolRequest{
			Arguments: map[string]any{"vault_path": "vault:/uploads/report.pdf", "profile": "work"},
		})
		require.NoError(t, err)
		require.False(t, res.IsError)
		sc, ok := res.StructuredContent.(map[string]any)
		require.True(t, ok)
		require.NotEmpty(t, sc["url"])
	})
}
