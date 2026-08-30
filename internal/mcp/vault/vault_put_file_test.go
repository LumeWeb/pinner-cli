package vault

import (
	"context"
	"io"
	"testing"

	"github.com/stretchr/testify/require"

	corevault "go.lumeweb.com/pinner-cli/internal/core/vault"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/model"
	"go.lumeweb.com/pinner-cli/internal/mcp/core/transfer"
	"go.lumeweb.com/pinner-cli/internal/mcp/hostenv"
)

// vaultPutDescriptor builds a unified vault_put_file descriptor for a given
// transport wiring, mirroring the production registration shape so the test
// exercises the exact same routing logic. It injects a no-op profile check so
// these tests are deterministic regardless of the host's real vault registry
// (the profile-required rule is exercised explicitly in the two-profile tests).
func vaultPutDescriptor(coLocated, remote bool, pathFn LocalPathVaultPutHandler, vu *transfer.VaultHTTPUpload, relayFn VaultPutHandler) model.ToolDescriptor {
	return NewVaultPutFileDescriptorWithProfileCheck(transportFeatures(coLocated, remote), coLocated, remote, pathFn, vu, relayFn, nil, 0, noProfileRequired, nil)
}

// noProfileRequired is the injectable profile guard for the deterministic
// single-profile tests: it always permits the write (never a multi-profile
// server).
func noProfileRequired(string) *corevault.ProfileRequiredError { return nil }

func TestVaultPutFileDescriptorRequiresVaultPath(t *testing.T) {
	desc := vaultPutDescriptor(true, false, func(ctx context.Context, path, vaultPath, archiveMode string, _ map[string]any) (any, error) {
		t.Fatal("handler must not run without vault_path")
		return nil, nil
	}, nil, nil)
	_, err := desc.Handler(context.Background(), model.ToolRequest{Arguments: map[string]any{
		"source": map[string]any{"mode": "path", "path": "/tmp/x"},
	}})
	require.ErrorContains(t, err, "vault_path is required")
}

func TestVaultPutFileDescriptorStdioPath(t *testing.T) {
	var gotPath, gotVaultPath, gotMode string
	desc := vaultPutDescriptor(true, false, func(ctx context.Context, path, vaultPath, archiveMode string, _ map[string]any) (any, error) {
		gotPath, gotVaultPath, gotMode = path, vaultPath, archiveMode
		return map[string]any{"vault_path": vaultPath}, nil
	}, nil, nil)

	res, err := desc.Handler(context.Background(), model.ToolRequest{Arguments: map[string]any{
		"source":       map[string]any{"mode": "path", "path": "/host/abs/file.bin"},
		"vault_path":   "vault:/uploads/file.bin",
		"archive_mode": "preserve",
	}})
	require.NoError(t, err)
	require.False(t, res.IsError)
	require.Equal(t, "/host/abs/file.bin", gotPath)
	require.Equal(t, "vault:/uploads/file.bin", gotVaultPath)
	require.Equal(t, "preserve", gotMode)
	// Text surfaces the result as canonical JSON (the vault path), not prose.
	require.JSONEq(t, `{"status":"ok","vault_path":"vault:/uploads/file.bin"}`, res.Text)
}

func TestVaultPutFileDescriptorStdioRejectsMint(t *testing.T) {
	desc := vaultPutDescriptor(true, false, func(ctx context.Context, path, vaultPath, archiveMode string, _ map[string]any) (any, error) {
		t.Fatal("path handler must not run for mint")
		return nil, nil
	}, nil, nil)
	_, err := desc.Handler(context.Background(), model.ToolRequest{Arguments: map[string]any{
		"source":     map[string]any{"mode": "mint"},
		"vault_path": "vault:/uploads/file.bin",
	}})
	require.Error(t, err)
}

func TestVaultPutFileDescriptorStdioPathNotConfigured(t *testing.T) {
	desc := vaultPutDescriptor(true, false, nil, nil, nil)
	_, err := desc.Handler(context.Background(), model.ToolRequest{Arguments: map[string]any{
		"source":     map[string]any{"mode": "path", "path": "/tmp/x"},
		"vault_path": "vault:/uploads/x.bin",
	}})
	require.ErrorContains(t, err, "local path vault handler is not configured")
}

