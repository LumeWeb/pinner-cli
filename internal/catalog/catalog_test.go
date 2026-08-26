package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

// markerHandler returns a fixed marker value from Execute, letting tests assert
// that Invoke actually reached the Handler.
type markerHandler struct {
	marker string
}

func (h markerHandler) Execute(_ context.Context, _ map[string]any) (any, error) {
	return h.marker, nil
}

// stub builds a named operation with the given properties and a marker handler.
func stub(name, title, summary, desc, agentDesc, category string, safety Safety, inter Interaction, vis Visibility, args ...OperationArg) Operation {
	if len(args) == 0 {
		args = []OperationArg{{Name: "cid", Type: ArgTypeString, Required: true}}
	}
	return NewOperation(OperationSpec{
		Name:             name,
		Title:            title,
		Summary:          summary,
		Description:      desc,
		MCPTargets:       MCPTargets(Fallback(agentDesc)),
		Args:             args,
		Safety:           safety,
		Interaction:      inter,
		Visibility:       vis,
		Category:         category,
		Handler:          markerHandler{marker: "ran:" + name},
	})
}

// newTestCatalog returns a catalog pre-populated with a spread of operations
// covering the interaction/visibility/safety matrix plus query/category variety.
func newTestCatalog(t *testing.T) Catalog {
	t.Helper()
	c := NewCatalog()
	ops := []Operation{
		// agent-safe, read, model-visible — the everyday automatable case.
		stub("pin add", "Add Pin", "pin a cid", "Long pin add desc", "Agent: add a pin",
			"pinning", SafetyRead, InteractionAgentSafe, VisibilityModel),
		// destructive but agent-safe — visible to both surfaces: a model can run
		// it (needs confirm) and a human/app can run it (confirms in-app).
		// Model-only would wrongly bar the app/human face of a destructive op.
		stub("pin remove", "Remove Pin", "remove a pin", "Long pin remove desc", "Agent: remove a pin",
			"pinning", SafetyDestructive, InteractionAgentSafe, VisibilityBoth),
		// human-only interactive op — refused for any non-human actor.
		stub("account login", "Login", "interactive login flow", "Long login desc", "",
			"auth", SafetyMutate, InteractionHumanOnly, VisibilityBoth),
		// app-only helper — invisible/refused to models.
		stub("internal resync", "Resync", "app internal resync helper", "Long resync desc", "",
			"internal", SafetyMutate, InteractionAgentSafe, VisibilityAppOnly),
		// both-visible, different category for category-filter test.
		stub("websites list", "List Websites", "list websites", "Long websites desc", "Agent: list sites",
			"websites", SafetyRead, InteractionAgentSafe, VisibilityBoth),
	}
	for _, o := range ops {
		if err := c.Add(o); err != nil {
			t.Fatalf("Add(%q): %v", o.Name(), err)
		}
	}
	return c
}

func TestAddGetRoundTrip(t *testing.T) {
	c := newTestCatalog(t)
	op, ok := c.Get("pin add")
	if !ok {
		t.Fatal("Get(pin add) not found")
	}
	if op.Name() != "pin add" || op.Title() != "Add Pin" || op.Safety() != SafetyRead {
		t.Fatalf("Get returned wrong op: %+v", op)
	}
	if _, ok := c.Get("does not exist"); ok {
		t.Fatal("Get for unknown name unexpectedly found an op")
	}
}

func TestAddRejectsDuplicate(t *testing.T) {
	c := NewCatalog()
	a := stub("dup", "", "", "", "", "", SafetyRead, InteractionAgentSafe, VisibilityModel)
	b := stub("dup", "", "", "", "", "", SafetyRead, InteractionAgentSafe, VisibilityModel)
	if err := c.Add(a); err != nil {
		t.Fatalf("first Add: %v", err)
	}
	if err := c.Add(b); err == nil {
		t.Fatal("second Add with same name should error")
	}
}

// TestAddRejectsAgentOnlyRequiredNoDefault guards that an AgentOnly arg marked
// Required without a Default is rejected at registration. AgentOnly args are
// never emitted as a CLI --flag, so such an arg could never be supplied and
// would make the CLI command permanently fail "missing required argument".
func TestAddRejectsAgentOnlyRequiredNoDefault(t *testing.T) {
	c := NewCatalog()
	err := c.Add(stub("w created", "Create", "create", "desc", "", "websites",
		SafetyMutate, InteractionAgentSafe, VisibilityBoth,
		OperationArg{Name: "platform", Type: ArgTypeBool, AgentOnly: true, Required: true}))
	if err == nil {
		t.Fatal("Add(AgentOnly && Required && no Default) should error")
	}
	if !strings.Contains(err.Error(), "AgentOnly") {
		t.Fatalf("error should explain the AgentOnly constraint, got: %v", err)
	}
	// AgentRequired (MCP-only requiredness) is a valid combo for an AgentOnly arg.
	if err := c.Add(stub("w created", "Create", "create", "desc", "", "websites",
		SafetyMutate, InteractionAgentSafe, VisibilityBoth,
		OperationArg{Name: "platform", Type: ArgTypeBool, AgentOnly: true, AgentRequired: true})); err != nil {
		t.Fatalf("AgentOnly + AgentRequired must be valid, got: %v", err)
	}
}

func TestAddRejectsNilAndEmptyName(t *testing.T) {
	c := NewCatalog()
	if err := c.Add(nil); err == nil {
		t.Fatal("Add(nil) should error")
	}
	if err := c.Add(stub("", "", "", "", "", "", SafetyRead, InteractionAgentSafe, VisibilityModel)); err == nil {
		t.Fatal("Add(empty name) should error")
	}
}

func TestSearchQuery(t *testing.T) {
	c := newTestCatalog(t)
	// Substring on name, case-insensitive.
	got := c.Search("add", "", VisibilityModel)
	if len(got) != 1 || got[0].Name() != "pin add" {
		t.Fatalf("Search(add) = %v, want just [pin add]", names(got))
	}
	// Substring on summary/description/title.
	if got := c.Search("login", "", VisibilityModel); len(got) != 1 || got[0].Name() != "account login" {
		t.Fatalf("Search(login) = %v", names(got))
	}
	// No match.
	if got := c.Search("zzzz", "", VisibilityModel); len(got) != 0 {
		t.Fatalf("Search(zzzz) = %v, want empty", names(got))
	}
	// Empty query with model visibility excludes the app-only op.
	if got := c.Search("", "", VisibilityModel); len(got) != 4 {
		t.Fatalf("Search(all, model) = %v, want 4 ops (app-only excluded)", names(got))
	}
}

func TestSearchCategory(t *testing.T) {
	c := newTestCatalog(t)
	got := c.Search("", "pinning", VisibilityModel)
	if len(got) != 2 {
		t.Fatalf("Search(pinning) = %v, want 2", names(got))
	}
	// Category match is case-insensitive.
	if got := c.Search("", "AUTH", VisibilityModel); len(got) != 1 || got[0].Name() != "account login" {
		t.Fatalf("Search(AUTH) = %v", names(got))
	}
	// Unknown category.
	if got := c.Search("", "nope", VisibilityModel); len(got) != 0 {
		t.Fatalf("Search(nope) = %v, want empty", names(got))
	}
}

func TestSearchVisibility(t *testing.T) {
	c := newTestCatalog(t)
	// Model search sees model + both, not app-only.
	model := c.Search("", "", VisibilityModel)
	if containsOp(model, "internal resync") {
		t.Fatalf("model search must not include app-only op: %v", names(model))
	}
	// App search sees app-only + both.
	app := c.Search("", "", VisibilityAppOnly)
	if !containsOp(app, "internal resync") {
		t.Fatalf("app search should include app-only op: %v", names(app))
	}
	// Both-visible op appears in both.
	if !containsOp(model, "websites list") || !containsOp(app, "websites list") {
		t.Fatalf("both-visible op should appear in model and app searches")
	}
	// Unrestricted search sees everything.
	if all := c.Search("", "", VisibilityBoth); len(all) != 5 {
		t.Fatalf("unrestricted search = %v, want 5", names(all))
	}
}

