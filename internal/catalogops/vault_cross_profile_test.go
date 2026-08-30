package catalogops

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"go.lumeweb.com/pinner-cli/internal/catalog"
	"go.lumeweb.com/pinner-cli/internal/core/vault"
)

// crossProfileProbeDeps builds VaultDeps that dispatch to two per-profile mock
// services ("alpha", "beta") and report both as unlocked (Profiles returns two
// names). This is the catalog-ops boundary of the two-profile cross-vault
// probe: it exercises the profile_required rule, the per-profile dispatch of
// vault_send, and the pinned accept-state — all without ever sleeping on Sia.
func crossProfileProbeDeps(svcA, svcB vault.VaultService) VaultDeps {
	for _, s := range []vault.VaultService{svcA, svcB} {
		if m, ok := s.(*vault.MockVaultService); ok {
			// Close may or may not be reached depending on which op / guard
			// path runs; never require it.
			m.On("Close").Return(nil).Maybe()
		}
	}
	return VaultDeps{
		Service: func(profileName, indexerURL string) (vault.VaultService, error) {
			switch profileName {
			case "alpha":
				return svcA, nil
			case "beta":
				return svcB, nil
			default:
				return nil, fmt.Errorf("unknown profile %q", profileName)
			}
		},
		ResolveIndexerURL: func() string { return "https://indexer.example.com" },
		Profiles:          func() []string { return []string{"alpha", "beta"} },
	}
}

func invokeProbe(t *testing.T, deps VaultDeps, name string, input map[string]any) (any, error) {
	t.Helper()
	cat := catalog.NewCatalog()
	for _, op := range VaultOperations(deps) {
		if err := cat.Add(op); err != nil {
			t.Fatalf("Add(%q): %v", op.Name(), err)
		}
	}
	return cat.Invoke(context.Background(), name, input, catalog.ActorModel)
}

// TestCrossProfileProbe_ProfileRequiredRule verifies that on a two-profile
// server, vault ops without an explicit profile return a structured
// profile_required result (listing both profiles) instead of silently hitting
// the active vault or returning a misleading "vault object not found".
func TestCrossProfileProbe_ProfileRequiredRule(t *testing.T) {
	svcA := vault.NewMockVaultService(t)
	svcB := vault.NewMockVaultService(t)
	deps := crossProfileProbeDeps(svcA, svcB)

	cases := []struct {
		op    string
		input map[string]any
	}{
		{"vault_status", map[string]any{}},
		{"vault_ls", map[string]any{}},
		{"vault_sync", map[string]any{}},
		{"vault_stat", map[string]any{"path": "vault:/docs/a.txt"}},
		{"vault_verify", map[string]any{"path": "vault:/docs/a.txt"}},
		{"vault_search", map[string]any{}},
		{"vault_flush", map[string]any{}},
		{"vault_share", map[string]any{"path": "vault:/docs/a.txt", "expiry": "7d"}},
		{"vault_share_accept", map[string]any{"share_url": "https://x/y", "path": "vault:/docs/b.txt"}},
	}
	for _, tc := range cases {
		res, err := invokeProbe(t, deps, tc.op, tc.input)
		require.NoError(t, err, "%s without profile must not error (returns structured result)", tc.op)
		pr, ok := res.(*VaultProfileRequiredResult)
		require.True(t, ok, "%s without profile: want *VaultProfileRequiredResult, got %T", tc.op, res)
		require.Equal(t, "profile_required", pr.Code, "%s code", tc.op)
		require.ElementsMatch(t, []string{"alpha", "beta"}, pr.Profiles, "%s must list both profiles", tc.op)
		require.NotContains(t, pr.Message, "not found", "%s must not say 'not found'", tc.op)
	}

	// vault_profiles lists the unlocked profiles.
	res, err := invokeProbe(t, deps, "vault_profiles", map[string]any{})
	require.NoError(t, err)
	pr := res.(*VaultProfilesResult)
	require.ElementsMatch(t, []string{"alpha", "beta"}, pr.Profiles)
}

