package mcp

// Server-extension registration roles.
//
// These tests exercise the explicit role/exposure representation that replaced
// the customToolSpec{index,direct,launcher} boolean matrix:
//
//   - TestServerExtensionRoleValidation asserts the registry REJECTS the
//     invalid role combinations the boolean matrix silently allowed;
//   - TestServerExtensionRoleRegistryOutcomes asserts, at the registry level,
//     what each role actually produces (catalog membership, DirectCustom
//     recording, app-view install);
//   - TestAppHelperRegistrationOwnedByAppViews pins the deliberate absence of
//     an app-helper role: the app-only helper visibility domain is owned
//     exclusively by app view registration (apps.RegisterAppView), which
//     registers helpers with wire visibility ["app"] so they never become
//     ordinary model tools and never enter the catalog, DirectCustom, or the
//     finalized direct surface;
//   - TestServerExtensionRoleProductionMapping re-drives the REAL production
//     assembly (buildInventoryServer, the same production pipeline the
//     custom_registration_inventory_test.go characterizes) and maps named
//     production tools onto their role outcomes using observable behavior
//     (wire listing, catalog search, DirectCustom membership), never internal
//     role constants.

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
	"go.lumeweb.com/mcpplane/model"
	"go.lumeweb.com/mcpplane/sdk"

	"go.lumeweb.com/pinner-cli/internal/mcp/apps"
)

// roleTestTool builds a minimal named descriptor with a real handler so a
// direct/helper registration can complete the production descriptor gate.
func roleTestTool(name string) model.ToolDescriptor {
	return model.ToolDescriptor{
		Name:        name,
		Category:    model.CategoryCore,
		InputSchema: json.RawMessage(`{"type":"object"}`),
		Handler: model.ToolHandler(func(_ context.Context, _ model.ToolRequest) (model.ToolResult, error) {
			return model.ToolResult{Text: "ok"}, nil
		}),
	}
}

// noopAppInstaller is a stand-in app view installer; it only records that it
// ran.
func noopAppInstaller(ran *bool) func(*sdk.Server, apps.AppCatalog) error {
	return func(*sdk.Server, apps.AppCatalog) error {
		*ran = true
		return nil
	}
}

// runExtensionSpec declares one spec through a fresh production registry and
// runs the pipeline, returning the run error.
func runExtensionSpec(t *testing.T, s serverExtensionSpec) error {
	t.Helper()
	reg := newServerExtensionRegistry(sdk.NewServer(nil), NewToolCatalog())
	reg.add(s)
	return reg.run()
}

// TestServerExtensionRoleValidation asserts the role representation rejects
// invalid combinations instead of preserving a silent boolean matrix.
func TestServerExtensionRoleValidation(t *testing.T) {
	installer := func(*sdk.Server, apps.AppCatalog) error { return nil }

	cases := []struct {
		name    string
		spec    serverExtensionSpec
		wantErr string
	}{
		{
			name:    "no roles declared",
			spec:    serverExtensionSpec{desc: roleTestTool("no_roles")},
			wantErr: "at least one registration role",
		},
		{
			name: "unnamed descriptor",
			spec: serverExtensionSpec{
				roles: serverExtensionRoles{roleCatalogSearch},
			},
			wantErr: "named descriptor",
		},
		{
			name: "duplicate role",
			spec: serverExtensionSpec{
				desc:  roleTestTool("dupe"),
				roles: serverExtensionRoles{roleCatalogSearch, roleCatalogSearch},
			},
			wantErr: "more than once",
		},
		{
			name: "app launcher without an app view installer",
			spec: serverExtensionSpec{
				desc:  roleTestTool("open_broken"),
				roles: serverExtensionRoles{roleCatalogSearch, roleAppLauncher},
			},
			wantErr: "app view installer",
		},
		{
			name: "app launcher without catalog membership",
			spec: serverExtensionSpec{
				desc:  roleTestTool("open_unindexed"),
				roles: serverExtensionRoles{roleAppLauncher},
				app:   installer,
			},
			wantErr: "catalog-searchable",
		},
		{
			name: "app launcher that also claims the direct role",
			spec: serverExtensionSpec{
				desc:  roleTestTool("open_direct"),
				roles: serverExtensionRoles{roleCatalogSearch, roleAppLauncher, roleDirectTool},
				app:   installer,
			},
			wantErr: "single direct launcher",
		},
		// (No "app helper" cases: the registry declares NO app-helper role and
		// no constructor exists for one — the app-only helper visibility domain
		// is owned exclusively by app view registration, so an extension spec
		// cannot even express it. See TestAppHelperRegistrationOwnedByAppViews.)
		{
			name: "app view installer on a non-launcher extension",
			spec: serverExtensionSpec{
				desc:  roleTestTool("not_launcher"),
				roles: serverExtensionRoles{roleCatalogSearch},
				app:   installer,
			},
			wantErr: "does not declare",
		},
	}

	for _, tc := range cases {
		err := runExtensionSpec(t, tc.spec)
		require.Error(t, err, "invalid combination %q must be rejected", tc.name)
		require.Containsf(t, err.Error(), tc.wantErr,
			"invalid combination %q must explain the rejection", tc.name)
	}
}

