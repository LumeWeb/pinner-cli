package catalog

import (
	"context"
	"encoding/json"
	"testing"
)

// buildMCPSample returns a catalog spanning the visibility and safety spectrum:
// an agent-safe Read, a Destructive op, an AppOnly helper, and a HumanOnly op.
// The Read op carries MCPTargets to exercise audience selection.
func buildMCPSample(t *testing.T) Catalog {
	t.Helper()
	c := NewCatalog()

	ok := func(op Operation) {
		if err := c.Add(op); err != nil {
			t.Fatalf("Add(%q): %v", op.Name(), err)
		}
	}

	ok(NewOperation(OperationSpec{
		Name:        "vault.get",
		Title:       "Get Vault",
		Summary:     "get a vault",
		Description: "long get description",
		MCPTargets:  MCPTargets(Fallback("agent-aware get description")),
		Category:    "vault",
		Safety:           SafetyRead,
		Interaction:      InteractionAgentSafe,
		Visibility:       VisibilityModel,
		Args: []OperationArg{
			{Name: "name", Type: ArgTypeString, Required: true, Help: "vault name"},
		},
		Handler: markerHandler{marker: "ran:vault.get"},
	}))

	ok(NewOperation(OperationSpec{
		Name:        "vault.delete",
		Title:       "Delete Vault",
		Summary:     "delete a vault",
		Description: "long delete description",
		Category:    "vault",
		Safety:      SafetyDestructive,
		Interaction: InteractionAgentSafe,
		Visibility:  VisibilityModel,
		Args: []OperationArg{
			{Name: "name", Type: ArgTypeString, Required: true, Help: "vault name"},
		},
		Handler: markerHandler{marker: "ran:vault.delete"},
	}))

	// App-only helper: a model agent must never discover or invoke this.
	ok(NewOperation(OperationSpec{
		Name:        "vault.reindex",
		Title:       "Reindex Vault",
		Summary:     "rebuild the vault index",
		Description: "app-only reindex helper",
		Category:    "vault",
		Safety:      SafetyMutate,
		Interaction: InteractionAgentSafe,
		Visibility:  VisibilityAppOnly,
		Handler:     markerHandler{marker: "ran:vault.reindex"},
	}))

	// Human-only, but both-visible so a model may see it discoverable (it is
	// still refused at Invoke for non-human actors).
	ok(NewOperation(OperationSpec{
		Name:        "account.login",
		Title:       "Login",
		Summary:     "log in interactively",
		Description: "long login description",
		Category:    "auth",
		Safety:      SafetyMutate,
		Interaction: InteractionHumanOnly,
		Visibility:  VisibilityBoth,
		Handler:     markerHandler{marker: "ran:account.login"},
	}))

	return c
}

// compileSurface compiles the sample catalog with the given compiler and returns
// the resulting []ToolDescriptor.
func compileSurface(t *testing.T, comp Compiler[ToolDescriptor]) []ToolDescriptor {
	t.Helper()
	tools, err := comp.Compile(buildMCPSample(t))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	return tools
}

// toolNamed returns the descriptor named name from tools.
func toolNamed(t *testing.T, tools []ToolDescriptor, name string) ToolDescriptor {
	t.Helper()
	for _, td := range tools {
		if td.Name == name {
			return td
		}
	}
	t.Fatalf("tool %q not found in compiled output", name)
	return ToolDescriptor{}
}

// TestMCPCompilerModelSurface compiles the model surface and asserts it contains
// the agent-safe, destructive, and human-only ops — but NOT the app-only helper.
func TestMCPCompilerModelSurface(t *testing.T) {
	tools := compileSurface(t, NewMCPCompiler())

	got := map[string]bool{}
	for _, td := range tools {
		got[td.Name] = true
	}
	if !got["vault.get"] {
		t.Error("model surface missing agent-safe read op vault.get")
	}
	if !got["vault.delete"] {
		t.Error("model surface missing destructive op vault.delete")
	}
	if !got["account.login"] {
		t.Error("model surface missing human-only op account.login")
	}
	// The app-only helper must stay out of agent discovery.
	if got["vault.reindex"] {
		t.Error("model surface must not include app-only op vault.reindex")
	}
}