func TestVaultPutFileDescriptorHTTPMints(t *testing.T) {
	vu := transfer.NewVaultHTTPUpload(nil, 0)
	defer vu.Stop(context.Background())
	desc := vaultPutDescriptor(false, false, nil, vu, nil)

	res, err := desc.Handler(context.Background(), model.ToolRequest{Arguments: map[string]any{
		"source":     map[string]any{"mode": "mint"},
		"vault_path": "vault:/uploads/report.pdf",
		"ttl":        "5m",
	}})
	require.NoError(t, err)
	require.False(t, res.IsError)
	sc, ok := res.StructuredContent.(map[string]any)
	require.True(t, ok)
	require.NotEmpty(t, sc["url"])
	require.Equal(t, "vault:/uploads/report.pdf", sc["vault_path"])
	require.NotEmpty(t, sc["curl_command"])
}

func TestVaultPutFileDescriptorHTTPRejectsPath(t *testing.T) {
	vu := transfer.NewVaultHTTPUpload(nil, 0)
	defer vu.Stop(context.Background())
	desc := vaultPutDescriptor(false, false, nil, vu, nil)
	_, err := desc.Handler(context.Background(), model.ToolRequest{Arguments: map[string]any{
		"source":     map[string]any{"mode": "path", "path": "/etc/passwd"},
		"vault_path": "vault:/uploads/passwd",
	}})
	require.Error(t, err, "path source invalid on HTTP transport")
}

// twoProfileRequired is the injectable profile guard for the multi-profile
// tests: it requires an explicit profile and reports the unlocked set when one
// is missing, mirroring corevault.ProfileRequired.
func twoProfileRequired() func(string) *corevault.ProfileRequiredError {
	return func(profile string) *corevault.ProfileRequiredError {
		if profile != "" {
			return nil
		}
		return &corevault.ProfileRequiredError{
			Code:     "profile_required",
			Profiles: []string{"alpha", "beta"},
			Message:  "more than one vault profile is unlocked (alpha, beta); pass profile=<name>",
		}
	}
}

// TestVaultPutFileMultiProfileMintRequiresProfile locks in the mint-time hole
// fix: on a multi-profile server, vault_put_file with source mode mint and no
// profile must fail with profile_required BEFORE a one-shot upload URL is
// minted (the URL would otherwise be unusable — the follow-up PUT 500s). The
// mint endpoint must never be reached.
func TestVaultPutFileMultiProfileMintRequiresProfile(t *testing.T) {
	vu := transfer.NewVaultHTTPUpload(nil, 0)
	defer vu.Stop(context.Background())
	desc := NewVaultPutFileDescriptorWithProfileCheck(transportFeatures(false, false), false, false, nil, vu, nil, nil, 0, twoProfileRequired(), nil)

	res, err := desc.Handler(context.Background(), model.ToolRequest{Arguments: map[string]any{
		"source":     map[string]any{"mode": "mint"},
		"vault_path": "vault:/uploads/report.pdf",
	}})
	require.NoError(t, err, "profile_required surfaces as an IsError result, not a transport error")
	require.True(t, res.IsError, "profile_required must be an error result")
	sc, ok := res.StructuredContent.(map[string]any)
	require.True(t, ok)
	require.Equal(t, "error", sc["status"])
	require.Equal(t, "profile_required", sc["error"])
	require.ElementsMatch(t, []string{"alpha", "beta"}, sc["profiles"])
}

// TestVaultPutFileMultiProfileMintWithProfileMints locks in that supplying the
// explicit profile on a multi-profile server still mints normally (the guard
// only fires on a missing profile).
func TestVaultPutFileMultiProfileMintWithProfileMints(t *testing.T) {
	vu := transfer.NewVaultHTTPUpload(nil, 0)
	defer vu.Stop(context.Background())
	desc := NewVaultPutFileDescriptorWithProfileCheck(transportFeatures(false, false), false, false, nil, vu, nil, nil, 0, twoProfileRequired(), nil)

	res, err := desc.Handler(context.Background(), model.ToolRequest{Arguments: map[string]any{
		"source":     map[string]any{"mode": "mint"},
		"vault_path": "vault:/uploads/report.pdf",
		"profile":    "alpha",
	}})
	require.NoError(t, err)
	require.False(t, res.IsError)
	sc, ok := res.StructuredContent.(map[string]any)
	require.True(t, ok)
	require.NotEmpty(t, sc["url"])
}