// TestSearchDeterministicOrder pins the round-26 fix: Search iterates the ops
// map (nondeterministic in Go), so it must sort by Name before returning —
// otherwise CLI help and MCP tool lists would vary between runs.
func TestSearchDeterministicOrder(t *testing.T) {
	c := newTestCatalog(t)
	// Run several times and assert the result is always in sorted-by-name order.
	for i := 0; i < 20; i++ {
		got := c.Search("", "", VisibilityBoth)
		for j := 1; j < len(got); j++ {
			if got[j-1].Name() > got[j].Name() {
				t.Fatalf("Search results not sorted by name: %v", names(got))
			}
		}
	}
	// The exact order is stable and sorted: account login, internal resync,
	// pin add, pin remove, websites list.
	expect := []string{"account login", "internal resync", "pin add", "pin remove", "websites list"}
	if gotStr := names(c.Search("", "", VisibilityBoth)); !reflect.DeepEqual(gotStr, expect) {
		t.Fatalf("Search order = %v, want %v", gotStr, expect)
	}
}

func TestDescribe(t *testing.T) {
	c := newTestCatalog(t)
	sch, ok := c.Describe("pin add", ActorModel)
	if !ok {
		t.Fatal("Describe(pin add) not found")
	}
	if sch.Name != "pin add" || sch.Title != "Add Pin" || sch.Category != "pinning" {
		t.Fatalf("Describe fields wrong: %+v", sch)
	}
	if sch.Safety != SafetyRead || sch.Interaction != InteractionAgentSafe || sch.Visibility != VisibilityModel {
		t.Fatalf("Describe classification wrong: %+v", sch)
	}
	// MCP description resolves from the MCPTargets fallback.
	if sch.Description != "Agent: add a pin" {
		t.Fatalf("Describe Description = %q, want agent description", sch.Description)
	}
	if _, ok := c.Describe("missing", ActorModel); ok {
		t.Fatal("Describe(missing) should be not-found")
	}
}

// TestDescribeScopesToActor verifies Describe both honors the audience for the
// acting actor and enforces the app-only visibility boundary for model actors.
func TestDescribeScopesToActor(t *testing.T) {
	c := newTestCatalog(t)
	// A model actor describes a model-visible op: agent description.
	sch, ok := c.Describe("pin add", ActorModel)
	if !ok || sch.Description != "Agent: add a pin" {
		t.Fatalf("model Describe = %+v ok=%v, want agent description", sch, ok)
	}
	// An app/human actor Cannot describe a model-only op: the app surface
	// (VisibilityAppOnly) excludes it, mirroring what the MCP app compiler
	// Search returns — so Describe and the compiler can never disagree.
	if _, ok := c.Describe("pin add", ActorApp); ok {
		t.Fatal("app Describe(model-only) should be not-found")
	}
	if _, ok := c.Describe("pin add", ActorHuman); ok {
		t.Fatal("human Describe(model-only) should be not-found")
	}
	// App-only op is invisible to a model actor but visible to the app/human.
	if _, ok := c.Describe("internal resync", ActorModel); ok {
		t.Fatal("model Describe(app-only) should be not-found")
	}
	if sch, ok := c.Describe("internal resync", ActorApp); !ok || sch.Name != "internal resync" {
		t.Fatalf("app Describe(app-only) = %+v ok=%v, want found", sch, ok)
	}
	if sch, ok := c.Describe("internal resync", ActorHuman); !ok || sch.Name != "internal resync" {
		t.Fatalf("human Describe(app-only) = %+v ok=%v, want found", sch, ok)
	}
	// VisibilityBoth ops are visible to both the model and app surfaces.
	// The description is audience-swapped: a model actor gets the MCP/agent
	// fallback description, while an app/human actor gets the plain human
	// Description so the app surface never exposes agent text.
	if sch, ok := c.Describe("websites list", ActorModel); !ok || sch.Description != "Agent: list sites" {
		t.Fatalf("model Describe(both) = %+v ok=%v, want agent fallback description", sch, ok)
	}
	if sch, ok := c.Describe("websites list", ActorApp); !ok || sch.Description != "Long websites desc" {
		t.Fatalf("app Describe(both) = %+v ok=%v, want plain human description", sch, ok)
	}
}

func TestDescribeInputSchema(t *testing.T) {
	stubOp := NewOperation(OperationSpec{
		Name: "schema op", Title: "T", Category: "c",
		Safety: SafetyRead, Interaction: InteractionAgentSafe, Visibility: VisibilityModel,
		Args: []OperationArg{
			{Name: "name", Type: ArgTypeString, Required: true},
			{Name: "count", Type: ArgTypeInt},
			{Name: "tags", Type: ArgTypeStringSlice},
		},
	})
	c := NewCatalog()
	if err := c.Add(stubOp); err != nil {
		t.Fatal(err)
	}
	sch, ok := c.Describe("schema op", ActorModel)
	if !ok {
		t.Fatal("Describe not found")
	}
	var parsed struct {
		Type       string                     `json:"type"`
		Properties map[string]json.RawMessage `json:"properties"`
		Required   []string                   `json:"required"`
	}
	if err := json.Unmarshal(sch.InputSchema, &parsed); err != nil {
		t.Fatalf("InputSchema is not valid JSON: %v", err)
	}
	if parsed.Type != "object" {
		t.Fatalf("InputSchema type = %q, want object", parsed.Type)
	}
	if len(parsed.Properties) != 3 {
		t.Fatalf("InputSchema properties = %v, want 3", parsed.Properties)
	}
	if len(parsed.Required) != 1 || parsed.Required[0] != "name" {
		t.Fatalf("InputSchema required = %v, want [name]", parsed.Required)
	}
	var tagType struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(parsed.Properties["tags"], &tagType); err != nil {
		t.Fatal(err)
	}
	if tagType.Type != "array" {
		t.Fatalf("tags property type = %q, want array", tagType.Type)
	}
}

func TestInvokeAgentSafeForModel(t *testing.T) {
	c := newTestCatalog(t)
	got, err := c.Invoke(context.Background(), "pin add", map[string]any{"cid": "QmX"}, ActorModel)
	if err != nil {
		t.Fatalf("Invoke(agent-safe, model): %v", err)
	}
	if got != "ran:pin add" {
		t.Fatalf("Invoke result = %v, want marker", got)
	}
}

func TestInvokeRefusesHumanOnlyForModel(t *testing.T) {
	c := newTestCatalog(t)
	if _, err := c.Invoke(context.Background(), "account login", map[string]any{}, ActorModel); err == nil {
		t.Fatal("HumanOnly op should be refused for ActorModel")
	}
	// Also refused for the app actor, which is not human.
	if _, err := c.Invoke(context.Background(), "account login", map[string]any{}, ActorApp); err == nil {
		t.Fatal("HumanOnly op should be refused for ActorApp")
	}
}