// TestMCPCompilerAppSurface compiles the app surface and asserts it includes the
// app-only helper (and correctly excludes model-only ops that the app does not
// target).
func TestMCPCompilerAppSurface(t *testing.T) {
	tools := compileSurface(t, NewMCPAppCompiler())

	got := map[string]bool{}
	for _, td := range tools {
		got[td.Name] = true
	}
	if !got["vault.reindex"] {
		t.Error("app surface missing app-only helper vault.reindex")
	}
	if !got["account.login"] {
		t.Error("app surface missing both-visible op account.login")
	}
	if got["vault.get"] {
		t.Error("app surface must not include model-only op vault.get")
	}
}

// TestMCPCompilerDescriptionAudience asserts the description selection: the model
// surface resolves the description from the MCPTargets fallback, the app surface
// uses Description.
func TestMCPCompilerDescriptionAudience(t *testing.T) {
	model := compileSurface(t, NewMCPCompiler())
	if got := toolNamed(t, model, "vault.get").Description; got != "agent-aware get description" {
		t.Errorf("model surface vault.get Description = %q, want MCPTargets fallback description", got)
	}
	// An op without MCPTargets falls back to Description on the model surface.
	if got := toolNamed(t, model, "vault.delete").Description; got != "long delete description" {
		t.Errorf("model surface vault.delete Description = %q, want Description fallback", got)
	}

	app := compileSurface(t, NewMCPAppCompiler())
	if got := toolNamed(t, app, "vault.reindex").Description; got != "app-only reindex helper" {
		t.Errorf("app surface vault.reindex Description = %q, want Description", got)
	}
}

// TestMCPCompilerInputSchema asserts every descriptor carries a populated JSON
// Schema with typed properties and the required list matching Args().
func TestMCPCompilerInputSchema(t *testing.T) {
	tools := compileSurface(t, NewMCPCompiler())
	td := toolNamed(t, tools, "vault.get")
	if len(td.InputSchema) == 0 {
		t.Fatal("vault.get InputSchema is empty")
	}

	var sch map[string]any
	if err := json.Unmarshal(td.InputSchema, &sch); err != nil {
		t.Fatalf("InputSchema is not valid JSON: %v", err)
	}
	if sch["type"] != "object" {
		t.Errorf("schema type = %v, want object", sch["type"])
	}
	props, ok := sch["properties"].(map[string]any)
	if !ok {
		t.Fatalf("schema properties missing: %#v", sch["properties"])
	}
	p, ok := props["name"].(map[string]any)
	if !ok {
		t.Fatalf("schema property name missing: %#v", props)
	}
	if p["type"] != "string" {
		t.Errorf("name property type = %v, want string", p["type"])
	}
	req, ok := sch["required"].([]any)
	if !ok || len(req) != 1 || req[0] != "name" {
		t.Errorf("schema required = %#v, want [name]", sch["required"])
	}

	// The destructive op also carries an InputSchema (required name arg).
	del := toolNamed(t, tools, "vault.delete")
	if len(del.InputSchema) == 0 {
		t.Error("vault.delete InputSchema is empty")
	}
}

// TestMCPCompilerPositionalOnlyStillInSchema asserts that a PositionalOnly arg
// — which suppresses the arg's CLI --flag only — still appears in the MCP
// JSON-Schema properties/required. MCP agents identify the zone by name, so the
// suppression must never leak from the CLI surface onto the MCP surface.
func TestMCPCompilerPositionalOnlyStillInSchema(t *testing.T) {
	c := NewCatalog()
	if err := c.Add(NewOperation(OperationSpec{
		Name: "dns.records.create", Title: "Create", Summary: "create a record",
		Description: "create a dns record", Category: "dns", Safety: SafetyMutate,
		Interaction: InteractionAgentSafe, Visibility: VisibilityBoth,
		Positional: "<domain>",
		Args: []OperationArg{
			{Name: "zone", Type: ArgTypeString, Required: true, PositionalOnly: true, Help: "Domain name or numeric zone ID"},
			{Name: "type", Type: ArgTypeString, Required: true, Help: "Record type"},
		},
		Handler: &captureHandler{},
	})); err != nil {
		t.Fatalf("Add: %v", err)
	}

	raw, err := NewMCPCompiler().Compile(c)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if len(raw) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(raw))
	}
	td := raw[0]

	var sch map[string]any
	if err := json.Unmarshal(td.InputSchema, &sch); err != nil {
		t.Fatalf("InputSchema not valid JSON: %v", err)
	}
	props, ok := sch["properties"].(map[string]any)
	if !ok {
		t.Fatalf("schema properties missing: %#v", sch["properties"])
	}
	if _, ok := props["zone"]; !ok {
		t.Errorf("PositionalOnly arg 'zone' must still appear in MCP schema properties, got %v", props)
	}
	req, ok := sch["required"].([]any)
	if !ok {
		t.Fatalf("schema required missing")
	}
	found := false
	for _, r := range req {
		if r == "zone" {
			found = true
		}
	}
	if !found {
		t.Errorf("PositionalOnly arg 'zone' must still be required in MCP schema, got %v", req)
	}
}