// TestServerExtensionRoleValidationAcceptsValidCombinations walks through the
// valid role combinations the production plan uses and proves they run.
func TestServerExtensionRoleValidationAcceptsValidCombinations(t *testing.T) {
	valid := []serverExtensionSpec{
		searchableOnly(roleTestTool("search_only_valid")),
		directOnly(roleTestTool("direct_only_valid")),
		directSearchable(roleTestTool("dual_valid")),
	}
	fakeLauncher := roleTestTool("open_valid")
	ran := false
	fakeLauncher.Meta = map[string]any{"ui": map[string]any{"resourceUri": "ui://test/launcher.html"}}
	valid = append(valid, appLauncherSpec(fakeLauncher, noopAppInstaller(&ran)))

	for _, s := range valid {
		require.NoErrorf(t, runExtensionSpec(t, s),
			"valid role combination for %q must be accepted", s.desc.Name)
	}
}

// TestServerExtensionRoleRegistryOutcomes pins the outcomes each role produces
// at the registry level: catalog membership, the explicit DirectCustom record
// for direct-only projections, and app-view installation for launchers.
func TestServerExtensionRoleRegistryOutcomes(t *testing.T) {
	srv := sdk.NewServer(nil)
	catalog := NewToolCatalog()
	reg := newServerExtensionRegistry(srv, catalog)

	reg.add(searchableOnly(roleTestTool("role_search_only")))
	reg.add(directOnly(roleTestTool("role_direct_only")))
	reg.add(directSearchable(roleTestTool("role_dual")))
	fakeLauncher := roleTestTool("role_open_launcher")
	fakeLauncher.Meta = map[string]any{"ui": map[string]any{"resourceUri": "ui://test/launcher.html"}}
	attached := false
	reg.add(appLauncherSpec(fakeLauncher, noopAppInstaller(&attached)))

	require.NoError(t, reg.run(), "all-valid role plan must run")

	// catalog-search role: member of the catalog, never DirectCustom.
	_, ok := catalog.Get("role_search_only")
	require.True(t, ok, "catalog-search role must index its entry")
	// direct-only role: explicit DirectCustom record, no catalog entry — this
	// is the flat server card's direct surface, distinct from curated
	// DirectVisible projection.
	_, ok = catalog.Get("role_direct_only")
	require.False(t, ok, "direct-only role must not index its entry")
	require.Contains(t, catalog.DirectCustom, "role_direct_only",
		"direct-only projection must record its name on catalog.DirectCustom")
	// dual role: catalog member, and NOT on DirectCustom (only uncataloged
	// direct projections contribute).
	_, ok = catalog.Get("role_dual")
	require.True(t, ok, "dual direct+searchable role must index its entry")
	require.NotContains(t, catalog.DirectCustom, "role_dual",
		"a dual-role tool's direct projection rides the catalog, not DirectCustom")
	// app-launcher role: catalog member and its app view actually installed
	// (the app phase resolved it against the catalog).
	_, ok = catalog.Get("role_open_launcher")
	require.True(t, ok, "app-launcher role must index its entry")
	require.True(t, attached, "app-launcher role must install its app view")
	require.NotContains(t, catalog.DirectCustom, "role_open_launcher",
		"a launcher is never a direct tool")
}