// TestVaultPutFileMultiProfileStdioRequiresProfile verifies the same
// profile_required rule applies on every write branch (not just mint), so a
// multi-profile stdio/path write without a profile fails before touching the
// local path handler.
func TestVaultPutFileMultiProfileStdioRequiresProfile(t *testing.T) {
	desc := NewVaultPutFileDescriptorWithProfileCheck(transportFeatures(true, false), true, false, func(ctx context.Context, path, vaultPath, archiveMode string, _ map[string]any) (any, error) {
		t.Fatal("path handler must not run without an explicit profile on a multi-profile server")
		return nil, nil
	}, nil, nil, nil, 0, twoProfileRequired(), nil)

	res, err := desc.Handler(context.Background(), model.ToolRequest{Arguments: map[string]any{
		"source":     map[string]any{"mode": "path", "path": "/tmp/x.bin"},
		"vault_path": "vault:/uploads/x.bin",
	}})
	require.NoError(t, err)
	require.True(t, res.IsError)
	sc := res.StructuredContent.(map[string]any)
	require.Equal(t, "profile_required", sc["error"])
}

func TestVaultPutFileDescriptorOpenAIData(t *testing.T) {
	var gotVaultPath string
	var size int64
	desc := vaultPutDescriptor(false, true, nil, nil, func(ctx context.Context, r io.Reader, sz int64, vaultPath string, _ map[string]any) (any, error) {
		size = sz
		gotVaultPath = vaultPath
		return map[string]any{"vault_path": vaultPath}, nil
	})

	res, err := desc.Handler(context.Background(), model.ToolRequest{Arguments: map[string]any{
		"source":     map[string]any{"mode": "data", "data": "data:;name=note.txt;size=2;base64,aGk="},
		"vault_path": "vault:/uploads/note.txt",
	}})
	require.NoError(t, err)
	require.False(t, res.IsError)
	require.Equal(t, int64(2), size)
	require.Equal(t, "vault:/uploads/note.txt", gotVaultPath)
}

func TestVaultPutFileDescriptorOpenAIRelayHonorsMaxBytes(t *testing.T) {
	// The relayed url/data source must honor the operator-configured relay cap,
	// not silently fall back to the 512 MiB package default.
	desc := NewVaultPutFileDescriptorWithProfileCheck(transportFeatures(false, true), false, true, nil, nil, func(ctx context.Context, r io.Reader, sz int64, vaultPath string, _ map[string]any) (any, error) {
		t.Fatal("relay must not receive an oversized upload")
		return nil, nil
	}, nil, 4, noProfileRequired, nil) // cap at 4 bytes
	_, err := desc.Handler(context.Background(), model.ToolRequest{Arguments: map[string]any{
		"source":     map[string]any{"mode": "data", "data": "data:;name=big.bin;size=100;base64,YWFhYWFhYWFh"},
		"vault_path": "vault:/uploads/big.bin",
	}})
	require.Error(t, err)
}

func TestVaultPutFileDescriptorOpenAIRejectsPath(t *testing.T) {
	desc := vaultPutDescriptor(false, true, nil, nil, nil)
	_, err := desc.Handler(context.Background(), model.ToolRequest{Arguments: map[string]any{
		"source":     map[string]any{"mode": "path", "path": "/etc/passwd"},
		"vault_path": "vault:/uploads/passwd",
	}})
	require.Error(t, err, "path source invalid on OpenAI transport")
}