// TestCrossProfileProbe_SendDispatchesToPerProfileServices verifies vault_send
// dispatches to the correct per-profile service: profile A is the durable
// source (Stat + Share on svcA), profile B is the destination (ShareAccept on
// svcB), and the result carries accept_state pinned — the success signal, not a
// digest failure. It does not block on Sia.
func TestCrossProfileProbe_SendDispatchesToPerProfileServices(t *testing.T) {
	svcA := vault.NewMockVaultService(t)
	svcB := vault.NewMockVaultService(t)
	deps := crossProfileProbeDeps(svcA, svcB)

	srcPath := "vault:/docs/source.txt"
	destPath := "vault:/docs/dest.txt"
	shareURL := "https://indexer.example.com/objects/x/shared#encryption_key=K"

	svcA.On("Stat", mock.Anything, srcPath).
		Return(&vault.StatResult{Path: srcPath, Status: vault.FileStatusDurable}, nil)
	svcA.On("Share", mock.Anything, srcPath, mock.Anything).Return(shareURL, nil)
	svcB.On("ShareAccept", mock.Anything, destPath, mock.Anything, "", mock.Anything).
		Return(&vault.File{ObjectKey: "objB", Size: 5}, nil)

	res, err := invokeProbe(t, deps, "vault_send", map[string]any{
		"path":         srcPath,
		"dest_path":    destPath,
		"from_profile": "alpha",
		"to_profile":   "beta",
	})
	require.NoError(t, err)

	// Dispatch routing: source ops on svcA, accept (row landing in the other
	// profile's vault) on svcB.
	svcA.AssertCalled(t, "Stat", mock.Anything, srcPath)
	svcA.AssertCalled(t, "Share", mock.Anything, srcPath, mock.Anything)
	svcB.AssertCalled(t, "ShareAccept", mock.Anything, destPath, mock.Anything, "", mock.Anything)
	svcB.AssertNotCalled(t, "Share", mock.Anything, mock.Anything, mock.Anything)

	sr, ok := res.(*VaultSendResult)
	require.True(t, ok, "want *VaultSendResult, got %T", res)
	require.Equal(t, "alpha", sr.FromProfile)
	require.Equal(t, "beta", sr.ToProfile)
	require.Equal(t, destPath, sr.DestPath)
	require.Equal(t, "objB", sr.ObjectKey)
	require.Equal(t, int64(5), sr.Size)
	require.Equal(t, "pinned", sr.AcceptState, "accept_state pinned is the success signal, not a digest failure")
}

