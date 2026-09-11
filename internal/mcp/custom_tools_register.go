package mcp

import (
	"fmt"
	"strings"

	"go.lumeweb.com/mcpplane/model"
	"go.lumeweb.com/mcpplane/sdk"
	"go.lumeweb.com/pinner-cli/internal/mcp/apps"
)

// serverExtensionRole names one explicit registration role a server extension
// plays in the per-server assembly plan. It replaces the former
// customToolSpec{index,direct,launcher} boolean matrix: instead of overlapping
// booleans whose meaning depended on the combination, every extension declares
// the roles it plays and the registry validates declared combinations,
// rejecting invalid ones instead of silently resolving a boolean override.
//
// The role vocabulary deliberately separates the two exposure axes the boolean
// spec conflated:
//
//   - roleCatalogSearch (catalog membership: searchable via search_tools,
//     describable, invocable through the typed dispatchers) is the
//     progressive-discovery domain;
//   - roleDirectTool is the explicit direct SDK projection performed by this
//     plan (RegisterOfficialDescriptor onto tools/list during the single
//     materialization pass). It is intentionally NOT
//     ToolEntry.DirectVisible: DirectVisible is a catalog-entry property
//     projected by the direct phase (stampDirectTools +
//     RegisterOfficialDirectTools), while roleDirectTool is an unconditional
//     registration owned by the extension plan itself. The materialization
//     pass de-duplicates the two projections by name BEFORE registering, so a
//     flat materialization that stamps an indexed direct extension
//     DirectVisible never projects the same tool twice.
//
// Domain kinds map onto the roles as follows: transport bridges and product
// affordances (upload/download/vault relays, capabilities, agent_guide) are
// direct and usually also catalog-searchable; OOB handoff tools (auth_sso, the
// *_resume pair, the account tools) are catalog-searchable — their direct
// exposure, where any, comes from the descriptor's own DirectVisible property
// through the direct phase; open_* launchers are roleAppLauncher (searchable
// app launcher plus its app view attachment, never individually direct); wizard
// tools and dev tools provision direct-visible catalog entries through
// beforeDirectTools; resources and prompts ride the same plan through afterSurface.
//
// App-only helpers (the iframe-only pollers/setup tools an app view drives) are
// deliberately NOT part of this role vocabulary: no spec declared through this
// registry becomes an app-only helper. The app-only visibility domain is owned
// exclusively by app view registration (apps.RegisterAppView), which carries the
// helper descriptors with their owning app/resource metadata and stamps
// visibility=["app"] on the wire so compliant hosts keep them out of the model
// surface. A standalone extension spec registering the same descriptor would
// expose an ordinary model-visible tool with app-only semantics, so the
// vocabulary has no app-helper role and app registration is its single path.
type serverExtensionRole string

const (
	// roleCatalogSearch makes the extension's descriptor a ToolCatalog member:
	// discoverable through search_tools/describe_tool/typed-invoke
	// dispatchers. App views resolve their launchers against the catalog, so
	// catalog membership always precedes app install.
	roleCatalogSearch serverExtensionRole = "catalog-search"

	// roleDirectTool projects the extension's descriptor onto tools/list via
	// the explicit direct SDK registration (RegisterOfficialDescriptor). This
	// is deliberately distinct from ToolEntry.DirectVisible, which is the
	// direct phase's catalog-entry projection. A direct-only extension (this
	// role without roleCatalogSearch) is additionally recorded on
	// catalog.DirectCustom, so a flat server's card consults an explicit set
	// instead of inferring direct membership from registration order.
	roleDirectTool serverExtensionRole = "direct-tool"

	// roleAppLauncher marks an open_* UI launcher. Launchers are projected
	// onto tools/list only through the consolidated open_app tool; they are
	// NEVER individually direct (invalid-combination rejection replaces the
	// old silent behavior where a launcher spec's direct flag was ignored).
	// Launchers are always catalog-searchable so the app view's AttachTo can
	// resolve them and agents can still discover them.
	roleAppLauncher serverExtensionRole = "app-launcher"
)

// serverExtensionRoles is the declared role set of one extension.
type serverExtensionRoles []serverExtensionRole

// has reports whether the declared role set contains r.
func (roles serverExtensionRoles) has(r serverExtensionRole) bool {
	for _, declared := range roles {
		if declared == r {
			return true
		}
	}
	return false
}

