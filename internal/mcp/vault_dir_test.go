package mcp

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// recordingPut returns a VaultPutFunc that records every (vaultPath, size, content)
// it is given and returns a fake object id derived from the vault path.
func recordingPut(t *testing.T, got *[]string) VaultPutFunc {
	return func(ctx context.Context, reader io.Reader, size int64, vaultPath string) (any, error) {
		content, err := io.ReadAll(reader)
		if err != nil {
			return nil, err
		}
		*got = append(*got, vaultPath+"|"+string(content))
		return "obj:" + vaultPath, nil
	}
}

func mkfile(t *testing.T, dir, rel string, content []byte) {
	t.Helper()
	full := filepath.Join(dir, rel)
	require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
	require.NoError(t, os.WriteFile(full, content, 0o644))
}

func TestDirToVaultSingleFile(t *testing.T) {
	dir := t.TempDir()
	mkfile(t, dir, "a.txt", []byte("hello vault"))

	var got []string
	res, err := DirToVault(context.Background(), dir, "vault:/docs", recordingPut(t, &got))
	require.NoError(t, err)

	require.Equal(t, "vault:/docs", res.Base)
	require.Equal(t, 1, res.Total)
	require.Equal(t, int64(len("hello vault")), res.Bytes)
	require.Len(t, res.Files, 1)
	require.Equal(t, "a.txt", res.Files[0].RelPath)
	require.Equal(t, "vault:/docs/a.txt", res.Files[0].Vault)
	require.Equal(t, int64(len("hello vault")), res.Files[0].Size)
	require.Equal(t, "obj:vault:/docs/a.txt", res.Files[0].Object)

	// The put func must have received the actual file content and vault path.
	require.Equal(t, []string{"vault:/docs/a.txt|hello vault"}, got)
}

func TestDirToVaultNested(t *testing.T) {
	dir := t.TempDir()
	mkfile(t, dir, "sub/a.txt", []byte("nested a"))
	mkfile(t, dir, "b.txt", []byte("root b"))

	var got []string
	res, err := DirToVault(context.Background(), dir, "vault:/docs", recordingPut(t, &got))
	require.NoError(t, err)

	require.Equal(t, 2, res.Total)
	require.Equal(t, int64(len("nested a")+len("root b")), res.Bytes)
	require.Len(t, res.Files, 2)

	byVault := map[string]*vaultFileResult{}
	for i := range res.Files {
		byVault[res.Files[i].Vault] = &res.Files[i]
	}
	require.Contains(t, byVault, "vault:/docs/sub/a.txt")
	require.Contains(t, byVault, "vault:/docs/b.txt")
	require.Equal(t, "sub/a.txt", byVault["vault:/docs/sub/a.txt"].RelPath)
	require.Equal(t, "b.txt", byVault["vault:/docs/b.txt"].RelPath)
	require.Contains(t, got, "vault:/docs/sub/a.txt|nested a")
	require.Contains(t, got, "vault:/docs/b.txt|root b")
}

func TestDirToVaultEmptyDir(t *testing.T) {
	dir := t.TempDir()
	res, err := DirToVault(context.Background(), dir, "vault:/docs", func(ctx context.Context, r io.Reader, size int64, p string) (any, error) {
		return nil, nil
	})
	require.NoError(t, err)
	require.Equal(t, 0, res.Total)
	require.Equal(t, int64(0), res.Bytes)
	require.Len(t, res.Files, 0)
}