// TestMCPCompilerMetadataCarriesDeclaredProps asserts the descriptor carries the
// declared Safety, Interaction, Visibility, and Category metadata — and that it
// exposes NO executable handler, so dispatch cannot bypass the owning catalog's
// gates (destructive-confirm, human-only, app-only) which live in Catalog.Invoke.
func TestMCPCompilerMetadataCarriesDeclaredProps(t *testing.T) {
	model := compileSurface(t, NewMCPCompiler())

	del := toolNamed(t, model, "vault.delete")
	if del.Safety != SafetyDestructive {
		t.Errorf("vault.delete Safety = %v, want SafetyDestructive", del.Safety)
	}
	if del.Visibility != VisibilityModel {
		t.Errorf("vault.delete Visibility = %v, want VisibilityModel", del.Visibility)
	}
	if del.Category != "vault" {
		t.Errorf("vault.delete Category = %q, want vault", del.Category)
	}

	get := toolNamed(t, model, "vault.get")
	if get.Safety != SafetyRead || get.Interaction != InteractionAgentSafe {
		t.Errorf("vault.get Safety/Interaction = %v/%v", get.Safety, get.Interaction)
	}

	app := compileSurface(t, NewMCPAppCompiler())
	re := toolNamed(t, app, "vault.reindex")
	if re.Visibility != VisibilityAppOnly {
		t.Errorf("vault.reindex Visibility = %v, want VisibilityAppOnly", re.Visibility)
	}
}

// TestMCPCompilerDispatchStillEnforcesGates asserts that even though the
// descriptor is metadata-only, the owning catalog still refuses an unsafe
// dispatch: a model actor must not run a HumanOnly op, and an app-only op must
// not be invocable by a model. This is the behavioral guarantee behind removing
// the Handler from ToolDescriptor — all dispatch funnels through Catalog.Invoke.
func TestMCPCompilerDispatchStillEnforcesGates(t *testing.T) {
	c := buildMCPSample(t)
	// account.login is HumanOnly: a model actor must be refused at Invoke.
	if _, err := c.Invoke(context.Background(), "account.login", map[string]any{"cid": "x"}, ActorModel); err == nil {
		t.Error("HumanOnly op invoke by model must be refused (gate still enforced at Invoke)")
	}
	// vault.reindex is AppOnly: a model actor must be refused at Invoke.
	if _, err := c.Invoke(context.Background(), "vault.reindex", map[string]any{"cid": "x"}, ActorModel); err == nil {
		t.Error("AppOnly op invoke by model must be refused (gate still enforced at Invoke)")
	}
}

// TestInputSchemaRequiredExcludesDefaultedArg pins the round-8 fix: an arg that
// is Required but declares a Default is satisfied by the default (per both the
// CLI flagFor and Invoke), so it must NOT appear in the schema's "required"
// array — otherwise a model is told to supply a value that dispatch treats as
// optional.
func TestInputSchemaRequiredExcludesDefaultedArg(t *testing.T) {
	c := NewCatalog()
	if err := c.Add(NewOperation(OperationSpec{
		Name: "job.schedule", Title: "Schedule", Summary: "schedule a job",
		Category: "job", Safety: SafetyMutate,
		Interaction: InteractionAgentSafe, Visibility: VisibilityModel,
		Args: []OperationArg{
			{Name: "name", Type: ArgTypeString, Required: true},
			{Name: "ttl", Type: ArgTypeInt, Required: true, Default: "3600"},
			{Name: "grace", Type: ArgTypeDuration, Default: "30s"},
		},
		Handler: &captureHandler{},
	})); err != nil {
		t.Fatalf("Add: %v", err)
	}
	raw, err := NewMCPCompiler().Compile(c)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	td := toolNamed(t, raw, "job.schedule")
	var sch map[string]any
	if err := json.Unmarshal(td.InputSchema, &sch); err != nil {
		t.Fatalf("InputSchema not valid JSON: %v", err)
	}
	req, ok := sch["required"].([]any)
	if !ok {
		t.Fatalf("schema required missing or not a list: %#v", sch["required"])
	}
	if len(req) != 1 || req[0] != "name" {
		t.Fatalf("schema required = %#v, want [name] only (ttl has a default, grace is optional)", req)
	}
}

