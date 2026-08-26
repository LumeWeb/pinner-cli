package catalogops

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"go.lumeweb.com/pinner-cli/internal/core/config"
	configmocks "go.lumeweb.com/pinner-cli/internal/core/config/mocks"
	"go.lumeweb.com/pinner-cli/internal/core/download"
)

// stubDownloadService satisfies download.Service by embedding the interface and
// overriding only ListDirectory. Every other method is a no-op stub.
type stubDownloadService struct {
	download.Service
	entries []download.DirEntry
}

func (s *stubDownloadService) ListDirectory(_ context.Context, _ string) ([]download.DirEntry, error) {
	return s.entries, nil
}

// structureDeps returns WebsitesDeps whose DownloadServiceFactory yields a fake
// download service returning the supplied directory entries.
func structureDeps(t testing.TB, entries []download.DirEntry) WebsitesDeps {
	return WebsitesDeps{
		CfgMgr: func() config.Manager { return configmocks.NewMockManager(t) },
		Secure: func() bool { return false },
		DownloadServiceFactory: func(_ config.Manager, _ bool, _ string) (download.Service, error) {
			return &stubDownloadService{entries: entries}, nil
		},
		GetAuthToken: func() string { return "" },
	}
}

func TestWebsiteStructureValidatesRootIndexHTML(t *testing.T) {
	cases := []struct {
		name     string
		entries  []download.DirEntry
		wantErr  bool
		wantText string
	}{
		{
			name: "root index.html passes",
			entries: []download.DirEntry{
				{Name: "index.html", Type: "file"},
				{Name: "assets", Type: "directory"},
				{Name: "about.html", Type: "file"},
			},
			wantErr: false,
		},
		{
			name: "wrapper directory detected",
			entries: []download.DirEntry{
				{Name: "mysite", Type: "directory"},
			},
			wantErr:  true,
			wantText: "wrapped in an extra parent directory",
		},
		{
			name: "missing root index.html",
			entries: []download.DirEntry{
				{Name: "assets", Type: "directory"},
				{Name: "about.html", Type: "file"},
			},
			wantErr:  true,
			wantText: "has no root index.html",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			deps := structureDeps(t, tc.entries)
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