// TestInvokeHumanOnlyRefusalWrapsErrHumanRequired is a regression test for the
// exported ErrHumanRequired sentinel: an InteractionHumanOnly op invoked by a
// non-human actor (this is a Handoff-capable flow, the same gate refuses both
// InteractionHumanOnly and InteractionNeedsHandoff) must return an error that
// errors.Is matches against ErrHumanRequired, and must NOT match the distinct
// ErrConfirmRequired sentinel, so callers can route refusals precisely.
func TestInvokeHumanRequiredSentinel(t *testing.T) {
	c := newTestCatalog(t)
	_, err := c.Invoke(context.Background(), "account login", map[string]any{}, ActorModel)
	if err == nil {
		t.Fatal("HumanOnly op should be refused for ActorModel")
	}
	if !errors.Is(err, ErrHumanRequired) {
		t.Fatalf("err = %v, want errors.Is(err, ErrHumanRequired)", err)
	}
	if errors.Is(err, ErrConfirmRequired) {
		t.Fatalf("err = %v, should NOT be ErrConfirmRequired", err)
	}
}

func TestInvokeAllowsHumanOnlyForHuman(t *testing.T) {
	c := newTestCatalog(t)
	got, err := c.Invoke(context.Background(), "account login", map[string]any{"cid": "x"}, ActorHuman)
	if err != nil {
		t.Fatalf("Invoke(HumanOnly, human): %v", err)
	}
	if got != "ran:account login" {
		t.Fatalf("Invoke result = %v, want marker", got)
	}
}

func TestInvokeRefusesAppOnlyForModel(t *testing.T) {
	c := newTestCatalog(t)
	_, err := c.Invoke(context.Background(), "internal resync", map[string]any{}, ActorModel)
	if err == nil {
		t.Fatal("AppOnly op should be refused for ActorModel")
	}
	// A human may run the app-only helper.
	if _, err := c.Invoke(context.Background(), "internal resync", map[string]any{"cid": "x"}, ActorHuman); err != nil {
		t.Fatalf("Invoke(AppOnly, human) should be allowed: %v", err)
	}
}

func TestInvokeDestructiveForModelReturnsConfirmRequired(t *testing.T) {
	c := newTestCatalog(t)
	_, err := c.Invoke(context.Background(), "pin remove", map[string]any{}, ActorModel)
	if err == nil {
		t.Fatal("Destructive op for ActorModel should error")
	}
	if !errors.Is(err, ErrConfirmRequired) {
		t.Fatalf("err = %v, want errors.Is(err, ErrConfirmRequired)", err)
	}
}

func TestInvokeDestructiveForAppAndHumanRuns(t *testing.T) {
	c := newTestCatalog(t)
	// A human can run a destructive op (they can confirm in-app).
	if _, err := c.Invoke(context.Background(), "pin remove", map[string]any{"cid": "x"}, ActorHuman); err != nil {
		t.Fatalf("Invoke(Destructive, human) should run: %v", err)
	}
	// The app can run a destructive op directly too.
	if _, err := c.Invoke(context.Background(), "pin remove", map[string]any{"cid": "x"}, ActorApp); err != nil {
		t.Fatalf("Invoke(Destructive, app) should run: %v", err)
	}
}

func TestInvokeUnknownOperation(t *testing.T) {
	c := newTestCatalog(t)
	if _, err := c.Invoke(context.Background(), "nope", map[string]any{}, ActorModel); err == nil {
		t.Fatal("Invoke(unknown) should error")
	}
}

// TestInvokeHandlerlessOperationErrors guards against a nil-handler panic:
// an operation registered without a handler (schema-only, e.g. for Describe
// or search) must return a descriptive error from Invoke, not dereference nil.
func TestInvokeHandlerlessOperationErrors(t *testing.T) {
	c := NewCatalog()
	noHander := NewOperation(OperationSpec{
		Name: "schema only", Safety: SafetyRead,
		Interaction: InteractionAgentSafe, Visibility: VisibilityBoth,
	})
	if err := c.Add(noHander); err != nil {
		t.Fatalf("Add(schema-only op): %v", err)
	}
	_, err := c.Invoke(context.Background(), "schema only", map[string]any{}, ActorHuman)
	if err == nil {
		t.Fatal("Invoke(handlerless op) should error")
	}
	if !strings.Contains(err.Error(), "no handler") {
		t.Fatalf("Invoke(handlerless op) error = %q, want it to mention missing handler", err)
	}
}

// captureHandler records the input map it received, letting Invoke tests assert
// that declared defaults were applied before the Handler ran.
type captureHandler struct{ got map[string]any }

func (h *captureHandler) Execute(_ context.Context, input map[string]any) (any, error) {
	h.got = input
	return "ok", nil
}

// TestInvokeAppliesDeclaredDefaults verifies Invoke fills declared defaults for
// absent args before dispatching to the Handler, so the input is uniform with
// the CLI actionFor path (which relies on the same normalizeInputDefaults sink).
func TestInvokeAppliesDeclaredDefaults(t *testing.T) {
	h := &captureHandler{}
	c := NewCatalog()
	if err := c.Add(NewOperation(OperationSpec{
		Name: "job.schedule", Title: "Schedule", Summary: "schedule a job",
		Category: "job", Safety: SafetyMutate,
		Interaction: InteractionAgentSafe, Visibility: VisibilityModel,
		Args: []OperationArg{
			{Name: "name", Type: ArgTypeString, Required: true},
			{Name: "ttl", Type: ArgTypeInt, Default: "3600"},
			{Name: "grace", Type: ArgTypeDuration, Default: "30s"},
			{Name: "public", Type: ArgTypeBool, Default: "true"},
			{Name: "tags", Type: ArgTypeStringSlice, Default: "a, b, b"},
		},
		Handler: h,
	})); err != nil {
		t.Fatalf("Add: %v", err)
	}
	// Invoke with only the required arg; the optional defaults must be filled.
	if _, err := c.Invoke(context.Background(), "job.schedule", map[string]any{"name": "x"}, ActorModel); err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if h.got["ttl"] != 3600 {
		t.Fatalf("default ttl not applied, got %#v", h.got["ttl"])
	}
	if d, ok := h.got["grace"].(time.Duration); !ok || d != 30*time.Second {
		t.Fatalf("default grace not applied, got %#v", h.got["grace"])
	}
	if h.got["public"] != true {
		t.Fatalf("default public not applied, got %#v", h.got["public"])
	}
	// Comma-joined slice defaults split to match urfave's StringSlice parsing
	// (--tags a,b => ["a","b"]), trimming whitespace and dropping empties.
	tags, ok := h.got["tags"].([]string)
	if !ok || len(tags) != 3 || tags[0] != "a" || tags[1] != "b" || tags[2] != "b" {
		t.Fatalf("default tags not split as comma list, got %#v", h.got["tags"])
	}
	// An explicitly supplied value must not be clobbered by the default.
	h2 := &captureHandler{}
	if err := c.Add(NewOperation(OperationSpec{
		Name: "job.schedule2", Title: "S", Summary: "s",
		Category: "job", Safety: SafetyMutate,
		Interaction: InteractionAgentSafe, Visibility: VisibilityModel,
		Args: []OperationArg{
			{Name: "ttl", Type: ArgTypeInt, Required: true, Default: "999"},
		},
		Handler: h2,
	})); err != nil {
		t.Fatalf("Add peer: %v", err)
	}
	if _, err := c.Invoke(context.Background(), "job.schedule2", map[string]any{"ttl": 5}, ActorModel); err != nil {
		t.Fatalf("Invoke peer: %v", err)
	}
	if h2.got["ttl"] != 5 {
		t.Fatalf("explicit ttl 5 clobbered by default, got %#v", h2.got["ttl"])
	}
}