// serverExtensionSpec declares one server extension and the explicit roles it
// plays. The zero value is invalid: an extension with no roles is rejected, as
// is any invalid role combination (see validate).
type serverExtensionSpec struct {
	// desc is the authoritative descriptor for the extension's tool.
	desc model.ToolDescriptor

	// roles declares every registration role the extension plays. A
	// descriptor may intentionally hold multiple roles (e.g. a transport
	// bridge is roleDirectTool + roleCatalogSearch).
	roles serverExtensionRoles

	// app, when non-nil, installs the extension's MCP App view after every
	// catalog entry is indexed. It attaches _meta.ui to the launcher entry in
	// the catalog and registers the ui:// resource + app-only helpers. The
	// catalog satisfies apps.AppCatalog (the view layer's narrower
	// interface). Only allowed on roleAppLauncher extensions.
	app func(srv *sdk.Server, catalog apps.AppCatalog) error
}

// validate rejects invalid role combinations that the former boolean matrix
// silently allowed (e.g. a "direct" launcher whose direct flag was ignored, or
// an app installer on a non-launcher extension).
func (s serverExtensionSpec) validate() error {
	if len(s.roles) == 0 {
		return fmt.Errorf("extension %q must declare at least one registration role", s.desc.Name)
	}
	seen := map[serverExtensionRole]bool{}
	for _, r := range s.roles {
		if seen[r] {
			return fmt.Errorf("extension %q declares role %q more than once", s.desc.Name, r)
		}
		seen[r] = true
	}
	if s.desc.Name == "" {
		return fmt.Errorf("server extension declaring roles %v must carry a named descriptor", s.roles)
	}
	if s.roles.has(roleAppLauncher) {
		if s.app == nil {
			return fmt.Errorf("app launcher extension %q must carry an app view installer", s.desc.Name)
		}
		if !s.roles.has(roleCatalogSearch) {
			return fmt.Errorf("app launcher extension %q must also be catalog-searchable (the app view resolves its launcher in the catalog)", s.desc.Name)
		}
		if s.roles.has(roleDirectTool) {
			return fmt.Errorf("app launcher extension %q must not also declare %q (the consolidated open_app tool is the single direct launcher)", s.desc.Name, roleDirectTool)
		}
	}
	// (No app-helper role: app-only helpers are registered exclusively inside
	// app view registration — see the package-level role vocabulary comment.)
	if s.app != nil && !s.roles.has(roleAppLauncher) {
		return fmt.Errorf("extension %q carries an app view installer but does not declare %q", s.desc.Name, roleAppLauncher)
	}
	return nil
}

// searchableOnly declares a catalog-searchable extension with no direct SDK
// projection (the OOB handoff tools, async upload management).
func searchableOnly(desc model.ToolDescriptor) serverExtensionSpec {
	return serverExtensionSpec{desc: desc, roles: serverExtensionRoles{roleCatalogSearch}}
}

// directOnly declares an extension with ONLY the explicit direct SDK
// projection: it is registered on tools/list, recorded on
// catalog.DirectCustom for card purposes, and never catalog-indexed (the
// no-app upload_file/vault_put_file mode).
func directOnly(desc model.ToolDescriptor) serverExtensionSpec {
	return serverExtensionSpec{desc: desc, roles: serverExtensionRoles{roleDirectTool}}
}

// directSearchable declares a dual-role extension: both the explicit direct
// SDK projection onto tools/list and catalog membership for progressive
// discovery (capabilities, agent_guide, the transport bridges in app mode).
func directSearchable(desc model.ToolDescriptor) serverExtensionSpec {
	return serverExtensionSpec{desc: desc, roles: serverExtensionRoles{roleDirectTool, roleCatalogSearch}}
}

// (No appOnlyHelper constructor: the app-only helper visibility domain has no
// extension-spec representation. Helpers are registered exclusively by their
// owning app views via apps.RegisterAppView — see the role vocabulary comment.)