// TestInputSchemaMarksSensitiveArg pins the round-12 fix: an OperationArg with
// Sensitive set is carried through the shared JSON Schema via SensitiveSchemaKey,
// so the MCP/adapter layer can redact the value from logs and echoed tool calls —
// it must not be indistinguishable from a normal arg on the model surface.
func TestInputSchemaMarksSensitiveArg(t *testing.T) {
	c := NewCatalog()
	if err := c.Add(NewOperation(OperationSpec{
		Name: "auth.token", Title: "Token", Summary: "issue a token",
		Category: "auth", Safety: SafetyMutate,
		Interaction: InteractionAgentSafe, Visibility: VisibilityModel,
		Args: []OperationArg{
			{Name: "label", Type: ArgTypeString},
			{Name: "secret", Type: ArgTypeString, Sensitive: true},
		},
		Handler: &captureHandler{},
	})); err != nil {
		t.Fatalf("Add: %v", err)
	}
	raw, err := NewMCPCompiler().Compile(c)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	td := toolNamed(t, raw, "auth.token")
	var sch map[string]any
	if err := json.Unmarshal(td.InputSchema, &sch); err != nil {
		t.Fatalf("InputSchema not valid JSON: %v", err)
	}
	props, ok := sch["properties"].(map[string]any)
	if !ok {
		t.Fatalf("schema properties missing: %#v", sch["properties"])
	}
	secret, ok := props["secret"].(map[string]any)
	if !ok {
		t.Fatalf("secret property missing: %#v", props)
	}
	if secret[SensitiveSchemaKey] != true {
		t.Errorf("secret property not marked sensitive: %#v", secret)
	}
	label, ok := props["label"].(map[string]any)
	if !ok {
		t.Fatalf("label property missing: %#v", props)
	}
	if _, marked := label[SensitiveSchemaKey]; marked {
		t.Errorf("label property should not be marked sensitive: %#v", label)
	}
}

// TestAgentRequiredMcpOnly pins the v3 fix: an arg marked AgentRequired appears
// in the MCP JSON-Schema "required" array AND is enforced by ValidateMCPRequired
// (called from the MCP dispatch layer), but is NEVER enforced by the shared
// NormalizeOperationInput and is NEVER a required urfave CLI flag — so a
// positionally-supplied CLI value keeps working. This is what lets pins_add.cids
// be required of an agent without regressing `pins add <cid>...` or leaking
// MCP-only validation into non-MCP callers.
func TestAgentRequiredMcpOnly(t *testing.T) {
	c := NewCatalog()
	if err := c.Add(NewOperation(OperationSpec{
		Name: "job.submit", Title: "Submit", Summary: "submit a job",
		Category: "job", Safety: SafetyMutate,
		Interaction: InteractionAgentSafe, Visibility: VisibilityModel,
		// cids is a StringSlice accepted positionally on the CLI, but the MCP
		// surface requires it.
		Args: []OperationArg{
			{Name: "cids", Type: ArgTypeStringSlice, AgentRequired: true},
		},
		Handler: &captureHandler{},
	})); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// MCP schema lists cids in required.
	raw, err := NewMCPCompiler().Compile(c)
	if err != nil {
		t.Fatalf("Compile MCP: %v", err)
	}
	td := toolNamed(t, raw, "job.submit")
	var sch map[string]any
	if err := json.Unmarshal(td.InputSchema, &sch); err != nil {
		t.Fatalf("InputSchema not valid JSON: %v", err)
	}
	req, ok := sch["required"].([]any)
	if !ok {
		t.Fatalf("schema required missing or not a list: %#v", sch["required"])
	}
	found := false
	for _, r := range req {
		if r == "cids" {
			found = true
		}
	}
	if !found {
		t.Fatalf("AgentRequired arg cids missing from MCP schema required: %#v", req)
	}

	// AgentRequired must be enforced by the dedicated MCP dispatch check, NOT by
	// the shared NormalizeOperationInput (which the CLI and other non-MCP callers
	// route through). This is the contract that keeps AgentRequired MCP-only: a
	// shared invocation must not inherit MCP-specific validation.
	op, ok := c.Get("job.submit")
	if !ok {
		t.Fatal("Get job.submit")
	}
	if _, err := NormalizeOperationInput(op, map[string]any{}); err != nil {
		t.Errorf("shared NormalizeOperationInput must NOT enforce AgentRequired (MCP-only), got: %v", err)
	}
	if err := ValidateMCPRequired(op, map[string]any{}); err == nil {
		t.Error("ValidateMCPRequired must reject a missing AgentRequired arg")
	}
	if err := ValidateMCPRequired(op, map[string]any{"cids": []string{"bafy"}}); err != nil {
		t.Errorf("ValidateMCPRequired with cids supplied should pass, got: %v", err)
	}

	// CLI: cids must NOT be a required urfave flag (positional-friendly).
	cliRaw, err := NewCLICompiler().Compile(c)
	if err != nil {
		t.Fatalf("Compile CLI: %v", err)
	}
	for _, cmd := range cliRaw {
		for _, f := range cmd.Flags {
			if f.Names()[0] == "cids" {
				if r, ok := f.(requireder); ok && r.IsRequired() {
					t.Error("AgentRequired arg cids must NOT be a required CLI flag")
				}
			}
		}
	}
}