// TestInvokeAliasesCamelCaseToKebabArg verifies a model sending the camelCase
// spelling of a kebab-declared arg (e.g. "deviceName" for "device-name") is
// normalized to the declared key before the Handler runs, so Handlers read a
// single canonical name and need no per-op dual-read.
func TestInvokeAliasesCamelCaseToKebabArg(t *testing.T) {
	h := &captureHandler{}
	c := NewCatalog()
	if err := c.Add(NewOperation(OperationSpec{
		Name: "job.device", Title: "Device", Summary: "device",
		Category: "job", Safety: SafetyMutate,
		Interaction: InteractionAgentSafe, Visibility: VisibilityModel,
		Args: []OperationArg{
			{Name: "device-name", Type: ArgTypeString, Required: true},
		},
		Handler: h,
	})); err != nil {
		t.Fatalf("Add: %v", err)
	}
	// Model sends camelCase only; the handler must see the canonical key.
	if _, err := c.Invoke(context.Background(), "job.device", map[string]any{"deviceName": "laptop"}, ActorModel); err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if got, _ := h.got["device-name"].(string); got != "laptop" {
		t.Fatalf("camelCase arg not aliased to kebab key, got %#v", h.got["device-name"])
	}
	if _, still := h.got["deviceName"]; still {
		t.Fatalf("camelCase alias should be removed from handler input, got %#v", h.got)
	}
	// The canonical kebab spelling still works normally.
	h2 := &captureHandler{}
	if err := c.Add(NewOperation(OperationSpec{
		Name: "job.device2", Title: "D", Summary: "d",
		Category: "job", Safety: SafetyMutate,
		Interaction: InteractionAgentSafe, Visibility: VisibilityModel,
		Args: []OperationArg{
			{Name: "device-name", Type: ArgTypeString, Required: true},
		},
		Handler: h2,
	})); err != nil {
		t.Fatalf("Add peer: %v", err)
	}
	if _, err := c.Invoke(context.Background(), "job.device2", map[string]any{"device-name": "workstation"}, ActorModel); err != nil {
		t.Fatalf("Invoke peer: %v", err)
	}
	if got, _ := h2.got["device-name"].(string); got != "workstation" {
		t.Fatalf("kebab arg not passed through, got %#v", h2.got["device-name"])
	}
}

// TestInvokeMissingRequiredArgErrors verifies Invoke rejects a handlerless
// dispatch when a required arg (no Default) is absent from input, mirroring the
// CLI actionFor contract. An arg that is Required but declares a Default is
// satisfied by normalizeInputDefaults, so it must not be rejected.
func TestInvokeMissingRequiredArgErrors(t *testing.T) {
	c := NewCatalog()
	if err := c.Add(NewOperation(OperationSpec{
		Name: "job.schedule", Title: "Schedule", Summary: "schedule a job",
		Category: "job", Safety: SafetyMutate,
		Interaction: InteractionAgentSafe, Visibility: VisibilityModel,
		Args: []OperationArg{
			{Name: "name", Type: ArgTypeString, Required: true},
			{Name: "ttl", Type: ArgTypeInt, Required: true, Default: "3600"},
		},
		Handler: &captureHandler{},
	})); err != nil {
		t.Fatalf("Add: %v", err)
	}
	// Missing required, non-defaulted arg -> error naming it.
	_, err := c.Invoke(context.Background(), "job.schedule", map[string]any{}, ActorModel)
	if err == nil {
		t.Fatal("Invoke with missing required arg should error")
	}
	if !strings.Contains(err.Error(), "name") {
		t.Fatalf("error = %q, want it to name the missing arg \"name\"", err)
	}
	// Present-but-empty required string arg is treated as missing, mirroring the
	// CLI actionFor empty check (set-but-empty required flag is refused).
	if _, err := c.Invoke(context.Background(), "job.schedule", map[string]any{"name": ""}, ActorModel); err == nil {
		t.Fatal("Invoke with empty required string arg should error")
	}
	// A nil value (JSON null from an MCP caller) is treated as empty/missing too.
	if _, err := c.Invoke(context.Background(), "job.schedule", map[string]any{"name": nil}, ActorModel); err == nil {
		t.Fatal("Invoke with nil required string arg should error")
	}
	// Required-but-defaulted arg is satisfied by the default -> no error.
	if _, err := c.Invoke(context.Background(), "job.schedule", map[string]any{"name": "x"}, ActorModel); err != nil {
		t.Fatalf("Invoke with required+default arg present should succeed: %v", err)
	}
}

// TestInvokeStringSliceAnyEmptyHandling pins the round-13 fix: an MCP caller
// decodes JSON arrays into []any, not []string. An empty []any{} must be treated
// like an empty []string by the required-arg check, and a defaulted StringSlice
// arg passed as []any{} must still receive its declared default (not a dangling
// empty []any{}).
func TestInvokeStringSliceAnyEmptyHandling(t *testing.T) {
	h := &captureHandler{}
	c := NewCatalog()
	if err := c.Add(NewOperation(OperationSpec{
		Name: "scan", Title: "Scan", Summary: "scan tags",
		Category: "scan", Safety: SafetyMutate,
		Interaction: InteractionAgentSafe, Visibility: VisibilityModel,
		Args: []OperationArg{
			{Name: "tags", Type: ArgTypeStringSlice, Required: true},
		},
		Handler: h,
	})); err != nil {
		t.Fatalf("Add: %v", err)
	}
	// A JSON-decoded empty array for a required slice is treated as missing.
	if _, err := c.Invoke(context.Background(), "scan", map[string]any{"tags": []any{}}, ActorModel); err == nil {
		t.Error("Invoke with []any{} required slice should error")
	}
	// A non-empty []any is a valid value and reaches the Handler, coerced to
	// []string so a Handler type-asserting []string never panics (MCP supplies
	// []any, CLI supplies []string — both must converge).
	if _, err := c.Invoke(context.Background(), "scan", map[string]any{"tags": []any{"a", "b"}}, ActorModel); err != nil {
		t.Fatalf("Invoke with non-empty []any slice: %v", err)
	}
	if tags, ok := h.got["tags"].([]string); !ok || len(tags) != 2 || tags[0] != "a" || tags[1] != "b" {
		t.Fatalf("[]any slice not coerced to []string, got %#v", h.got["tags"])
	}

	// A defaulted slice passed as empty []any gets its default, not []any{}.
	h2 := &captureHandler{}
	if err := c.Add(NewOperation(OperationSpec{
		Name: "scan2", Title: "Scan2", Summary: "scan with defaults",
		Category: "scan", Safety: SafetyMutate,
		Interaction: InteractionAgentSafe, Visibility: VisibilityModel,
		Args: []OperationArg{
			{Name: "tags", Type: ArgTypeStringSlice, Default: "a,b"},
		},
		Handler: h2,
	})); err != nil {
		t.Fatalf("Add defaulted: %v", err)
	}
	if _, err := c.Invoke(context.Background(), "scan2", map[string]any{"tags": []any{}}, ActorModel); err != nil {
		t.Fatalf("Invoke defaulted: %v", err)
	}
	tags, ok := h2.got["tags"].([]string)
	if !ok || len(tags) != 2 || tags[0] != "a" || tags[1] != "b" {
		t.Fatalf("defaulted slice passed as []any{} got %#v, want default [a b]", h2.got["tags"])
	}
	// A defaulted slice supplied as non-empty []any is kept AND coerced to
	// []string — not clobbered by the default, and not left as []any.
	h3 := &captureHandler{}
	if err := c.Add(NewOperation(OperationSpec{
		Name: "scan3", Title: "Scan3", Summary: "scan supplied tags",
		Category: "scan", Safety: SafetyMutate,
		Interaction: InteractionAgentSafe, Visibility: VisibilityModel,
		Args: []OperationArg{
			{Name: "tags", Type: ArgTypeStringSlice, Default: "x,y"},
		},
		Handler: h3,
	})); err != nil {
		t.Fatalf("Add scan3: %v", err)
	}
	if _, err := c.Invoke(context.Background(), "scan3", map[string]any{"tags": []any{"p", "q"}}, ActorModel); err != nil {
		t.Fatalf("Invoke scan3: %v", err)
	}
	if tags, ok := h3.got["tags"].([]string); !ok || len(tags) != 2 || tags[0] != "p" || tags[1] != "q" {
		t.Fatalf("supplied []any not coerced to []string / clobbered, got %#v", h3.got["tags"])
	}
}