// appLauncherSpec builds the spec for an open_* UI launcher. Per-app launchers
// are catalog-searchable (so the app view's AttachTo can resolve them and they
// stay discoverable via search_tools) and their app views are installed, but
// they are NEVER individually projected onto tools/list. Instead the single,
// consolidated open_app tool is the one direct-surfaced launcher for a
// GUI-capable host (see the open_app afterSurface hook in custom_tools.go); it
// resolves an app name to its ui:// resource. This means an agent never pays
// one tools/list schema slot per iframe screen. The descriptor must carry
// _meta.ui — the launcher build path enforces that guard directly in
// apps.NewOpenLauncherDescriptor (erroring on the marshal failure, e.g. an
// empty ResourceURI), which addLauncher routes through.
func appLauncherSpec(desc model.ToolDescriptor, app func(srv *sdk.Server, catalog apps.AppCatalog) error) serverExtensionSpec {
	return serverExtensionSpec{
		desc:  desc,
		roles: serverExtensionRoles{roleCatalogSearch, roleAppLauncher},
		app:   app,
	}
}

// serverExtensionRegistry runs the fixed multi-phase registration pipeline for
// the server-extension surface. It separates gathering (declare specs and plan
// hooks) from the ordered phases that actually register, so ordering
// dependencies are properties of the pipeline rather than of call order.
type serverExtensionRegistry struct {
	srv     *sdk.Server
	catalog *ToolCatalog
	specs   []*serverExtensionSpec

	// directProvisions run before the direct phase stamps and projects the
	// direct surface: provisions that append catalog entries whose direct
	// visibility rides the DirectVisible scan (wizard tools, dev tools).
	directProvisions []func(*ToolCatalog) error

	// postSurfaceHooks run after the direct tool surface is projected
	// (resources, prompts, the consolidated open_app launcher).
	postSurfaceHooks []func() error

	// materialized records that the projection half already ran. A plan
	// materializes exactly once: a second pass would double-project the
	// direct surface.
	materialized bool
}

// newServerExtensionRegistry creates a registry bound to the server and
// catalog.
func newServerExtensionRegistry(srv *sdk.Server, catalog *ToolCatalog) *serverExtensionRegistry {
	return &serverExtensionRegistry{srv: srv, catalog: catalog}
}

// add declares a spec for a server extension. Callers that need a spec to be
// conditional (e.g. only register upload_file when a byte source is wired)
// gate on the condition before calling add. Declared role combinations are
// validated when the plan runs (see run), not silently coerced.
func (r *serverExtensionRegistry) add(s serverExtensionSpec) *serverExtensionRegistry {
	spec := s
	r.specs = append(r.specs, &spec)
	return r
}

// beforeDirectTools provisions catalog entries that must exist before the pipeline
// stamps the direct surface (wizard.RegisterWizardTools, registerDevTools).
func (r *serverExtensionRegistry) beforeDirectTools(fn func(*ToolCatalog) error) *serverExtensionRegistry {
	r.directProvisions = append(r.directProvisions, fn)
	return r
}

// afterSurface queues a registration that must run once the direct tool
// surface is projected (resources, prompts, the consolidated open_app
// launcher).
func (r *serverExtensionRegistry) afterSurface(fn func() error) *serverExtensionRegistry {
	r.postSurfaceHooks = append(r.postSurfaceHooks, fn)
	return r
}

