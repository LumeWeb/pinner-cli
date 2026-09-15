package cli

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ipfs "go.lumeweb.com/ipfs-sdk"

	"go.lumeweb.com/pinner/catalogops"
	"go.lumeweb.com/pinner/core/workspaces"
)

// fakeWorkspacesService is an in-memory workspaces.Service stub with
// call-recording for the resolution tests.
type fakeWorkspacesService struct {
	listFn  func(ctx context.Context, opts workspaces.ListOptions) ([]ipfs.WorkspaceResponse, error)
	getFn   func(ctx context.Context, id string) (*ipfs.WorkspaceResponse, error)
	getIDs  []string
	lists   int
	lastOpt workspaces.ListOptions
}

func (f *fakeWorkspacesService) RequireAuthenticated() error { return nil }
func (f *fakeWorkspacesService) SetAuthToken(token string)   {}

func (f *fakeWorkspacesService) List(ctx context.Context, opts workspaces.ListOptions) ([]ipfs.WorkspaceResponse, error) {
	f.lists++
	f.lastOpt = opts
	if f.listFn == nil {
		return nil, nil
	}
	return f.listFn(ctx, opts)
}

func (f *fakeWorkspacesService) Get(ctx context.Context, id string) (*ipfs.WorkspaceResponse, error) {
	f.getIDs = append(f.getIDs, id)
	if f.getFn == nil {
		return nil, nil
	}
	return f.getFn(ctx, id)
}

func (f *fakeWorkspacesService) Create(ctx context.Context, req ipfs.WorkspaceRequest) (*ipfs.WorkspaceResponse, error) {
	return nil, nil
}

func (f *fakeWorkspacesService) Delete(ctx context.Context, id string) (*ipfs.WorkspaceResponse, error) {
	f.getIDs = append(f.getIDs, id)
	return nil, nil
}

func (f *fakeWorkspacesService) Access(ctx context.Context, id string, rotate bool) (*ipfs.WorkspaceAccessResponse, error) {
	f.getIDs = append(f.getIDs, id)
	return nil, nil
}

func (f *fakeWorkspacesService) Attach(ctx context.Context, id string, websiteID int) (*ipfs.WorkspaceResponse, error) {
	f.getIDs = append(f.getIDs, id)
	return nil, nil
}

func (f *fakeWorkspacesService) Resume(ctx context.Context, id string) (*ipfs.WorkspaceResponse, error) {
	f.getIDs = append(f.getIDs, id)
	return nil, nil
}

func (f *fakeWorkspacesService) Suspend(ctx context.Context, id string) (*ipfs.WorkspaceResponse, error) {
	f.getIDs = append(f.getIDs, id)
	return nil, nil
}

var _ workspaces.Service = (*fakeWorkspacesService)(nil)

// tableOutput captures PrintTable calls while delegating everything else.
type tableOutput struct {
	Output
	headers []string
	rows    [][]string
}

func (t *tableOutput) PrintTable(headers []string, rows [][]string) {
	t.headers = headers
	t.rows = rows
}

func TestWorkspaceSlugFromLabel(t *testing.T) {
	assert.Equal(t, "ugki684o", workspaceSlugFromLabel("ws-ugki684o"))
	assert.Equal(t, "k7x4p9zq", workspaceSlugFromLabel("k7x4p9zq"))
	assert.Equal(t, "", workspaceSlugFromLabel(""))
}

func TestWorkspaceSlugID(t *testing.T) {
	assert.Equal(t, "ugki684o", workspaceSlugID(&ipfs.WorkspaceResponse{Id: 9, Label: "ws-ugki684o"}))
	assert.Equal(t, "k7x4p9zq", workspaceSlugID(&ipfs.WorkspaceResponse{Id: 9, Label: "k7x4p9zq"}))
	assert.Equal(t, "7", workspaceSlugID(&ipfs.WorkspaceResponse{Id: 7}))
	assert.Equal(t, "", workspaceSlugID(nil))
}

func TestLabelResolvingWorkspaces_NumericIDPassesThrough(t *testing.T) {
	fake := &fakeWorkspacesService{}
	svc := wrapLabelResolvingWorkspaces(fake)

	_, err := svc.Get(context.Background(), "42")
	require.NoError(t, err)

	assert.Equal(t, []string{"42"}, fake.getIDs)
	assert.Zero(t, fake.lists, "numeric ids must not trigger a list scan")
}