// TestInvokeStringSliceRejectsNonStringElement pins the round-17 fix: a
// StringSlice supplied as []any with a non-string element (or as a non-slice
// scalar) must ERROR loudly rather than silently drop the element and hand the
// Handler a truncated value.
func TestInvokeStringSliceRejectsNonStringElement(t *testing.T) {
	c := NewCatalog()
	if err := c.Add(NewOperation(OperationSpec{
		Name: "scan", Title: "Scan", Summary: "scan tags",
		Category: "scan", Safety: SafetyMutate,
		Interaction: InteractionAgentSafe, Visibility: VisibilityModel,
		Args: []OperationArg{
			{Name: "tags", Type: ArgTypeStringSlice, Required: true},
		},
		Handler: &captureHandler{},
	})); err != nil {
		t.Fatalf("Add: %v", err)
	}
	cases := []map[string]any{
		{"tags": []any{"a", 5}},  // mixed, non-string element
		{"tags": []any{1, 2, 3}}, // all non-string
		{"tags": "not-a-slice"},  // scalar where a slice is declared
	}
	for _, in := range cases {
		if _, err := c.Invoke(context.Background(), "scan", in, ActorModel); err == nil {
			t.Fatalf("Invoke with %#v should error, got nil", in["tags"])
		}
	}
	// An all-string []any still passes and is coerced to []string.
	h := &captureHandler{}
	if err := c.Add(NewOperation(OperationSpec{
		Name: "scanok", Title: "ScanOk", Summary: "scan tags",
		Category: "scan", Safety: SafetyMutate,
		Interaction: InteractionAgentSafe, Visibility: VisibilityModel,
		Args: []OperationArg{
			{Name: "tags", Type: ArgTypeStringSlice, Required: true},
		},
		Handler: h,
	})); err != nil {
		t.Fatalf("Add scanok: %v", err)
	}
	if _, err := c.Invoke(context.Background(), "scanok", map[string]any{"tags": []any{"a", "b"}}, ActorModel); err != nil {
		t.Fatalf("Invoke with all-string []any should succeed: %v", err)
	}
	if tags, ok := h.got["tags"].([]string); !ok || len(tags) != 2 {
		t.Fatalf("all-string []any not coerced, got %#v", h.got["tags"])
	}
}

// TestInvokeOptionalEmptySliceReachesHandlerCoerced pins the round-18 fix: an
// OPTIONAL StringSlice arg with no Default, supplied by an MCP caller as the
// present-but-empty []any{}, must reach the Handler as []string{} — never as the
// raw []any{}, which would panic a Handler type-asserting `.([]string)`.
func TestInvokeOptionalEmptySliceReachesHandlerCoerced(t *testing.T) {
	c := NewCatalog()
	h := &captureHandler{}
	if err := c.Add(NewOperation(OperationSpec{
		Name: "tags", Title: "Tags", Summary: "optional tags",
		Category: "tags", Safety: SafetyMutate,
		Interaction: InteractionAgentSafe, Visibility: VisibilityModel,
		Args: []OperationArg{
			{Name: "tags", Type: ArgTypeStringSlice}, // optional, no default
		},
		Handler: h,
	})); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := c.Invoke(context.Background(), "tags", map[string]any{"tags": []any{}}, ActorModel); err != nil {
		t.Fatalf("Invoke optional empty []any{} should succeed: %v", err)
	}
	if tags, ok := h.got["tags"].([]string); !ok || len(tags) != 0 {
		t.Fatalf("optional empty []any{} not coerced to []string{}, got %#v", h.got["tags"])
	}
}

// TestInvokeOptionalNullSliceReachesHandlerCoerced pins the round-19 fix: an
// OPTIONAL StringSlice supplied as JSON null (nil) must not hard-error in
// normalizeSlice — it is coerced to []string{} (the []any{} shape), so a Handler
// type-asserting `.([]string)` still receives a []string.
func TestInvokeOptionalNullSliceReachesHandlerCoerced(t *testing.T) {
	c := NewCatalog()
	h := &captureHandler{}
	if err := c.Add(NewOperation(OperationSpec{
		Name: "tags", Title: "Tags", Summary: "optional tags",
		Category: "tags", Safety: SafetyMutate,
		Interaction: InteractionAgentSafe, Visibility: VisibilityModel,
		Args: []OperationArg{
			{Name: "tags", Type: ArgTypeStringSlice}, // optional, no default
		},
		Handler: h,
	})); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := c.Invoke(context.Background(), "tags", map[string]any{"tags": nil}, ActorModel); err != nil {
		t.Fatalf("Invoke optional null slice should succeed, not hard-error: %v", err)
	}
	if tags, ok := h.got["tags"].([]string); !ok || len(tags) != 0 {
		t.Fatalf("optional null slice not coerced to []string{}, got %#v", h.got["tags"])
	}
}

// TestInvokeOptionalAbsentArgReachesZeroShape pins the round-21 fix: an optional
// arg OMITTED from the input (stateAbsent) with no default must reach the Handler
// as the declared zero-shape (e.g. "" for a string, []string{} for a slice) —
// never as a naked nil, which would panic a Handler type-asserting `.(string)`.
func TestInvokeOptionalAbsentArgReachesZeroShape(t *testing.T) {
	c := NewCatalog()
	h := &captureHandler{}
	if err := c.Add(NewOperation(OperationSpec{
		Name: "opts", Title: "Opts", Summary: "optional args",
		Category: "opts", Safety: SafetyMutate,
		Interaction: InteractionAgentSafe, Visibility: VisibilityModel,
		Args: []OperationArg{
			{Name: "name", Type: ArgTypeString},      // optional string
			{Name: "tags", Type: ArgTypeStringSlice}, // optional slice
			{Name: "ttl", Type: ArgTypeInt},          // optional int
		},
		Handler: h,
	})); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := c.Invoke(context.Background(), "opts", map[string]any{}, ActorModel); err != nil {
		t.Fatalf("Invoke with no optional args should succeed: %v", err)
	}
	if v, ok := h.got["name"].(string); !ok || v != "" {
		t.Fatalf("absent optional string got %#v, want \"\"", h.got["name"])
	}
	if v, ok := h.got["tags"].([]string); !ok || len(v) != 0 {
		t.Fatalf("absent optional slice got %#v, want []string{}", h.got["tags"])
	}
	if v, ok := h.got["ttl"].(int); !ok || v != 0 {
		t.Fatalf("absent optional int got %#v, want 0", h.got["ttl"])
	}
}