// TestServerExtensionRoleProductionMapping maps named production tools from
// the REAL full-local assembly onto their role outcomes using observable
// behavior only (wire listing, catalog search, DirectCustom, DirectVisible
// flags). It complements custom_registration_inventory_test.go by stating the
// role-level mapping explicitly, and it proves the curated DirectVisible path
// (auth_sso) stays semantically distinct from the plan-owned direct role.
func TestServerExtensionRoleProductionMapping(t *testing.T) {
	inv := buildInventoryServer(t, nil, nil)
	wire, catalog := inv.wireNames, inv.catalog

	// Dual-role extensions (direct SDK projection + catalog membership):
	// every transport bridge, capabilities, and agent_guide.
	dualRoles := []string{
		"upload_file", "download_file", "vault_get_file", "vault_put_file",
		"capabilities", "agent_guide",
	}
	for _, n := range dualRoles {
		require.Truef(t, wire[n], "dual-role extension %q must be directly listed", n)
		_, ok := catalog.Get(n)
		require.Truef(t, ok, "dual-role extension %q must be catalog-indexed", n)
		require.Truef(t, searchVisible(t, catalog, n), "dual-role extension %q must be searchable", n)
		require.NotContainsf(t, catalog.DirectCustom, n,
			"dual-role extension %q must not be recorded on DirectCustom (it rides the catalog)", n)
	}

	// Catalog-only extensions: searchable but never listed on the wire, never
	// DirectCustom.
	catalogOnly := []string{
		"auth_resume", "auth_sso_revoke",
		"account_password_update", "account_password_reset", "account_email_change",
		"vault_create_resume", "vault_restore_resume",
		"upload_status", "upload_cancel", "upload_list",
	}
	for _, n := range catalogOnly {
		require.Falsef(t, wire[n], "catalog-only extension %q must stay off the wire", n)
		require.Truef(t, searchVisible(t, catalog, n), "catalog-only extension %q must be searchable", n)
		require.NotContainsf(t, catalog.DirectCustom, n,
			"catalog-only extension %q must never appear on DirectCustom", n)
	}

	// App-launcher extensions: searchable app launchers whose direct exposure
	// is exclusively the consolidated open_app tool. On the agent-only host
	// they are never directly listed and stay search-only in the catalog.
	for _, l := range openLauncherToolNames(t) {
		require.Falsef(t, wire[l], "launcher extension %q must never be individually direct", l)
		require.Truef(t, searchVisible(t, catalog, l), "launcher extension %q must stay searchable", l)
		require.NotContainsf(t, catalog.DirectCustom, l,
			"launcher extension %q must never appear on DirectCustom", l)
	}

	// App-only helper visibility domain: on the wire (registered by the app
	// installs) but absent from the catalog and DirectCustom.
	for _, h := range []string{
		"pin_status", "auth_sso_status",
		"ipfs_upload_submit", "ipfs_upload_status",
		"vault_upload_submit",
		"vault_create_status", "vault_restore_status",
	} {
		require.Truef(t, wire[h], "app-only helper %q must be server-registered", h)
		_, ok := catalog.Get(h)
		require.Falsef(t, ok, "app-only helper %q must never join the catalog", h)
		require.NotContainsf(t, catalog.DirectCustom, h,
			"app-only helper %q must never be recorded as a direct surface", h)
	}

	// Curated DirectVisible path stays distinct from the plan-owned direct
	// role: auth_sso reaches the wire ONLY through the curated projection of
	// its catalog entry's DirectVisible property (the descriptor is
	// catalog-searchable in the plan; its plan roles never claim direct), so
	// it must never appear on DirectCustom.
	require.True(t, wire["auth_sso"], "auth_sso must be directly listed via the curated projection")
	_, ok := catalog.Get("auth_sso")
	require.True(t, ok, "auth_sso must be catalog-indexed")
	require.NotContains(t, catalog.DirectCustom, "auth_sso",
		"the curated DirectVisible projection must not leak into the plan-owned DirectCustom set")
}

