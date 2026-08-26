package catalogops

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"go.lumeweb.com/pinner-cli/internal/core/config"
	configmocks "go.lumeweb.com/pinner-cli/internal/core/config/mocks"
	"go.lumeweb.com/pinner-cli/internal/core/download"
)

// stubDownloadService satisfies download.Service by embedding the interface and
// overriding ListDirectory. A path containing a "/" is treated as a listing of
// a subdirectory (the candidate wrapper), so tests can model a wrapper dir that
// does or does not contain index.html.
type stubDownloadService struct {
	download.Service
	rootEntries []download.DirEntry
	subEntries  []download.DirEntry
}

func (s *stubDownloadService) ListDirectory(_ context.Context, ipfsPath string) ([]download.DirEntry, error) {
	if strings.Contains(ipfsPath, "/") {
		return s.subEntries, nil
	}
	return s.rootEntries, nil
}

// structureDeps returns WebsitesDeps whose DownloadServiceFactory yields a fake
// download service returning the supplied root and subdirectory entries.
func structureDeps(t testing.TB, rootEntries, subEntries []download.DirEntry) WebsitesDeps {
	return WebsitesDeps{
		CfgMgr: func() config.Manager { return configmocks.NewMockManager(t) },
		Secure: func() bool { return false },
		DownloadServiceFactory: func(_ config.Manager, _ bool, _ string) (download.Service, error) {
			return &stubDownloadService{rootEntries: rootEntries, subEntries: subEntries}, nil
		},
		GetAuthToken: func() string { return "" },
	}
}

func TestWebsiteStructureValidatesRootIndexHTML(t *testing.T) {
	rootWithIndex := []download.DirEntry{
		{Name: "index.html", Type: "file"},
		{Name: "assets", Type: "directory"},
		{Name: "about.html", Type: "file"},
	}
	wrapperRoot := []download.DirEntry{
		{Name: "mysite", Type: "directory"},
	}
	wrapperWithIndex := []download.DirEntry{
		{Name: "index.html", Type: "file"},
		{Name: "assets", Type: "directory"},
	}
	singleDirNoIndex := []download.DirEntry{
		{Name: "assets", Type: "directory"},
		{Name: "about.html", Type: "file"},
	}

	cases := []struct {
		name        string
		rootEntries []download.DirEntry
		subEntries  []download.DirEntry
		wantErr     bool
		wantText    string
	}{
		{
			name:        "root index.html passes",
			rootEntries: rootWithIndex,
			wantErr:     false,
		},
		{
			name:        "wrapper directory detected",
			rootEntries: wrapperRoot,
			subEntries:  wrapperWithIndex,
			wantErr:     true,
			wantText:    "wrapped in an extra parent directory",
		},
		{
			name:        "single directory without index.html is not a wrapper",
			rootEntries: singleDirNoIndex,
			wantErr:     true,
			wantText:    "has no root index.html",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			deps := structureDeps(t, tc.rootEntries, tc.subEntries)
			err := validateWebsiteStructure(context.Background(), deps, map[string]any{}, "cid123", TargetTypeIPFS)
			if tc.wantErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.wantText)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestWebsiteStructurePermissiveWithoutDownloadService(t *testing.T) {
	// No DownloadServiceFactory → the guardrail must not block the operation.
	deps := WebsitesDeps{
		CfgMgr:       func() config.Manager { return configmocks.NewMockManager(t) },
		Secure:       func() bool { return false },
		GetAuthToken: func() string { return "" },
	}
	require.Nil(t, deps.DownloadServiceFactory)
	err := validateWebsiteStructure(context.Background(), deps, map[string]any{}, "cid123", TargetTypeIPFS)
	require.NoError(t, err)
}