// TestInvokeCoercesMCPScalarTypes pins the round-15 fix: an MCP caller decodes
// JSON scalars into shapes that differ from the CLI (integers -> float64,
// durations -> string). coerceValue must reconcile these so Handlers type-
// asserting the CLI shapes (int, time.Duration) don't panic on MCP invocations.
func TestInvokeCoercesMCPScalarTypes(t *testing.T) {
	h := &captureHandler{}
	c := NewCatalog()
	if err := c.Add(NewOperation(OperationSpec{
		Name: "billing.set", Title: "Set", Summary: "set billing values",
		Category: "billing", Safety: SafetyMutate,
		Interaction: InteractionAgentSafe, Visibility: VisibilityModel,
		Args: []OperationArg{
			{Name: "ttl", Type: ArgTypeInt, Required: true},
			{Name: "rate", Type: ArgTypeFloat, Required: true},
			{Name: "retry", Type: ArgTypeDuration},
		},
		Handler: h,
	})); err != nil {
		t.Fatalf("Add: %v", err)
	}
	// MCP JSON-decode shapes: int -> float64, float -> float64, duration -> string.
	if _, err := c.Invoke(context.Background(), "billing.set", map[string]any{
		"ttl":   float64(30),
		"rate":  float64(1.5),
		"retry": "10s",
	}, ActorModel); err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if v, ok := h.got["ttl"].(int); !ok || v != 30 {
		t.Fatalf("ttl float64 not coerced to int, got %#v", h.got["ttl"])
	}
	if v, ok := h.got["rate"].(float64); !ok || v != 1.5 {
		t.Fatalf("rate got %#v, want float64 1.5", h.got["rate"])
	}
	if d, ok := h.got["retry"].(time.Duration); !ok || d != 10*time.Second {
		t.Fatalf("retry string not coerced to time.Duration, got %#v", h.got["retry"])
	}
	// A caller that already supplies the CLI shape passes through unchanged.
	h2 := &captureHandler{}
	if err := c.Add(NewOperation(OperationSpec{
		Name: "billing.set2", Title: "Set2", Summary: "set typed values",
		Category: "billing", Safety: SafetyMutate,
		Interaction: InteractionAgentSafe, Visibility: VisibilityModel,
		Args:    []OperationArg{{Name: "ttl", Type: ArgTypeInt, Required: true}},
		Handler: h2,
	})); err != nil {
		t.Fatalf("Add typed: %v", err)
	}
	if _, err := c.Invoke(context.Background(), "billing.set2", map[string]any{"ttl": 7}, ActorModel); err != nil {
		t.Fatalf("Invoke typed: %v", err)
	}
	if v, ok := h2.got["ttl"].(int); !ok || v != 7 {
		t.Fatalf("already-int ttl clobbered, got %#v", h2.got["ttl"])
	}
}

// TestInvokeRefusesNeedsHandoffForNonHuman pins the round-10 gate: an operation
// with InteractionNeedsHandoff (external browser/device, two-call split) must be
// refused for any non-human actor, exactly like InteractionHumanOnly.
func TestInvokeRefusesNeedsHandoffForNonHuman(t *testing.T) {
	c := NewCatalog()
	if err := c.Add(NewOperation(OperationSpec{
		Name: "auth.sso", Title: "SSO", Summary: "log in via external device",
		Category: "auth", Safety: SafetyMutate,
		Interaction: InteractionNeedsHandoff, Visibility: VisibilityBoth,
		Args:    []OperationArg{{Name: "name", Type: ArgTypeString, Required: true}},
		Handler: &captureHandler{},
	})); err != nil {
		t.Fatalf("Add: %v", err)
	}
	// A model agent must be refused.
	if _, err := c.Invoke(context.Background(), "auth.sso", map[string]any{"name": "x"}, ActorModel); err == nil {
		t.Error("NeedsHandoff op should be refused for ActorModel")
	}
	// The app actor is not a human, so it is refused too.
	if _, err := c.Invoke(context.Background(), "auth.sso", map[string]any{"name": "x"}, ActorApp); err == nil {
		t.Error("NeedsHandoff op should be refused for ActorApp")
	}
	// A human may drive it.
	if _, err := c.Invoke(context.Background(), "auth.sso", map[string]any{"name": "x"}, ActorHuman); err != nil {
		t.Fatalf("Invoke(NeedsHandoff, human) should run: %v", err)
	}
}

// TestAddRejectsMalformedDefault pins the #3 fix: an operation whose typed arg
// declares a Default that cannot parse (e.g. "abc" for an int) must be rejected
// at Add, not silently coerced to a wrong zero value at dispatch.
func TestAddRejectsMalformedDefault(t *testing.T) {
	cases := []struct {
		name string
		arg  OperationArg
	}{
		{"bad int", OperationArg{Name: "n", Type: ArgTypeInt, Default: "abc"}},
		{"bad float", OperationArg{Name: "n", Type: ArgTypeFloat, Default: "x.y"}},
		{"bad duration", OperationArg{Name: "n", Type: ArgTypeDuration, Default: "soon"}},
		{"bad bool", OperationArg{Name: "n", Type: ArgTypeBool, Default: "yes"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := NewCatalog()
			err := c.Add(NewOperation(OperationSpec{
				Name: "op", Title: "Op", Summary: "op",
				Category: "cat", Safety: SafetyMutate,
				Interaction: InteractionAgentSafe, Visibility: VisibilityModel,
				Args:    []OperationArg{tc.arg},
				Handler: &captureHandler{},
			}))
			if err == nil {
				t.Fatalf("Add with malformed default %q should error", tc.arg.Default)
			}
		})
	}
	// Valid defaults for every typed arg still register fine.
	c := NewCatalog()
	if err := c.Add(NewOperation(OperationSpec{
		Name: "ok", Title: "Ok", Summary: "ok",
		Category: "cat", Safety: SafetyMutate,
		Interaction: InteractionAgentSafe, Visibility: VisibilityModel,
		Args: []OperationArg{
			{Name: "b", Type: ArgTypeBool, Default: "true"},
			{Name: "i", Type: ArgTypeInt, Default: "5"},
			{Name: "f", Type: ArgTypeFloat, Default: "1.5"},
			{Name: "d", Type: ArgTypeDuration, Default: "30s"},
			{Name: "s", Type: ArgTypeString, Default: "anything"},
			{Name: "sl", Type: ArgTypeStringSlice, Default: "a,b"},
		},
		Handler: &captureHandler{},
	})); err != nil {
		t.Fatalf("Add with valid defaults should succeed: %v", err)
	}
}