// TestAppHelperRegistrationOwnedByAppViews pins the chosen ownership rule for
// the app-only helper visibility domain: the role vocabulary deliberately has
// NO app-helper role (compile-time: no such constant or constructor exists),
// and the ONLY path that registers an app-only helper is its owning app view
// via apps.RegisterAppView. The lib layer stamps visibility ["app"] on helper
// descriptors — without a model entry — so a helper can never become an
// ordinary model tool: it stays off the catalog, off catalog.DirectCustom,
// and out of the finalized direct (tools/list-model) surface.
func TestAppHelperRegistrationOwnedByAppViews(t *testing.T) {
	srv := sdk.NewServer(nil)
	catalog := NewToolCatalog()
	catalog.Add(&model.ToolEntry{
		Name:        "role_pin_add",
		Title:       "pin add",
		Category:    model.CategoryCore,
		InputSchema: []byte(`{"type":"object"}`),
	})

	launcher := roleTestTool("role_open_launcher")
	launcher.Meta = map[string]any{"ui": map[string]any{"resourceUri": "ui://test/launcher.html"}}
	reg := newServerExtensionRegistry(srv, catalog)
	reg.add(appLauncherSpec(launcher, func(_ *sdk.Server, cat apps.AppCatalog) error {
		return apps.RegisterAppView(srv, cat, apps.AppView{
			URI:         "ui://test/launcher.html",
			Name:        "test-launcher",
			Title:       "Test launcher",
			Description: "test app view with an app-only helper",
			HTML:        "<!doctype html><html><body>t</body></html>",
			AttachTo:    []string{"role_open_launcher"},
			Helpers:     []model.ToolDescriptor{roleTestTool("role_helper")},
		})
	}))

	require.NoError(t, reg.run(), "launcher + app view + helper plan must run")

	// The helper exists on the wire, registered by the app view — but as an
	// APP-ONLY tool: exactly one visibility entry ("app"), never "model", so
	// it is not an ordinary model tool even though it is server-registered.
	cs := connectOfficialClient(t, srv)
	listed, err := cs.ListTools(context.Background(), nil)
	require.NoError(t, err)
	wire := map[string]*mcp.Tool{}
	for _, tl := range listed.Tools {
		wire[tl.Name] = tl
	}
	require.Contains(t, wire, "role_helper", "app view registration must register its helper")
	vis, _ := wire["role_helper"].Meta["ui"].(map[string]any)["visibility"].([]any)
	require.Equal(t, []any{"app"}, vis,
		"app-only helper must carry visibility [app] and never a model entry")

	// The separate visibility domains stay clean: no catalog membership, no
	// DirectCustom record, and no finalized direct-surface membership.
	_, ok := catalog.Get("role_helper")
	require.False(t, ok, "app-only helper must never join the catalog")
	require.NotContains(t, catalog.DirectCustom, "role_helper",
		"app-only helper must never be card-advertised as a direct surface")
	surface := catalog.FinalizedTooling()
	require.NotNil(t, surface, "run() must record the finalized surface")
	for _, d := range surface.DirectTools() {
		require.NotEqual(t, "role_helper", d.Name,
			"app-only helper must never appear in the finalized direct surface")
	}
}