// TestInputSchemaArraysHaveItems pins the JSON Schema requirement that every
// array-typed property declares its element schema (items). Hosting surfaces
// (AWS Bedrock and several MCP validators) reject tools whose input_schema has
// an array without items, so a string-slice arg must always render as
// {"type":"array","items":{"type":"string"}}.
func TestInputSchemaArraysHaveItems(t *testing.T) {
	c := NewCatalog()
	if err := c.Add(NewOperation(OperationSpec{
		Name: "pins.rm", Title: "Remove pins", Summary: "remove pins",
		Category: "core", Safety: SafetyDestructive,
		Interaction: InteractionAgentSafe, Visibility: VisibilityModel,
		Args: []OperationArg{
			{Name: "confirm", Type: ArgTypeBool, Required: true},
			{Name: "cids", Type: ArgTypeStringSlice, SelectionGroup: "cids_or_all"},
			{Name: "all", Type: ArgTypeBool, SelectionGroup: "cids_or_all"},
			{Name: "meta", Type: ArgTypeStringSlice},
		},
		Handler: &captureHandler{},
	})); err != nil {
		t.Fatalf("Add: %v", err)
	}
	raw, err := NewMCPCompiler().Compile(c)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	td := toolNamed(t, raw, "pins.rm")
	var sch map[string]any
	if err := json.Unmarshal(td.InputSchema, &sch); err != nil {
		t.Fatalf("InputSchema not valid JSON: %v", err)
	}
	if bad := assertArraysHaveItems(sch, ""); len(bad) > 0 {
		t.Fatalf("array-typed properties missing items: %v (schema %#v)", bad, sch)
	}
	// The cids/meta array properties must carry a string items schema.
	for _, prop := range []string{"cids", "meta"} {
		ps, ok := sch["properties"].(map[string]any)[prop].(map[string]any)
		if !ok {
			t.Fatalf("property %s missing from schema", prop)
		}
		if ps["type"] != "array" {
			t.Fatalf("property %s expected type array, got %#v", prop, ps["type"])
		}
		items, ok := ps["items"].(map[string]any)
		if !ok || items["type"] != "string" {
			t.Fatalf("property %s must emit items:{\"type\":\"string\"}, got %#v", prop, ps["items"])
		}
	}
}

// assertArraysHaveItems recursively walks a JSON Schema and returns the dotted
// paths of every type:array node that lacks an items schema. It descends into
// properties and items so nested schemas are covered as well.
func assertArraysHaveItems(node any, path string) []string {
	switch v := node.(type) {
	case map[string]any:
		if v["type"] == "array" {
			if _, ok := v["items"]; !ok {
				key := "schema"
				if path != "" {
					key = path
				}
				return []string{key}
			}
		}
		var out []string
		if props, ok := v["properties"].(map[string]any); ok {
			for k, p := range props {
				pth := k
				if path != "" {
					pth = path + "." + k
				}
				out = append(out, assertArraysHaveItems(p, pth)...)
			}
		}
		if it, ok := v["items"]; ok {
			out = append(out, assertArraysHaveItems(it, path+".items")...)
		}
		return out
	case []any:
		var out []string
		for _, e := range v {
			out = append(out, assertArraysHaveItems(e, path)...)
		}
		return out
	default:
		return nil
	}
}