// TestVaultPutFileRejectsUnsafePathOnEverySourceBranch extends the mint-only
// guard (TestVaultUploadMintRejectsUnsafePath) to the vault_put_file tool:
// the destination path is validated (validateVaultFilePath) before ANY byte is
// read or written, across every source branch (path, mint, and url/data), so
// no transport can write a directory, a traversal, a non-vault destination, or
// a profile-authority path. There is intentionally no single-folder (uploads)
// scope: any well-formed vault file path is allowed on every branch.
func TestVaultPutFileRejectsUnsafePathOnEverySourceBranch(t *testing.T) {
	// Bad on EVERY branch: not a well-formed vault FILE path under the rules.
	badPaths := []string{
		"vault:/uploads/",                // directory (trailing slash)
		"vault:/uploads/../../secret.db", // traversal
		"vault:/uploads/..",              // .. as the leaf filename
		"vault:/uploads/.",               // . as the leaf filename
		"vault://work/uploads/x.pdf",     // profile-authority path unsupported
		"not-a-vault-path",               // not a vault: path
	}
	// A well-formed vault FILE path outside any single folder — must be allowed
	// on every branch (handler must be reached and succeed).
	anyFolderFile := "vault:/secret.db"

	stdio := vaultPutDescriptor(true, false, noopPathFn, nil, nil)
	vu := transfer.NewVaultHTTPUpload(nil, 0)
	defer vu.Stop(context.Background())
	http := vaultPutDescriptor(false, false, nil, vu, func(ctx context.Context, r io.Reader, sz int64, vaultPath string, _ map[string]any) (any, error) {
		return map[string]any{"stored": true}, nil
	})
	openai := vaultPutDescriptor(false, true, nil, nil, func(ctx context.Context, r io.Reader, sz int64, vaultPath string, _ map[string]any) (any, error) {
		return map[string]any{"stored": true}, nil
	})

	branches := map[string]struct {
		desc model.ToolDescriptor
		args map[string]any
	}{
		"stdio/path": {
			desc: stdio,
			args: map[string]any{"source": map[string]any{"mode": "path", "path": "/tmp/x"}},
		},
		"http/mint": {
			desc: http,
			args: map[string]any{"source": map[string]any{"mode": "mint"}},
		},
		"openai/data": {
			desc: openai,
			args: map[string]any{"source": map[string]any{"mode": "data", "data": "data:;name=x.txt;size=1;base64,aA=="}},
		},
		"openai/url": {
			desc: openai,
			args: map[string]any{"source": map[string]any{"mode": "url", "url": "https://example.org/x.txt"}},
		},
	}

	for branch, bc := range branches {
		for _, p := range badPaths {
			args := map[string]any{"vault_path": p}
			for k, v := range bc.args {
				args[k] = v
			}
			res, err := bc.desc.Handler(context.Background(), model.ToolRequest{Arguments: args})
			// Unsafe paths must be rejected before any write: either the
			// handler surface returns a Go error or an IsError result.
			require.True(t, err != nil || res.IsError, "%s: unsafe vault path %q must be rejected before write", branch, p)
		}
		// A well-formed file path outside any single folder is accepted on
		// every branch (reaching the write handler), confirming there is no
		// folder restriction. The OpenAI branch accepts via data mode (inline
		// decode, no network fetch) to keep the test hermetic.
		acceptSource := bc.args["source"]
		if branch == "openai/url" {
			acceptSource = map[string]any{"mode": "data", "data": "data:;name=x.txt;size=2;base64,aGk="}
		}
		args := map[string]any{"vault_path": anyFolderFile, "source": acceptSource}
		res, err := bc.desc.Handler(context.Background(), model.ToolRequest{Arguments: args})
		require.NoError(t, err, "%s: vault file path %q must be accepted", branch, anyFolderFile)
		require.False(t, res.IsError)
	}
}

// noopPathFn is a successful no-op LocalPathVaultPutHandler used where a
// branch's handler must be reached to prove path validation passed.
func noopPathFn(ctx context.Context, path, vaultPath, archiveMode string, _ map[string]any) (any, error) {
	return map[string]any{"stored": true}, nil
}