func TestLabelResolvingWorkspaces_ResolvesSlugAcrossPages(t *testing.T) {
	fake := &fakeWorkspacesService{
		listFn: func(ctx context.Context, opts workspaces.ListOptions) ([]ipfs.WorkspaceResponse, error) {
			if opts.Start == 0 {
				page := make([]ipfs.WorkspaceResponse, workspaceScanPageSize)
				for i := range page {
					page[i] = ipfs.WorkspaceResponse{Id: i + 1, Label: fmt.Sprintf("ws-fill%03d", i)}
				}
				return page, nil
			}
			return []ipfs.WorkspaceResponse{{Id: 9, Label: "ws-ugki684o"}}, nil
		},
		getFn: func(ctx context.Context, id string) (*ipfs.WorkspaceResponse, error) {
			return &ipfs.WorkspaceResponse{Id: 9, Label: "ws-ugki684o"}, nil
		},
	}
	svc := wrapLabelResolvingWorkspaces(fake)

	ws, err := svc.Get(context.Background(), "ugki684o")
	require.NoError(t, err)
	require.NotNil(t, ws)
	assert.Equal(t, 9, ws.Id)

	// The numeric id arrived at the backend.
	assert.Equal(t, []string{"9"}, fake.getIDs)
	// The scan paged with the configured page size.
	assert.GreaterOrEqual(t, fake.lists, 2)
	assert.Equal(t, workspaceScanPageSize, fake.lastOpt.Limit)
}

func TestLabelResolvingWorkspaces_AcceptsFullLegacyLabelAndBareLabel(t *testing.T) {
	fake := &fakeWorkspacesService{
		listFn: func(ctx context.Context, opts workspaces.ListOptions) ([]ipfs.WorkspaceResponse, error) {
			return []ipfs.WorkspaceResponse{{Id: 9, Label: "ws-ugki684o"}}, nil
		},
	}
	svc := wrapLabelResolvingWorkspaces(fake)

	for _, id := range []string{"ws-ugki684o", "ugki684o"} {
		_, err := svc.Get(context.Background(), id)
		require.NoError(t, err, "id %q should resolve", id)
	}
}

func TestLabelResolvingWorkspaces_NotFound(t *testing.T) {
	fake := &fakeWorkspacesService{
		listFn: func(ctx context.Context, opts workspaces.ListOptions) ([]ipfs.WorkspaceResponse, error) {
			return []ipfs.WorkspaceResponse{{Id: 9, Label: "ws-other1"}}, nil
		},
	}
	svc := wrapLabelResolvingWorkspaces(fake)

	_, err := svc.Get(context.Background(), "ugki6840")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `workspace "ugki6840" not found`)
	assert.Empty(t, fake.getIDs, "no backend call may fire for an unresolvable label")
}

func TestRenderWorkspacesListResult_HumanTableUsesSlugIDAndDropsLabel(t *testing.T) {
	created := parseTime(t, "2026-09-14 22:27:13")
	websiteID := 4
	result := catalogops.NewListResult([]ipfs.WorkspaceResponse{
		{Id: 9, Domain: "ws-ugki684o.build.pinned.site", Label: "ws-ugki684o", Status: "deleting", WebsiteId: &websiteID, Created: created},
		{Id: 10, Domain: "k7x4p9zq.build.pinned.site", Label: "k7x4p9zq", Status: "running", Created: created},
	}, catalogops.ListResultMeta{
		Noun:    "workspace(s)",
		Headers: []string{"ID", "DOMAIN", "LABEL", "STATUS", "WEBSITE ID", "CREATED"},
		Rows: [][]string{
			{"9", "ws-ugki684o.build.pinned.site", "ws-ugki684o", "deleting", "4", "2026-09-14 22:27:13"},
			{"10", "k7x4p9zq.build.pinned.site", "k7x4p9zq", "running", "-", "2026-09-14 22:27:13"},
		},
	})

	out := &tableOutput{Output: newTestOutput()}
	require.NoError(t, renderWorkspacesListResult(out, result))

	assert.Equal(t, []string{"ID", "DOMAIN", "STATUS", "WEBSITE ID", "CREATED"}, out.headers)
	require.Len(t, out.rows, 2)
	assert.Equal(t, []string{"ugki684o", "ws-ugki684o.build.pinned.site", "deleting", "4", "2026-09-14 22:27:13"}, out.rows[0])
	assert.Equal(t, []string{"k7x4p9zq", "k7x4p9zq.build.pinned.site", "running", "-", "2026-09-14 22:27:13"}, out.rows[1])
}

func TestRenderWorkspaceHuman_IDIsSlugAndNoLabelRow(t *testing.T) {
	websiteID := 4
	fake := &fieldsOutput{Output: newTestOutput()}
	renderWorkspaceHuman(fake, &ipfs.WorkspaceResponse{
		Id:        9,
		Domain:    "ws-ugki684o.build.pinned.site",
		Label:     "ws-ugki684o",
		Status:    "deleting",
		WebsiteId: &websiteID,
	})

	require.Len(t, fake.groups, 1)
	require.Len(t, fake.groups[0].Fields, 6)
	assert.Equal(t, Field{"ID", "ugki684o"}, fake.groups[0].Fields[0])
	assert.Equal(t, Field{"Website ID", "4"}, fake.groups[0].Fields[3])
}

// fieldsOutput captures PrintFields groups while delegating everything else.
type fieldsOutput struct {
	Output
	groups []FieldGroup
}

func (f *fieldsOutput) PrintFields(group FieldGroup) {
	f.groups = append(f.groups, group)
}

func parseTime(t *testing.T, s string) time.Time {
	t.Helper()
	parsed, err := time.Parse("2006-01-02 15:04:05", s)
	require.NoError(t, err)
	return parsed
}