// TestInputSchemaNoRootCombinator pins the compatibility policy: a generated
// MCP tool input schema must never carry oneOf/anyOf/allOf at the root. Several
// hosts (AWS Bedrock, Copilot, and other model gateways) implement only a
// subset of JSON Schema and reject combinators at the top level of
// input_schema. Exactly-one-of SelectionGroup members are enforced at dispatch
// by enforceSelectionGroups, not advertised as a root-level schema oneOf.
func TestInputSchemaNoRootCombinator(t *testing.T) {
	c := NewCatalog()
	if err := c.Add(NewOperation(OperationSpec{
		Name: "pick.one", Title: "Pick one", Summary: "pick one",
		Category: "core", Safety: SafetyMutate,
		Interaction: InteractionAgentSafe, Visibility: VisibilityModel,
		Args: []OperationArg{
			{Name: "a", Type: ArgTypeString, SelectionGroup: "choice"},
			{Name: "b", Type: ArgTypeBool, SelectionGroup: "choice"},
			{Name: "c", Type: ArgTypeStringSlice, SelectionGroup: "choice"},
		},
		Handler: &captureHandler{},
	})); err != nil {
		t.Fatalf("Add: %v", err)
	}
	raw, err := NewMCPCompiler().Compile(c)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	td := toolNamed(t, raw, "pick.one")
	var sch map[string]any
	if err := json.Unmarshal(td.InputSchema, &sch); err != nil {
		t.Fatalf("InputSchema not valid JSON: %v", err)
	}
	// The SelectionGroup schema must still emit items for its slice member.
	if bad := assertArraysHaveItems(sch, ""); len(bad) > 0 {
		t.Fatalf("array-typed properties missing items: %v (schema %#v)", bad, sch)
	}
	// And it must not advertise the group as a root-level combinator.
	for _, k := range []string{"oneOf", "anyOf", "allOf"} {
		if _, ok := sch[k]; ok {
			t.Fatalf("root-level %q must not appear (incompatible with strict hosts); got %#v", k, sch[k])
		}
	}
}

// TestMCPCompilerAgentOnlyStillInSchema asserts that an AgentOnly arg — which
// suppresses the arg's CLI --flag only — still appears in the MCP JSON-Schema.
// The agent/MCP surface is exactly where an agent-only control must be exposed;
// the CLI suppression must never leak onto the MCP surface.
func TestMCPCompilerAgentOnlyStillInSchema(t *testing.T) {
	c := NewCatalog()
	if err := c.Add(NewOperation(OperationSpec{
		Name: "websites.create", Title: "Create", Summary: "create a site",
		Description: "create a website", Category: "websites", Safety: SafetyMutate,
		Interaction: InteractionAgentSafe, Visibility: VisibilityBoth,
		Positional: "<domain>",
		Args: []OperationArg{
			{Name: "website", Type: ArgTypeString, Help: "Custom domain"},
			{Name: "cid", Type: ArgTypeString, Required: true, Help: "IPFS CID"},
			{Name: "platform", Type: ArgTypeBool, AgentOnly: true, Help: "Claim a platform subdomain", AgentHelp: "Set to claim a platform subdomain at create"},
			{Name: "label", Type: ArgTypeString, AgentOnly: true, Help: "Subdomain label", AgentHelp: "Subdomain label to claim"},
		},
		Handler: &captureHandler{},
	})); err != nil {
		t.Fatalf("Add: %v", err)
	}

	raw, err := NewMCPCompiler().Compile(c)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if len(raw) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(raw))
	}
	var sch map[string]any
	if err := json.Unmarshal(raw[0].InputSchema, &sch); err != nil {
		t.Fatalf("InputSchema not valid JSON: %v", err)
	}
	props, ok := sch["properties"].(map[string]any)
	if !ok {
		t.Fatalf("schema properties missing: %#v", sch["properties"])
	}
	for _, name := range []string{"platform", "label"} {
		if _, ok := props[name]; !ok {
			t.Errorf("AgentOnly arg %q must still appear in MCP schema properties, got %#v", name, props)
		}
	}
}

func contains(hay []string, needle string) bool {
	for _, h := range hay {
		if h == needle {
			return true
		}
	}
	return false
}

func toStrings(v any) []string {
	switch t := v.(type) {
	case []string:
		return t
	case []any:
		out := make([]string, len(t))
		for i, e := range t {
			out[i], _ = e.(string)
		}
		return out
	}
	return nil
}