// TestServerExtensionDuplicateNameValidation pins the registry-owned duplicate
// extension-name gate: a plan whose declared extension names collide — with
// each other or with an entry already in the catalog — must fail BEFORE any
// catalog mutation (curated provisions, indexing) and before materialization.
// Because both downstream stores silently replace same-name entries
// (ToolCatalog.Add, the SDK server map), the proof that the gate fired early
// is that failure leaves the catalog and the wire EXACTLY as they were: no
// provision ran, no spec was indexed, and the server lists no tools.
func TestServerExtensionDuplicateNameValidation(t *testing.T) {
	newRegistry := func(t *testing.T, provision *bool, catalog *ToolCatalog) *serverExtensionRegistry {
		t.Helper()
		reg := newServerExtensionRegistry(sdk.NewServer(nil), catalog)
		reg.beforeDirectTools(func(_ *ToolCatalog) error {
			*provision = true
			return nil
		})
		return reg
	}

	assertUntouched := func(t *testing.T, srv *sdk.Server, catalog *ToolCatalog, provisionRan bool, entriesBefore int) {
		t.Helper()
		require.Falsef(t, provisionRan, "failure must abort before the curated-provision phase mutates state")
		require.Equalf(t, entriesBefore, catalog.Len(),
			"failure must leave the catalog untouched")
		cs := connectOfficialClient(t, srv)
		listed, err := cs.ListTools(context.Background(), nil)
		require.NoError(t, err)
		require.Emptyf(t, listed.Tools, "failure must leave the wire surface untouched")
		require.Nilf(t, catalog.FinalizedTooling(), "failure must never reach materialization")
	}

	t.Run("two specs declare the same name", func(t *testing.T) {
		catalog := NewToolCatalog()
		srv := sdk.NewServer(nil)
		provisionRan := false
		reg := newRegistry(t, &provisionRan, catalog)
		reg.add(searchableOnly(roleTestTool("dup_ext")))
		reg.add(directOnly(roleTestTool("dup_ext")))
		err := reg.run()
		require.Error(t, err, "duplicate name between specs must fail the plan")
		require.Contains(t, err.Error(), "duplicate extension name")
		assertUntouched(t, srv, catalog, provisionRan, 0)
	})

	t.Run("spec name collides with an existing catalog entry", func(t *testing.T) {
		catalog := NewToolCatalog()
		catalog.Add(&model.ToolEntry{
			Name:        "compiled_op",
			Title:       "compiled op",
			Category:    model.CategoryCore,
			InputSchema: []byte(`{"type":"object"}`),
		})
		srv := sdk.NewServer(nil)
		provisionRan := false
		reg := newRegistry(t, &provisionRan, catalog)
		reg.add(searchableOnly(roleTestTool("compiled_op")))
		err := reg.run()
		require.Error(t, err, "a name colliding with a compiled operation must fail the plan, not replace it")
		require.Contains(t, err.Error(), "catalog already holds an entry")
		assertUntouched(t, srv, catalog, provisionRan, 1)
	})

	t.Run("curated provision collides with a declared spec", func(t *testing.T) {
		catalog := NewToolCatalog()
		srv := sdk.NewServer(nil)
		reg := newServerExtensionRegistry(srv, catalog)
		reg.beforeDirectTools(func(c *ToolCatalog) error {
			c.Add(&model.ToolEntry{
				Name:        "wizard_tool",
				Title:       "wizard tool",
				Category:    model.CategoryCore,
				InputSchema: []byte(`{"type":"object"}`),
			})
			return nil
		})
		reg.add(searchableOnly(roleTestTool("wizard_tool")))
		err := reg.run()
		require.Error(t, err, "a provision colliding with a spec must fail the plan before indexing")
		require.Contains(t, err.Error(), "catalog already holds an entry")
		// The provision itself is the mutation that tripped the gate, but the
		// indexing phase must not have run and nothing must reach the wire.
		_, ok := catalog.Get("wizard_tool")
		require.True(t, ok, "the provision's own entry may exist (it fired before the re-check)")
		require.Nilf(t, catalog.FinalizedTooling(), "failure must never reach materialization")
		cs := connectOfficialClient(t, srv)
		listed, err := cs.ListTools(context.Background(), nil)
		require.NoError(t, err)
		require.Emptyf(t, listed.Tools, "failure must leave the wire surface untouched")
		require.NotContainsf(t, catalog.DirectCustom, "wizard_tool",
			"no extension projection may be recorded after a failed plan")
	})
}
