package mcp

import (
	"context"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
)

// vaultPutDescriptor builds a unified vault_put_file descriptor for a given
// transport wiring, mirroring the production registration shape so the test
// exercises the exact same routing logic.
func vaultPutDescriptor(coLocated, remote bool, pathFn LocalPathVaultPutHandler, vu *vaultHTTPUpload, relayFn VaultPutHandler) ToolDescriptor {
	return NewVaultPutFileDescriptor(coLocated, remote, pathFn, vu, relayFn, nil, 0)
}

func TestVaultPutFileDescriptorRequiresVaultPath(t *testing.T) {
	desc := vaultPutDescriptor(true, false, func(ctx context.Context, path, vaultPath, archiveMode string) (any, error) {
		t.Fatal("handler must not run without vault_path")
		return nil, nil
	}, nil, nil)
	_, err := desc.Handler(context.Background(), ToolRequest{Arguments: map[string]any{
		"source": map[string]any{"mode": "path", "path": "/tmp/x"},
	}})
	require.ErrorContains(t, err, "vault_path is required")
}

func TestVaultPutFileDescriptorStdioPath(t *testing.T) {
	var gotPath, gotVaultPath, gotMode string
	desc := vaultPutDescriptor(true, false, func(ctx context.Context, path, vaultPath, archiveMode string) (any, error) {
		gotPath, gotVaultPath, gotMode = path, vaultPath, archiveMode
		return map[string]any{"vault_path": vaultPath}, nil
	}, nil, nil)

	res, err := desc.Handler(context.Background(), ToolRequest{Arguments: map[string]any{
		"source":       map[string]any{"mode": "path", "path": "/host/abs/file.bin"},
		"vault_path":   "vault:/uploads/file.bin",
		"archive_mode": "preserve",
	}})
	require.NoError(t, err)
	require.False(t, res.IsError)
	require.Equal(t, "/host/abs/file.bin", gotPath)
	require.Equal(t, "vault:/uploads/file.bin", gotVaultPath)
	require.Equal(t, "preserve", gotMode)
	require.Equal(t, "Stored in the vault.", res.Text)
}

func TestVaultPutFileDescriptorStdioRejectsMint(t *testing.T) {
	desc := vaultPutDescriptor(true, false, func(ctx context.Context, path, vaultPath, archiveMode string) (any, error) {
		t.Fatal("path handler must not run for mint")
		return nil, nil
	}, nil, nil)
	_, err := desc.Handler(context.Background(), ToolRequest{Arguments: map[string]any{
		"source":     map[string]any{"mode": "mint"},
		"vault_path": "vault:/uploads/file.bin",
	}})
	require.Error(t, err)
}

func TestVaultPutFileDescriptorStdioPathNotConfigured(t *testing.T) {
	desc := vaultPutDescriptor(true, false, nil, nil, nil)
	_, err := desc.Handler(context.Background(), ToolRequest{Arguments: map[string]any{
		"source":     map[string]any{"mode": "path", "path": "/tmp/x"},
		"vault_path": "vault:/uploads/x.bin",
	}})
	require.ErrorContains(t, err, "local path vault handler is not configured")
}