// TestCrossProfileProbe_SendSourceNotDurable verifies vault_send does NOT block
// on a long Sia upload when the source is still staged: it returns a structured
// not_durable result (flush-if-needed) instead of waiting.
func TestCrossProfileProbe_SendSourceNotDurable(t *testing.T) {
	svcA := vault.NewMockVaultService(t)
	svcB := vault.NewMockVaultService(t)
	deps := crossProfileProbeDeps(svcA, svcB)

	srcPath := "vault:/docs/source.txt"
	svcA.On("Stat", mock.Anything, srcPath).
		Return(&vault.StatResult{Path: srcPath, Status: vault.FileStatusStaged}, nil)

	res, err := invokeProbe(t, deps, "vault_send", map[string]any{
		"path":         srcPath,
		"dest_path":    "vault:/docs/dest.txt",
		"from_profile": "alpha",
		"to_profile":   "beta",
	})
	require.NoError(t, err)
	nd, ok := res.(*VaultNotDurableResult)
	require.True(t, ok, "want *VaultNotDurableResult, got %T", res)
	require.Equal(t, "not_durable", nd.Code)
	require.Equal(t, vault.FileStatusStaged, nd.Status)
	svcA.AssertNotCalled(t, "Share", mock.Anything, mock.Anything, mock.Anything)
	svcB.AssertNotCalled(t, "ShareAccept", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

// TestCrossProfileProbe_AcceptStatePinned verifies accepting into a specific
// profile returns accept_state pinned (a metadata-only pin of the same object
// key), NOT a digest mismatch signal.
func TestCrossProfileProbe_AcceptStatePinned(t *testing.T) {
	svcB := vault.NewMockVaultService(t)
	svcA := vault.NewMockVaultService(t)
	deps := crossProfileProbeDeps(svcA, svcB)

	destPath := "vault:/docs/acc.txt"
	shareURL := "https://indexer.example.com/objects/x/shared#encryption_key=K"
	svcB.On("ShareAccept", mock.Anything, destPath, shareURL, "", mock.Anything).
		Return(&vault.File{ObjectKey: "objAcc", Size: 7}, nil)

	res, err := invokeProbe(t, deps, "vault_share_accept", map[string]any{
		"share_url": shareURL,
		"path":      destPath,
		"profile":   "beta",
	})
	require.NoError(t, err)
	ar, ok := res.(*VaultShareAcceptResult)
	require.True(t, ok, "want *VaultShareAcceptResult, got %T", res)
	require.Equal(t, "pinned", ar.AcceptState)
	require.Equal(t, "not_applicable", ar.DigestVerified)
	require.Equal(t, destPath, ar.Path)
	require.Equal(t, "objAcc", ar.ObjectKey)
}

// TestCrossProfileProbe_SendRequiresAllFourArgs verifies vault_send's input
// schema requires path, from_profile, to_profile, and dest_path (the tool
// description claims all four). This guards the leftover where the description
// promised four required args but the schema only enforced path + dest_path.
func TestCrossProfileProbe_SendRequiresAllFourArgs(t *testing.T) {
	svcA := vault.NewMockVaultService(t)
	svcB := vault.NewMockVaultService(t)
	deps := crossProfileProbeDeps(svcA, svcB)

	// Missing from_profile and to_profile must be rejected by required-arg
	// validation (like path/dest_path), never a silent invalid_profiles fallback
	// and never a service hit.
	_, err := invokeProbe(t, deps, "vault_send", map[string]any{
		"path":      "vault:/docs/a.txt",
		"dest_path": "vault:/docs/b.txt",
	})
	require.Error(t, err, "vault_send without from_profile/to_profile must fail required-arg validation")
	require.Contains(t, err.Error(), "from_profile")
	svcA.AssertNotCalled(t, "Stat", mock.Anything, mock.Anything)
	svcB.AssertNotCalled(t, "ShareAccept", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

// TestCrossProfileProbe_VerifyAfterSendIsNotAppplicable pins the leftover that
// vault_verify must report digest_verified "not_applicable" (not "unverified")
// for a freshly sent/accepted object whose digest has not been recorded yet.
// Accept-state pinned is the success signal; verify must not read as a failure.
func TestCrossProfileProbe_VerifyAfterSendIsNotAppplicable(t *testing.T) {
	svcA := vault.NewMockVaultService(t)
	svcB := vault.NewMockVaultService(t)
	deps := crossProfileProbeDeps(svcA, svcB)

	path := "vault:/docs/sent.txt"
	// A pinned object with no recorded digest (accepted, not yet decrypted).
	svcB.On("Verify", mock.Anything, path).
		Return(&vault.VerifyResult{
			Path:           path,
			ObjectExists:   true,
			DigestVerified: vault.DigestVerifiedNotApplicable,
		}, nil)

	res, err := invokeProbe(t, deps, "vault_verify", map[string]any{
		"path":    path,
		"profile": "beta",
	})
	require.NoError(t, err)
	vr, ok := res.(*vault.VerifyResult)
	require.True(t, ok, "want *vault.VerifyResult, got %T", res)
	require.Equal(t, "not_applicable", vr.DigestVerified,
		"verify on a freshly sent path must not be 'unverified'")
}

// TestCrossProfileProbe_SendSchemaRequiresAllFourArgs verifies the vault_send
// MCP JSON schema's required list contains all four args (path, from_profile,
// to_profile, dest_path) — matching the tool description. This pins the leftover
// where the schema only required path + dest_path.
func TestCrossProfileProbe_SendSchemaRequiresAllFourArgs(t *testing.T) {
	svcA := vault.NewMockVaultService(t)
	svcB := vault.NewMockVaultService(t)
	deps := crossProfileProbeDeps(svcA, svcB)

	cat := catalog.NewCatalog()
	for _, op := range VaultOperations(deps) {
		if err := cat.Add(op); err != nil {
			t.Fatalf("Add(%q): %v", op.Name(), err)
		}
	}
	td, ok := cat.Describe("vault_send", catalog.ActorModel)
	require.True(t, ok, "vault_send not described")
	var parsed struct {
		Required []string `json:"required"`
	}
	require.NoError(t, json.Unmarshal(td.InputSchema, &parsed))
	require.ElementsMatch(t, []string{"path", "from_profile", "to_profile", "dest_path"}, parsed.Required,
		"vault_send schema must require all four args, matching its description")
}