// TestResolveArgStateMachine pins the Option-A consolidation: resolveArg is the
// SOLE classifier + coercer of a raw argument value. Absent/Null/Empty/Filled/
// Invalid must be an exhaustive partition, and the coercion it returns must agree
// with the state it reports (the round-17/18/19 drift was exactly two functions
// disagreeing about this). No new surface — resolveArg is unexported.
func TestResolveArgStateMachine(t *testing.T) {
	str := OperationArg{Type: ArgTypeString}
	enum := OperationArg{Type: ArgTypeString, Enum: []string{"A", "MX", "TXT"}}
	sli := OperationArg{Type: ArgTypeStringSlice}
	num := OperationArg{Type: ArgTypeInt}
	flt := OperationArg{Type: ArgTypeFloat}
	dur := OperationArg{Type: ArgTypeDuration}

	// 2^62 exceeds a 32-bit native int but fits a 64-bit one; the expected
	// state for the platform-range case below depends on the build's int size.
	largeIntWant := stateFilled
	if strconv.IntSize < 63 {
		largeIntWant = stateInvalid
	}

	cases := []struct {
		name    string
		a       OperationArg
		raw     any
		present bool
		want    argState
	}{
		{"string absent", str, nil, false, stateAbsent},
		{"string null", str, nil, true, stateNull},
		{"string empty", str, "", true, stateEmpty},
		{"string filled", str, "x", true, stateFilled},
		{"string wrong type", str, 1, true, stateInvalid},
		{"enum in-range", enum, "MX", true, stateFilled},
		{"enum in-range lowercase", enum, "mx", true, stateFilled},
		{"enum in-range mixed case", enum, "TxT", true, stateFilled},
		{"enum out-of-range", enum, "ZZZ", true, stateInvalid},
		{"enum empty string", enum, "", true, stateEmpty},

		{"slice absent", sli, nil, false, stateAbsent},
		{"slice null", sli, nil, true, stateNull},
		{"slice empty []any", sli, []any{}, true, stateEmpty},
		{"slice empty []string", sli, []string{}, true, stateEmpty},
		{"slice filled []any", sli, []any{"a"}, true, stateFilled},
		{"slice filled []string", sli, []string{"a"}, true, stateFilled},
		{"slice non-string element", sli, []any{"a", 5}, true, stateInvalid},
		{"slice scalar", sli, "x", true, stateInvalid},

		{"int absent", num, nil, false, stateAbsent},
		{"int null", num, nil, true, stateNull},
		{"int filled int", num, 5, true, stateFilled},
		{"int filled float64", num, float64(5), true, stateFilled},
		{"int fractional float64", num, float64(30.7), true, stateInvalid},
		{"int wrong type", num, "5", true, stateInvalid},
		// Exactly 2^63 as float64 must be rejected: float64(math.MaxInt) rounds
		// 2^63-1 up to 2^63, so a loose > bound would let it overflow silently.
		{"int 2^63 boundary", num, math.Exp2(63), true, stateInvalid},
		// A value in int64 range but possibly beyond the platform's native int:
		// 2^62 fits a 64-bit int (stateFilled) but is rejected on 32-bit builds
		// (resolveArg round-trips through the native int — round 25). The
		// expected state is therefore platform-dependent.
		{"int large platform-range", num, math.Exp2(62), true, largeIntWant},
		// Round-26: the public Invoke API must accept any native Go integer a
		// caller can pass for an int arg — not just int and float64.
		{"int filled int64", num, int64(42), true, stateFilled},
		{"int filled int32", num, int32(42), true, stateFilled},
		{"int filled uint", num, uint(42), true, stateFilled},
		{"int filled json.Number", num, json.Number("42"), true, stateFilled},
		{"int uint64 overflow", num, uint64(1 << 63), true, stateInvalid},
		{"int json.Number fractional", num, json.Number("42.5"), true, stateInvalid},

		{"float filled float32", flt, float32(1.5), true, stateFilled},
		{"float filled int64", flt, int64(7), true, stateFilled},
		{"float filled json.Number", flt, json.Number("1.5"), true, stateFilled},

		{"duration absent", dur, nil, false, stateAbsent},
		{"duration null", dur, nil, true, stateNull},
		{"duration empty string", dur, "", true, stateEmpty},
		{"duration filled duration", dur, time.Second, true, stateFilled},
		{"duration filled string", dur, "1s", true, stateFilled},
		{"duration bad string", dur, "soon", true, stateInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, st, err := resolveArg(tc.a, tc.raw, tc.present)
			if err != nil != (tc.want == stateInvalid) {
				t.Fatalf("resolveArg(%v,%v,present=%v) err=%v, wantInvalid=%v",
					tc.a.Name, tc.raw, tc.present, err, tc.want == stateInvalid)
			}
			if st != tc.want {
				t.Fatalf("resolveArg state = %d, want %d (err=%v)", st, tc.want, err)
			}
		})
	}

	// Classification and coercion agree: a coerced Null slice yields []string{}.
	_, st, _ := resolveArg(sli, nil, true)
	if st != stateNull {
		t.Fatalf("null slice state = %d, want stateNull", st)
	}
}

// TestInvokeRejectsWrongScalarType pins the strict-scalar behavior gained by the
// consolidation: a present arg of the wrong scalar type (e.g. a string where an
// int is declared) now errors loudly in resolveArg instead of being passed
// through to the Handler as a wrong shape, matching the round-17 principle for
// slices.
func TestInvokeRejectsWrongScalarType(t *testing.T) {
	c := NewCatalog()
	if err := c.Add(NewOperation(OperationSpec{
		Name: "billing.set", Title: "Set", Summary: "set values",
		Category: "billing", Safety: SafetyMutate,
		Interaction: InteractionAgentSafe, Visibility: VisibilityModel,
		Args:    []OperationArg{{Name: "ttl", Type: ArgTypeInt, Required: true}},
		Handler: &captureHandler{},
	})); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := c.Invoke(context.Background(), "billing.set", map[string]any{"ttl": "not-an-int"}, ActorModel); err == nil {
		t.Fatal("Invoke with a string for an int arg should error, not pass through")
	}
	if _, err := c.Invoke(context.Background(), "billing.set", map[string]any{"ttl": float64(5)}, ActorModel); err != nil {
		t.Fatalf("Invoke with float64(5) for an int arg should succeed: %v", err)
	}
}

// TestInvokeAndSchemaHonorEnum pins the round-23 Enum wiring: an enum-constrained
// string arg rejects out-of-range values at dispatch (not handed to the Handler),
// and the JSON Schema advertises the allowed values so the model gets guidance.
func TestInvokeAndSchemaHonorEnum(t *testing.T) {
	c := NewCatalog()
	h := &captureHandler{}
	if err := c.Add(NewOperation(OperationSpec{
		Name: "dns.add", Title: "Add", Summary: "add dns record",
		Category: "dns", Safety: SafetyMutate,
		Interaction: InteractionAgentSafe, Visibility: VisibilityModel,
		Args: []OperationArg{
			{Name: "type", Type: ArgTypeString, Required: true, Enum: []string{"A", "MX", "TXT"}, Help: "record type"},
		},
		Handler: h,
	})); err != nil {
		t.Fatalf("Add: %v", err)
	}
	// Out-of-range enum value is rejected before the Handler runs.
	if _, err := c.Invoke(context.Background(), "dns.add", map[string]any{"type": "ZZZ"}, ActorModel); err == nil {
		t.Fatal("Invoke with out-of-enum value should error")
	}
	// In-range value passes and reaches the Handler.
	if _, err := c.Invoke(context.Background(), "dns.add", map[string]any{"type": "MX"}, ActorModel); err != nil {
		t.Fatalf("Invoke with in-enum value should succeed: %v", err)
	}
	if v, ok := h.got["type"].(string); !ok || v != "MX" {
		t.Fatalf("in-enum value not passed through, got %#v", h.got["type"])
	}
	// The schema advertises the enum array.
	raw, err := NewMCPCompiler().Compile(c)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	td := toolNamed(t, raw, "dns.add")
	var sch map[string]any
	if err := json.Unmarshal(td.InputSchema, &sch); err != nil {
		t.Fatalf("InputSchema not valid JSON: %v", err)
	}
	props, _ := sch["properties"].(map[string]any)
	p, _ := props["type"].(map[string]any)
	enum, ok := p["enum"].([]any)
	if !ok || len(enum) != 3 {
		t.Fatalf("schema enum missing/wrong, got %#v", p["enum"])
	}
	if p["description"] != "record type" {
		t.Fatalf("schema description wrong, got %#v (AgentHelp should fall back to Help)", p["description"])
	}
}