func TestVaultPutFileDescriptorHTTPMints(t *testing.T) {
	vu := NewVaultHTTPUpload(nil, 0)
	defer vu.Stop(context.Background())
	desc := vaultPutDescriptor(false, false, nil, vu, nil)

	res, err := desc.Handler(context.Background(), ToolRequest{Arguments: map[string]any{
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
	vu := NewVaultHTTPUpload(nil, 0)
	defer vu.Stop(context.Background())
	desc := vaultPutDescriptor(false, false, nil, vu, nil)
	_, err := desc.Handler(context.Background(), ToolRequest{Arguments: map[string]any{
		"source":     map[string]any{"mode": "path", "path": "/etc/passwd"},
		"vault_path": "vault:/uploads/passwd",
	}})
	require.Error(t, err, "path source invalid on HTTP transport")
}

func TestVaultPutFileDescriptorOpenAIData(t *testing.T) {
	var gotVaultPath string
	var size int64
	desc := vaultPutDescriptor(false, true, nil, nil, func(ctx context.Context, r io.Reader, sz int64, vaultPath string) (any, error) {
		size = sz
		gotVaultPath = vaultPath
		return map[string]any{"vault_path": vaultPath}, nil
	})

	res, err := desc.Handler(context.Background(), ToolRequest{Arguments: map[string]any{
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
	desc := NewVaultPutFileDescriptor(false, true, nil, nil, func(ctx context.Context, r io.Reader, sz int64, vaultPath string) (any, error) {
		t.Fatal("relay must not receive an oversized upload")
		return nil, nil
	}, nil, 4) // cap at 4 bytes
	_, err := desc.Handler(context.Background(), ToolRequest{Arguments: map[string]any{
		"source":     map[string]any{"mode": "data", "data": "data:;name=big.bin;size=100;base64,YWFhYWFhYWFh"},
		"vault_path": "vault:/uploads/big.bin",
	}})
	require.Error(t, err)
}

func TestVaultPutFileDescriptorOpenAIRejectsPath(t *testing.T) {
	desc := vaultPutDescriptor(false, true, nil, nil, nil)
	_, err := desc.Handler(context.Background(), ToolRequest{Arguments: map[string]any{
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
	vu := NewVaultHTTPUpload(nil, 0)
	defer vu.Stop(context.Background())
	http := vaultPutDescriptor(false, false, nil, vu, func(ctx context.Context, r io.Reader, sz int64, vaultPath string) (any, error) {
		return map[string]any{"stored": true}, nil
	})
	openai := vaultPutDescriptor(false, true, nil, nil, func(ctx context.Context, r io.Reader, sz int64, vaultPath string) (any, error) {
		return map[string]any{"stored": true}, nil
	})

	branches := map[string]struct {
		desc ToolDescriptor
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
			res, err := bc.desc.Handler(context.Background(), ToolRequest{Arguments: args})
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
		res, err := bc.desc.Handler(context.Background(), ToolRequest{Arguments: args})
		require.NoError(t, err, "%s: vault file path %q must be accepted", branch, anyFolderFile)
		require.False(t, res.IsError)
	}
}

// noopPathFn is a successful no-op LocalPathVaultPutHandler used where a
// branch's handler must be reached to prove path validation passed.
func noopPathFn(ctx context.Context, path, vaultPath, archiveMode string) (any, error) {
	return map[string]any{"stored": true}, nil
}

// TestVaultPutFileAnyFolderAcrossAllBranches locks in that vault_put_file can
// write to any vault folder (e.g. vault:/docs/f.pdf) on every transport, not
// just the historical uploads scope.
func TestVaultPutFileAnyFolderAcrossAllBranches(t *testing.T) {
	pathBranches := []struct {
		desc ToolDescriptor
		args map[string]any
	}{
		{
			desc: vaultPutDescriptor(true, false, noopPathFn, nil, func(ctx context.Context, r io.Reader, sz int64, vaultPath string) (any, error) {
				return map[string]any{"stored": true}, nil
			}),
			args: map[string]any{"source": map[string]any{"mode": "path", "path": "/tmp/host/report.pdf"}},
		},
		{
			desc: vaultPutDescriptor(false, false, nil, NewVaultHTTPUpload(nil, 0), func(ctx context.Context, r io.Reader, sz int64, vaultPath string) (any, error) {
				return map[string]any{"stored": true}, nil
			}),
			args: map[string]any{"source": map[string]any{"mode": "mint"}},
		},
		{
			desc: vaultPutDescriptor(false, true, nil, nil, func(ctx context.Context, r io.Reader, sz int64, vaultPath string) (any, error) {
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
		res, err := bc.desc.Handler(context.Background(), ToolRequest{Arguments: args})
		require.NoError(t, err, "vault:/docs/f.pdf must be accepted")
		require.False(t, res.IsError)
	}
}