// TestVaultPutFileAnyFolderAcrossAllBranches locks in that vault_put_file can
// write to any vault folder (e.g. vault:/docs/f.pdf) on every transport, not
// just the historical uploads scope.
func TestVaultPutFileAnyFolderAcrossAllBranches(t *testing.T) {
	pathBranches := []struct {
		desc model.ToolDescriptor
		args map[string]any
	}{
		{
			desc: vaultPutDescriptor(true, false, noopPathFn, nil, func(ctx context.Context, r io.Reader, sz int64, vaultPath string, _ map[string]any) (any, error) {
				return map[string]any{"stored": true}, nil
			}),
			args: map[string]any{"source": map[string]any{"mode": "path", "path": "/tmp/host/report.pdf"}},
		},
		{
			desc: vaultPutDescriptor(false, false, nil, transfer.NewVaultHTTPUpload(nil, 0), func(ctx context.Context, r io.Reader, sz int64, vaultPath string, _ map[string]any) (any, error) {
				return map[string]any{"stored": true}, nil
			}),
			args: map[string]any{"source": map[string]any{"mode": "mint"}},
		},
		{
			desc: vaultPutDescriptor(false, true, nil, nil, func(ctx context.Context, r io.Reader, sz int64, vaultPath string, _ map[string]any) (any, error) {
				return map[string]any{"stored": true}, nil
			}),
			args: map[string]any{"source": map[string]any{"mode": "data", "data": "data:;name=x.pdf;size=2;base64,aGk="}},
		},
	}
	for _, bc := range pathBranches {
		args := map[string]any{"vault_path": "vault:/docs/f.pdf"}
		for k, v := range bc.args {
			args[k] = v
		}
		res, err := bc.desc.Handler(context.Background(), model.ToolRequest{Arguments: args})
		require.NoError(t, err, "vault:/docs/f.pdf must be accepted")
		require.False(t, res.IsError)
	}
}

// TestVaultPutFileStampsMetadata verifies the stdio path handler receives the
// auto-stamped write-context metadata the tool handler builds: src=mcp, host
// from the request's detected profile, plus caller-supplied agent and KV. It
// does NOT include profile (the CLI closure stamps that), so the caller-supplied
// metadata is asserted to carry src + host + agent + kind.
func TestVaultPutFileStampsMetadata(t *testing.T) {
	var got map[string]any
	desc := vaultPutDescriptor(true, false, func(ctx context.Context, path, vaultPath, archiveMode string, meta map[string]any) (any, error) {
		got = meta
		return map[string]any{"vault_path": vaultPath}, nil
	}, nil, nil)

	req := model.ToolRequest{
		Arguments: map[string]any{
			"source":     map[string]any{"mode": "path", "path": "/tmp/x.bin"},
			"vault_path": "vault:/docs/x.bin",
			"agent":      "orchestrator-a",
			"metadata":   map[string]any{"kind": "artifact", "project": "reports"},
		},
		Caps: &model.RequestCaps{Profile: &hostenv.PlatformProfile{HostType: hostenv.HostCodex}},
	}
	_, err := desc.Handler(context.Background(), req)
	require.NoError(t, err)

	require.Equal(t, "mcp", got[corevault.MetaKeySrc])
	require.Equal(t, string(hostenv.HostCodex), got[corevault.MetaKeyHost])
	require.Equal(t, "orchestrator-a", got["agent"])
	require.Equal(t, "artifact", got["kind"])
	require.Equal(t, "reports", got["project"])
	// No explicit profile argument was passed, so the tool surface must NOT
	// stamp one; the CLI closure resolves and stamps the active profile.
	require.NotContains(t, got, corevault.MetaKeyProfile, "profile is stamped by the CLI closure, not the tool handler")
}

// TestVaultPutFileStampsRequestedProfile verifies that when the caller passes
// an explicit profile argument, the tool surface stamps it into the write
// metadata so the CLI closure routes to that vault instead of the active one.
func TestVaultPutFileStampsRequestedProfile(t *testing.T) {
	var got map[string]any
	desc := vaultPutDescriptor(true, false, func(ctx context.Context, path, vaultPath, archiveMode string, meta map[string]any) (any, error) {
		got = meta
		return map[string]any{"vault_path": vaultPath}, nil
	}, nil, nil)

	req := model.ToolRequest{
		Arguments: map[string]any{
			"source":     map[string]any{"mode": "path", "path": "/tmp/x.bin"},
			"vault_path": "vault:/docs/x.bin",
			"profile":    "work",
		},
		Caps: &model.RequestCaps{Profile: &hostenv.PlatformProfile{HostType: hostenv.HostCodex}},
	}
	_, err := desc.Handler(context.Background(), req)
	require.NoError(t, err)

	require.Equal(t, "mcp", got[corevault.MetaKeySrc])
	require.Equal(t, "work", got[corevault.MetaKeyProfile], "explicit profile must be stamped into the write metadata")
}
