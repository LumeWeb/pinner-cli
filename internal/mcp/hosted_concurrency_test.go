package mcp

import (
	"context"
	"io"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lumeweb.com/mcpplane/sdk"
	mcptransfer "go.lumeweb.com/mcpplane/transfer"
	"go.lumeweb.com/pinner-cli/internal/mcp/apps"
	"go.lumeweb.com/pinner-cli/internal/mcp/upload"
)

// TestBuildHostedServerConcurrentViewDomainAssemblies is the concurrency
// regression for the process-global app registry: multiple hosted assemblies
// with DISTINCT deployment origins built concurrently must each attribute
// their registered ui:// views to their OWN origin. With the plain
// install/deferred-clear pair this interleaved (assembly B's resolver clobbered
// assembly A's views, or A's teardown cleared B's resolver mid-registration),
// so BuildHostedServer now runs its app-registration window inside the
// serialized apps.ForViewDomainResolver window (see
// internal/mcp/apps/registrar.go for the documented one-server-at-a-time
// assembly restriction).
func TestBuildHostedServerConcurrentViewDomainAssemblies(t *testing.T) {
	// Preserve whatever resolver is installed for other tests.
	previous := apps.ViewDomainResolver()
	apps.SetViewDomainResolver(nil)
	t.Cleanup(func() { apps.SetViewDomainResolver(previous) })

	const assemblies = 4
	servers := make([]*sdk.Server, assemblies)

	var wg sync.WaitGroup
	errs := make([]error, assemblies)
	for i := 0; i < assemblies; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			tasks := mcptransfer.NewUploadTaskManager(func(ctx context.Context, r io.Reader, size int64, name string, wait bool, archiveMode string, wrap bool) (any, error) {
				return map[string]any{"cid": "QmTest"}, nil
			}, 0)
			srv, _, _, err := BuildHostedServer(HostedServerConfig{
				CatalogDeps: func() *CatalogDepsBundle { return &CatalogDepsBundle{} },
				// Distinct origin per assembly (alternate two hosts).
				Options: []MCPServerOption{WithUploadTaskManager(tasks)},
				BaseURL: hostedRaceOriginFor(i),
			})
			servers[i] = srv
			errs[i] = err
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		require.NoErrorf(t, err, "hosted assembly %d must build", i)
	}

	// Every assembly's ui:// upload view carries ITS OWN origin, never a
	// sibling's (the race symptom was views losing their origin or inheriting
	// whichever assembly ran last).
	for i := 0; i < assemblies; i++ {
		srv := servers[i]
		cs := connectOfficialClient(t, srv)
		res, err := cs.ListResources(context.Background(), nil)
		require.NoErrorf(t, err, "assembly %d list resources", i)
		var uploadView *mcp.Resource
		for _, r := range res.Resources {
			if r.URI == upload.IPFSUploadAppURI {
				uploadView = r
				break
			}
		}
		require.NotNilf(t, uploadView, "assembly %d must register its upload app view", i)
		ui, ok := uploadView.Meta["ui"].(map[string]any)
		require.Truef(t, ok, "assembly %d upload view carries _meta.ui", i)
		assert.Equalf(t, hostedRaceOriginFor(i), ui["domain"],
			"assembly %d's view must attribute its own origin, not a concurrently-assembling sibling's", i)
	}
}

// TestBuildHostedServerEmptyOriginSerializedAgainstResolver is the concurrency
// regression for the empty-origin assembly path: a hosted assembly with NO
// BaseURL previously skipped the serialized resolver window entirely, so while
// an origin-bearing sibling assembly was inside its window (resolver globally
// installed), the empty-origin assembly's ui:// views could inherit the
// sibling's origin instead of carrying no domain. Both assemblies must now run
// through the same serialized apps.ForViewDomainResolver window (regardless of
// origin), so the empty-origin server's views must end up with NO ui.domain
// even when assembled concurrently with an origin-bearing assembly.
func TestBuildHostedServerEmptyOriginSerializedAgainstResolver(t *testing.T) {
	previous := apps.ViewDomainResolver()
	apps.SetViewDomainResolver(nil)
	t.Cleanup(func() { apps.SetViewDomainResolver(previous) })

	const assemblies = 2 // [0] = origin-bearing, [1] = empty origin
	baseURLs := []string{"https://hosted-c.example.com", ""}
	servers := make([]*sdk.Server, assemblies)

	var wg sync.WaitGroup
	errs := make([]error, assemblies)
	for i := 0; i < assemblies; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			tasks := mcptransfer.NewUploadTaskManager(func(ctx context.Context, r io.Reader, size int64, name string, wait bool, archiveMode string, wrap bool) (any, error) {
				return map[string]any{"cid": "QmTest"}, nil
			}, 0)
			srv, _, _, err := BuildHostedServer(HostedServerConfig{
				CatalogDeps: func() *CatalogDepsBundle { return &CatalogDepsBundle{} },
				Options:     []MCPServerOption{WithUploadTaskManager(tasks)},
				BaseURL:     baseURLs[i],
			})
			servers[i] = srv
			errs[i] = err
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		require.NoErrorf(t, err, "hosted assembly %d must build", i)
	}

	uploadViewMetaUI := func(i int, what string) map[string]any {
		t.Helper()
		cs := connectOfficialClient(t, servers[i])
		res, err := cs.ListResources(context.Background(), nil)
		require.NoErrorf(t, err, "assembly %d list resources (%s)", i, what)
		for _, r := range res.Resources {
			if r.URI == upload.IPFSUploadAppURI {
				ui, ok := r.Meta["ui"].(map[string]any)
				require.Truef(t, ok, "assembly %d upload view carries _meta.ui (%s)", i, what)
				return ui
			}
		}
		require.Failf(t, "missing upload view", "assembly %d must register its upload app view (%s)", i, what)
		return nil
	}

	// The origin-bearing assembly must still attribute its own origin.
	assert.Equalf(t, baseURLs[0], uploadViewMetaUI(0, "origin-bearing")["domain"],
		"origin-bearing assembly's view must attribute its own origin")
	// The empty-origin assembly must carry NO domain — it must not have
	// inherited the sibling's origin while both windows raced.
	assert.Emptyf(t, uploadViewMetaUI(1, "empty-origin")["domain"],
		"empty-origin assembly's view must carry no ui.domain, not a concurrently-assembling sibling's origin")
}

func hostedRaceOriginFor(i int) string {
	if i%2 == 0 {
		return "https://hosted-a.example.com"
	}
	return "https://hosted-b.example.com"
}