// TestInvokeVisibilityBoundarySymmetric pins the round-24 fix: Invoke's
// visibility gate must mirror Search/Describe on BOTH sides. The old gate only
// refused app-only ops for a model, letting an ActorApp/ActorHuman execute a
// model-only op that is invisible on their app surface — a discovery/dispatch
// mismatch. The symmetric gate (visibleTo(op.Visibility(), actorVisibility(actor)))
// refuses whichever surface the op is excluded from.
func TestInvokeVisibilityBoundarySymmetric(t *testing.T) {
	c := NewCatalog()
	modelOnly := &captureHandler{}
	appOnly := &captureHandler{}
	both := &captureHandler{}
	for _, op := range []Operation{
		NewOperation(OperationSpec{Name: "m.only", Title: "M", Summary: "model only",
			Category: "x", Safety: SafetyRead, Interaction: InteractionAgentSafe,
			Visibility: VisibilityModel, Handler: modelOnly}),
		NewOperation(OperationSpec{Name: "a.only", Title: "A", Summary: "app only",
			Category: "x", Safety: SafetyRead, Interaction: InteractionAgentSafe,
			Visibility: VisibilityAppOnly, Handler: appOnly}),
		NewOperation(OperationSpec{Name: "b.both", Title: "B", Summary: "both",
			Category: "x", Safety: SafetyRead, Interaction: InteractionAgentSafe,
			Visibility: VisibilityBoth, Handler: both}),
	} {
		if err := c.Add(op); err != nil {
			t.Fatalf("Add: %v", err)
		}
	}
	empty := func() map[string]any { return map[string]any{} }
	// App-only is invisible to the model surface -> both refuse and search agree.
	if _, err := c.Invoke(context.Background(), "a.only", empty(), ActorModel); err == nil {
		t.Fatal("model must not invoke an app-only op")
	}
	if app := c.Search("a.only", "", VisibilityModel); len(app) != 0 {
		t.Fatal("app-only op must not appear on the model search surface")
	}
	// Model-only is invisible to the app/human surface -> refused for both (THE
	// asymmetric gap this fixes), and absent from the app search surface.
	if _, err := c.Invoke(context.Background(), "m.only", empty(), ActorApp); err == nil {
		t.Fatal("app actor must not invoke a model-only op")
	}
	if _, err := c.Invoke(context.Background(), "m.only", empty(), ActorHuman); err == nil {
		t.Fatal("human actor must not invoke a model-only op")
	}
	if m := c.Search("m.only", "", VisibilityAppOnly); len(m) != 0 {
		t.Fatal("model-only op must not appear on the app search surface")
	}
	// Both-visible runs for every actor.
	for _, actor := range []Actor{ActorModel, ActorApp, ActorHuman} {
		if _, err := c.Invoke(context.Background(), "b.both", empty(), actor); err != nil {
			t.Fatalf("both-visible op refused for %s: %v", actor, err)
		}
	}
	// Model-only still runs for the model actor.
	if _, err := c.Invoke(context.Background(), "m.only", empty(), ActorModel); err != nil {
		t.Fatalf("model-only op refused for model actor: %v", err)
	}
}

// TestAddRejectsBadEnumConfig pins the round-23 validation: an Enum declared on a
// non-string arg, or a Default that is not a member of the Enum, is a config bug
// rejected at Add.
func TestAddRejectsBadEnumConfig(t *testing.T) {
	c := NewCatalog()
	if err := c.Add(NewOperation(OperationSpec{
		Name: "bad", Title: "Bad", Summary: "bad enum",
		Category: "cat", Safety: SafetyMutate,
		Interaction: InteractionAgentSafe, Visibility: VisibilityModel,
		Args:    []OperationArg{{Name: "n", Type: ArgTypeInt, Enum: []string{"1", "2"}}},
		Handler: &captureHandler{},
	})); err == nil {
		t.Fatal("Add with Enum on a non-string arg should error")
	}
	c2 := NewCatalog()
	if err := c2.Add(NewOperation(OperationSpec{
		Name: "bad2", Title: "Bad2", Summary: "bad enum default",
		Category: "cat", Safety: SafetyMutate,
		Interaction: InteractionAgentSafe, Visibility: VisibilityModel,
		Args:    []OperationArg{{Name: "t", Type: ArgTypeString, Enum: []string{"A", "B"}, Default: "C"}},
		Handler: &captureHandler{},
	})); err == nil {
		t.Fatal("Add with a Default outside the Enum should error")
	}
}

// --- helpers ---

func names(ops []Operation) []string {
	out := make([]string, 0, len(ops))
	for _, o := range ops {
		out = append(out, o.Name())
	}
	return out
}

func containsOp(ops []Operation, name string) bool {
	for _, o := range ops {
		if o.Name() == name {
			return true
		}
	}
	return false
}

// TestNullableBoolTriState pins the core contract of ArgTypeNullableBool: the
// Handler input preserves absent, true, and false as three distinct states
// (nil / &true / &false) through Invoke's normalization, unlike ArgTypeBool
// which collapses absence to false.
func TestNullableBoolTriState(t *testing.T) {
	for _, tc := range []struct {
		name  string
		input map[string]any
		want  *bool // nil == unset
	}{
		{"absent stays nil", map[string]any{}, nil},
		{"json null stays nil", map[string]any{"flag": nil}, nil},
		{"explicit true", map[string]any{"flag": true}, ptr(true)},
		{"explicit false", map[string]any{"flag": false}, ptr(false)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := &captureHandler{}
			c := NewCatalog()
			if err := c.Add(NewOperation(OperationSpec{
				Name: "tristate", Title: "T", Summary: "t",
				Category: "c", Safety: SafetyMutate,
				Interaction: InteractionAgentSafe, Visibility: VisibilityModel,
				Args:    []OperationArg{{Name: "flag", Type: ArgTypeNullableBool}},
				Handler: h,
			})); err != nil {
				t.Fatalf("Add: %v", err)
			}
			if _, err := c.Invoke(context.Background(), "tristate", tc.input, ActorModel); err != nil {
				t.Fatalf("Invoke: %v", err)
			}
			got, ok := h.got["flag"].(*bool)
			if !ok {
				t.Fatalf("handler flag type = %T, want *bool (got %#v)", h.got["flag"], h.got["flag"])
			}
			switch {
			case tc.want == nil && got != nil:
				t.Fatalf("want nil, got <&%v>", *got)
			case tc.want != nil && got == nil:
				t.Fatalf("want <&%v>, got nil", *tc.want)
			case tc.want != nil && *got != *tc.want:
				t.Fatalf("want <&%v>, got <&%v>", *tc.want, *got)
			}
		})
	}

	// BoolArgPtr must surface the same tri-state from a normalized input map.
	h := &captureHandler{}
	c := NewCatalog()
	if err := c.Add(NewOperation(OperationSpec{
		Name: "tristate2", Title: "T", Summary: "t",
		Category: "c", Safety: SafetyMutate,
		Interaction: InteractionAgentSafe, Visibility: VisibilityModel,
		Args:    []OperationArg{{Name: "flag", Type: ArgTypeNullableBool}},
		Handler: h,
	})); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := c.Invoke(context.Background(), "tristate2", map[string]any{"flag": true}, ActorModel); err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if got := BoolArgPtr(h.got, "flag"); got == nil || !*got {
		t.Fatalf("BoolArgPtr(true) = %v, want &true", got)
	}
	if got := BoolArgPtr(map[string]any{}, "flag"); got != nil {
		t.Fatalf("BoolArgPtr(absent) = %v, want nil", got)
	}
}

// TestNullableBoolSchema verifies the nullable-bool arg emits a schema type of
// ["boolean","null"] and stays out of the required set (it is optional).
func TestNullableBoolSchema(t *testing.T) {
	args := []OperationArg{
		{Name: "hosting", Type: ArgTypeNullableBool},
		{Name: "name", Type: ArgTypeString, Required: true},
	}
	raw := inputSchemaFromArgs("n", args)
	var sch struct {
		Type       string                     `json:"type"`
		Required   []string                   `json:"required"`
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(raw, &sch); err != nil {
		t.Fatalf("unmarshal schema: %v", err)
	}
	var prop struct {
		Type []string `json:"type"`
	}
	if err := json.Unmarshal(sch.Properties["hosting"], &prop); err != nil {
		t.Fatalf("unmarshal hosting prop: %v", err)
	}
	if len(prop.Type) != 2 || prop.Type[0] != "boolean" || prop.Type[1] != "null" {
		t.Fatalf("hosting type = %v, want [boolean null]", prop.Type)
	}
	for _, r := range sch.Required {
		if r == "hosting" {
			t.Fatalf("nullable bool must not be required")
		}
	}
}

func ptr(b bool) *bool { return &b }