// complete executes the COLLECTION half of the fixed pipeline: the phases
// that must finish before any per-server projection is derived. It runs in the
// same relative order the former single run() used:
//
//  1. validate  — reject every invalid role combination before touching state
//  2. provision — wizard/dev catalog additions riding the direct projection
//  3. index     — add every catalog-searchable spec so app wiring can resolve
//     launchers
//
// When complete() returns, the plan is COMPLETE: the catalog holds the final
// indexed membership (compiled operations + every provision + every searchable
// extension), and the registry holds the explicit direct descriptors, app
// installers, and post-surface hooks. Everything that does not need the
// official server object finishes here, so initialization instructions and the
// per-server survey of the surface are computed from these completed facts and
// never from a partially-collected catalog.
func (r *serverExtensionRegistry) complete() error {
	for i, s := range r.specs {
		if err := s.validate(); err != nil {
			return fmt.Errorf("server extension spec %d: %w", i, err)
		}
	}

	// Registry-owned duplicate extension-name validation, run BEFORE any
	// catalog mutation: Catalog.Add silently replaces same-name entries and the
	// SDK server map silently replaces same-name registrations, so a duplicate
	// name must fail the plan here — before direct provisions run, before the
	// searchable specs are indexed, and long before materialization — instead
	// of overwriting a registration.
	if err := r.rejectDuplicateExtensionNames(); err != nil {
		return err
	}

	// Snapshot the pre-existing catalog entries (by pointer identity) before the
	// provision phase so the post-provision checks can detect (a) a provision
	// REPLACING an existing compiled/direct entry — ToolCatalog.Add silently
	// swaps the pointer, so identity is the reliable signal — and (b) two
	// provisions appending the same name.
	preByName := make(map[string]*model.ToolEntry, r.catalog.Len())
	for _, entry := range r.catalog.Entries() {
		preByName[entry.Name] = entry
	}

	// Every provision is checked per-step against the running entry map: a
	// provision whose appended entry collides with a PRE-EXISTING catalog
	// entry (a compiled operation, or an entry an earlier provision appended)
	// must fail the plan here. Left unguarded, the ToolCatalog map would
	// silently collapse the duplicate into a replacement and silently drop
	// whatever the previous entry carried. Pointer identity is the reliable
	// signal: Add swaps the map's entry pointer.
	for _, fn := range r.directProvisions {
		if err := fn(r.catalog); err != nil {
			return err
		}
		for _, entry := range r.catalog.Entries() {
			if prev, existed := preByName[entry.Name]; existed && prev != entry {
				return fmt.Errorf("duplicate extension name %q: a direct provision replaced an existing catalog entry (compiled op or earlier provision) — registration must fail, not silently overwrite", entry.Name)
			}
		}
		for _, entry := range r.catalog.Entries() {
			if _, existed := preByName[entry.Name]; !existed {
				preByName[entry.Name] = entry
			}
		}
	}

	// Re-check after the provision phase and before the indexing phase: a
	// provision that appended an entry colliding with a declared extension
	// name must fail the plan here, never silently replace.
	if err := r.rejectDuplicateExtensionNames(); err != nil {
		return err
	}

	for _, s := range r.specs {
		if s.roles.has(roleCatalogSearch) {
			r.catalog.Add(model.ToolEntryFromDescriptor(s.desc))
		}
	}
	return nil
}

// rejectDuplicateExtensionNames fails the plan when a declared extension name
// is duplicated — either between two declared specs or against an entry
// already in the catalog (a compiled operation, or a direct provision that
// already ran). Both downstream stores (ToolCatalog.Add, the SDK server map)
// silently replace same-name entries, so this registry-owned gate is the only
// place a duplicate can surface as a wiring error instead of a silent
// overwrite. It runs before any catalog mutation and again before indexing,
// and always before materialization.
func (r *serverExtensionRegistry) rejectDuplicateExtensionNames() error {
	declared := make(map[string]int, len(r.specs))
	for i, s := range r.specs {
		if prev, dup := declared[s.desc.Name]; dup {
			return fmt.Errorf("duplicate extension name %q: specs %d and %d both declare it", s.desc.Name, prev, i)
		}
		declared[s.desc.Name] = i
	}
	for _, s := range r.specs {
		if _, exists := r.catalog.Get(s.desc.Name); exists {
			return fmt.Errorf("duplicate extension name %q: the catalog already holds an entry with this name, so registering it would replace an existing tool", s.desc.Name)
		}
	}
	return nil
}

// materialize executes the PROJECTION half of the fixed pipeline: the one
// authoritative per-server pass that derives every server-facing projection
// from the completed plan. It requires the official server object (set via
// MaterializationPlan.Materialize) and must run exactly once per plan:
//
//  4. app       — install app views (requires all catalog entries present)
//  5. direct    — stamp the compiled direct surface and project it on
//     tools/list
//  6. surface   — project directly-registered extensions on tools/list,
//     de-duplicating against the direct projection (and later direct specs)
//     by name BEFORE registering, so an indexed direct extension stamped
//     DirectVisible by a flat stampDirectTools is never registered twice — the SDK
//     map would silently replace the first registration and hide the bug
//  7. post      — resources, prompts, the consolidated open_app launcher
//
// It finalizes the MaterializedTooling: the single authoritative record of the
// direct (tools/list) membership, meta-tool presence, and installed app views,
// which the server card and any downstream survey read instead of maintaining a
// parallel list.
func (r *serverExtensionRegistry) materialize() (*MaterializedTooling, error) {
	if r.materialized {
		return nil, fmt.Errorf("server extension plan was already materialized; a plan projects exactly once")
	}
	if r.srv == nil {
		return nil, fmt.Errorf("server extension plan must be materialized against an official server")
	}
	r.materialized = true

	var specDirect []DirectTool
	m := &MaterializedTooling{
		catalog:    r.catalog,
		metaOnWire: r.catalog.servesMetaTools(),
	}

	for _, s := range r.specs {
		if s.app != nil {
			if err := s.app(r.srv, r.catalog); err != nil {
				return nil, err
			}
			// Capture the app-view facts (launcher, bare screen name, and the
			// ui:// URI the registration just attached) into THIS server's
			// finalized surface, so open_app resolves only server-local apps.
			record := InstalledAppView{Launcher: s.desc.Name, Screen: strings.TrimPrefix(s.desc.Name, "open_")}
			if info, ok := apps.AppInfoForTool(s.desc.Name); ok {
				record.URI = info.URI
			}
			m.apps = append(m.apps, record)
		}
	}

	stampDirectTools(r.catalog)
	// The names the direct/flat DirectVisible projection is about to register:
	// the de-dup key for the extension direct pass below.
	alreadyDirect := map[string]bool{}
	for _, entry := range r.catalog.Entries() {
		if isDirectCatalogEntry(entry) {
			alreadyDirect[entry.Name] = true
		}
	}
	if err := RegisterOfficialDirectTools(r.srv, r.catalog); err != nil {
		return nil, err
	}

	for _, s := range r.specs {
		// roleAppLauncher extensions are never projected directly here — the
		// consolidated open_app tool is the single direct launcher (added in
		// the open_app afterSurface hook in custom_tools.go), and the role
		// validation already rejected any direct+launcher combination.
		if !s.roles.has(roleDirectTool) {
			continue
		}
		// De-duplicate against the direct projection (flat materialization
		// stamps every agent-safe indexed entry DirectVisible, which for a
		// direct+searchable extension would otherwise project the same tool
		// twice) and against an earlier spec with the same name — BEFORE
		// registering, never by relying on SDK map replacement.
		if alreadyDirect[s.desc.Name] {
			continue
		}
		if err := RegisterOfficialDescriptor(r.srv, s.desc); err != nil {
			return nil, err
		}
		alreadyDirect[s.desc.Name] = true
		specDirect = append(specDirect, DirectTool{Name: s.desc.Name, Description: s.desc.Description})
		// A directly-registered tool that is NOT catalog-searchable (e.g.
		// upload_file / vault_put_file in the no-app mode) is absent from the
		// catalog, so a legacy (non-finalized) flat card must consult this
		// explicit set to advertise exactly the direct surface that was
		// registered. Finalized surfaces read m.direct instead; this record is
		// kept as a compatibility side channel.
		if !s.roles.has(roleCatalogSearch) {
			r.catalog.DirectCustom = append(r.catalog.DirectCustom, s.desc.Name)
		}
	}
	// (No app-helper projection: this registry never registers app-only
	// helpers. They are registered inside their owning app views during the
	// app phase above, with visibility=["app"] wire metadata, and deliberately
	// never appear in m.direct.)

	for _, fn := range r.postSurfaceHooks {
		if err := fn(); err != nil {
			return nil, err
		}
	}

	m.finalize(specDirect)
	r.catalog.setFinalized(m)
	return m, nil
}

// run keeps the former combined pipeline for callers (tests, the legacy
// single-pass registerCustomTools) that hold a registry bound to an existing
// server: it is complete() followed by the single materialization pass.
func (r *serverExtensionRegistry) run() error {
	if err := r.complete(); err != nil {
		return err
	}
	_, err := (&MaterializationPlan{reg: r}).Materialize(r.srv)
	return err
}

// addLauncher builds the open_* launcher descriptor for spec (via
// apps.NewOpenLauncherDescriptor) and records its serverExtensionSpec. The
// descriptor build error (the _meta.ui marshal failure, e.g. an empty
// ResourceURI) is a wiring bug at this assembly seam — a launcher without its
// marshaled app meta would register as a plain tool whose app view silently
// fails to render — so it fails the assembly hard instead of being swallowed.
func (r *serverExtensionRegistry) addLauncher(spec apps.OpenLauncherSpec, app func(srv *sdk.Server, catalog apps.AppCatalog) error) error {
	desc, err := apps.NewOpenLauncherDescriptor(spec)
	if err != nil {
		return err
	}
	r.add(appLauncherSpec(desc, app))
	return nil
}
