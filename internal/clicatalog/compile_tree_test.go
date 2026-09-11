package clicatalog

import (
	"reflect"
	"strings"
	"testing"

	opmesh "go.lumeweb.com/opmesh"
)

// testNode is a neutral frontend node used by the transform tests. It stands in
// for *cli.Command without importing internal/cli (neutrality constraint).
type testNode struct {
	name     string
	category string
	usage    string
	aliases  []string
	isLeaf   bool
	leaf     LeafLocator
	own      *testNode // folded-in leaf, when present
	children []testNode
}

func testLeafBuilder(loc LeafLocator, cfg any) (testNode, error) {
	return testNode{name: loc.Name, isLeaf: true, leaf: loc}, nil
}

func testParentBuilder(spec NodeSpec, own testNode, children []testNode) (testNode, error) {
	n := testNode{name: spec.Name, category: spec.Category, usage: spec.Usage, aliases: spec.Aliases, children: children}
	if own.name != "" {
		o := own
		n.own = &o
	}
	return n, nil
}

// shapeOp builds a stub opmesh operation by name (no args/handler needed — the
// transform works purely on shape data).
func shapeOp(name string) opmesh.Operation {
	return opmesh.NewOperation(opmesh.OperationSpec{
		Name: name, Title: name, Summary: name, Category: "test",
		Safety: opmesh.SafetyRead, Interaction: opmesh.InteractionAgentSafe, Visibility: opmesh.VisibilityBoth,
	})
}

// renderTree serializes materialized testNodes into a path string, e.g.
// "keys>list,create" , so tests can assert exact nesting/order.
func renderTree(nodes []testNode) string {
	var b strings.Builder
	for i, n := range nodes {
		if i > 0 {
			b.WriteString(" | ")
		}
		writeNode(&b, &n)
	}
	return b.String()
}

func writeNode(b *strings.Builder, n *testNode) {
	b.WriteString(n.name)
	if len(n.children) > 0 {
		b.WriteString(">")
		for i, c := range n.children {
			if i > 0 {
				b.WriteString(",")
			}
			writeNode(b, &c)
		}
	}
}

func compileTestTree(t *testing.T, ops []opmesh.Operation, reg ShapeRegistry, root DomainRoot) ([]testNode, error) {
	t.Helper()
	return CompileCommandTree(ops, reg, root, nil, testLeafBuilder, testParentBuilder)
}

// TestCompileTree_FlatDefault asserts ops with no registry entry render flat
// under the root with hyphenated leaf names, in declaration order.
func TestCompileTree_FlatDefault(t *testing.T) {
	ops := []opmesh.Operation{
		shapeOp("dom_thing_one"),
		shapeOp("dom_thing_two"),
	}
	nodes, err := compileTestTree(t, ops, ShapeRegistry{}, DomainRoot{Name: "dom", Category: "Mgmt"})
	if err != nil {
		t.Fatalf("CompileCommandTree: %v", err)
	}
	got := renderTree(nodes)
	if got != "thing-one | thing-two" {
		t.Fatalf("renderTree = %q, want %q", got, "thing-one | thing-two")
	}
}

// TestCompileTree_Nesting asserts parent creation, ordering, category/usage and
// deep nesting from declared Segments.
func TestCompileTree_Nesting(t *testing.T) {
	keys := Segment{Name: "keys", Category: "Mgmt", Usage: "Manage keys", Order: 1}
	reg := ShapeRegistry{
		"a_keys_list":   {Path: []string{"a", "keys", "list"}, Segments: map[int]Segment{1: keys}},
		"a_keys_create": {Path: []string{"a", "keys", "create"}, Segments: map[int]Segment{1: keys}},
		"a_publish":     {Path: []string{"a", "publish"}},
	}
	nodes, err := compileTestTree(t,
		[]opmesh.Operation{shapeOp("a_keys_list"), shapeOp("a_keys_create"), shapeOp("a_publish")},
		reg, DomainRoot{Name: "a", Category: "Mgmt", Usage: "Manage a"})
	if err != nil {
		t.Fatalf("CompileCommandTree: %v", err)
	}
	got := renderTree(nodes)
	if got != "publish | keys>list,create" {
		t.Fatalf("renderTree = %q, want %q", got, "publish | keys>list,create")
	}
}

// TestCompileTree_DeepNesting exercises the one-Path-vehicle for depth (3+
// levels) and the exact dane/republish path shape.
func TestCompileTree_DeepNesting(t *testing.T) {
	reg := ShapeRegistry{
		"w_domains_dane_republish": {
			Path: []string{"w", "domains", "dane", "republish"},
			Segments: map[int]Segment{
				1: {Name: "domains", Category: "Mgmt", Usage: "Manage domains"},
				2: {Name: "dane", Category: "Mgmt", Usage: "Manage dane"},
			},
		},
	}
	nodes, err := compileTestTree(t,
		[]opmesh.Operation{shapeOp("w_domains_dane_republish")},
		reg, DomainRoot{Name: "w", Category: "Mgmt"})
	if err != nil {
		t.Fatalf("CompileCommandTree: %v", err)
	}
	if got := renderTree(nodes); got != "domains>dane>republish" {
		t.Fatalf("renderTree = %q, want %q", got, "domains>dane>republish")
	}
	// The leaf's category should inherit from the deepest parent.
	leaf := nodes[0].children[0].children[0]
	if leaf.leaf.Category != "Mgmt" || leaf.leaf.Name != "republish" {
		t.Fatalf("leaf = %+v", leaf.leaf)
	}
}

// TestCompileTree_NameOverrideAndHyphen asserts KeepUnderscores and primary Name
// override behavior.
func TestCompileTree_NameOverrideAndHyphen(t *testing.T) {
	reg := ShapeRegistry{
		"x_keep_under": {Path: []string{"x", "keep_under"}, KeepUnderscores: true},
		"x_renamed":    {Path: []string{"x", "renamed"}, Name: "renamed-primary"},
		"x_hyphen":     {Path: []string{"x", "hyphen_leaf"}},
		"x_alias":      {Path: []string{"x", "alias"}, Aliases: []string{"rm"}},
	}
	nodes, err := compileTestTree(t,
		[]opmesh.Operation{shapeOp("x_keep_under"), shapeOp("x_renamed"), shapeOp("x_hyphen"), shapeOp("x_alias")},
		reg, DomainRoot{Name: "x", Category: "Mgmt"})
	if err != nil {
		t.Fatalf("CompileCommandTree: %v", err)
	}
	byName := map[string]testNode{}
	for _, n := range nodes {
		byName[n.name] = n
	}
	if byName["keep_under"].name != "keep_under" {
		t.Errorf("KeepUnderscores leaf should stay underscore, got %q", byName["keep_under"].name)
	}
	if byName["renamed-primary"].name != "renamed-primary" {
		t.Errorf("Name override not applied, got %q", byName["renamed-primary"].name)
	}
	if byName["hyphen-leaf"].name != "hyphen-leaf" {
		t.Errorf("default hyphenation not applied, got %q", byName["hyphen-leaf"].name)
	}
	if got := byName["alias"].leaf.Aliases; !reflect.DeepEqual(got, []string{"rm"}) {
		t.Errorf("leaf aliases = %v, want [rm]", got)
	}
}

// TestCompileTree_Exclusion asserts ExcludeFromFlatMount drops an op entirely.
func TestCompileTree_Exclusion(t *testing.T) {
	reg := ShapeRegistry{
		"acct_otp_disable": {Path: []string{"acct", "otp", "disable"}, ExcludeFromFlatMount: true},
		"acct_update":      {Path: []string{"acct", "update-email"}},
	}
	nodes, err := compileTestTree(t,
		[]opmesh.Operation{shapeOp("acct_otp_disable"), shapeOp("acct_update")},
		reg, DomainRoot{Name: "acct", Category: "Mgmt"})
	if err != nil {
		t.Fatalf("CompileCommandTree: %v", err)
	}
	if got := renderTree(nodes); got != "update-email" {
		t.Fatalf("renderTree = %q, want only update-email (otp excluded)", got)
	}
}

// TestCompileTree_Fold asserts FoldFlat moves a flat leaf into a sibling parent
// as its top-level Action, and errors when no matching parent exists.
func TestCompileTree_Fold(t *testing.T) {
	share := Segment{Name: "share", Category: "Mgmt", Usage: "Share items"}
	reg := ShapeRegistry{
		"vault_share":        {Path: []string{"vault", "share"}, FoldFlat: true},
		"vault_share_accept": {Path: []string{"vault", "share", "accept"}, Segments: map[int]Segment{1: share}},
	}
	nodes, err := compileTestTree(t,
		[]opmesh.Operation{shapeOp("vault_share"), shapeOp("vault_share_accept")},
		reg, DomainRoot{Name: "vault", Category: "Mgmt"})
	if err != nil {
		t.Fatalf("CompileCommandTree: %v", err)
	}
	if len(nodes) != 1 || nodes[0].name != "share" {
		t.Fatalf("expected single folded 'share' parent, got %q", renderTree(nodes))
	}
	// The folded-in flat leaf is the parent's own leaf.
	parent := nodes[0]
	if parent.own == nil || parent.own.name != "share" {
		t.Fatalf("parent.own = %+v, want folded flat leaf", parent.own)
	}
	if len(parent.children) != 1 || parent.children[0].name != "accept" {
		t.Fatalf("parent children = %q, want [accept]", renderTree([]testNode{parent}))
	}

	// Malformed: FoldFlat with no matching parent must error.
	bad := ShapeRegistry{
		"vault_share": {Path: []string{"vault", "share"}, FoldFlat: true},
	}
	if _, err := compileTestTree(t, []opmesh.Operation{shapeOp("vault_share")}, bad, DomainRoot{Name: "vault"}); err == nil {
		t.Fatal("FoldFlat with no matching parent should error")
	}
}

// TestCompileTree_OrderingDeterminism asserts repeated compiles yield identical
// trees (map-order independence) and that explicit Order reorders siblings.
func TestCompileTree_OrderingDeterminism(t *testing.T) {
	ops := []opmesh.Operation{shapeOp("d_b"), shapeOp("d_a"), shapeOp("d_c")}
	reg := ShapeRegistry{
		"d_b": {Path: []string{"d", "b"}},
		"d_a": {Path: []string{"d", "a"}},
		"d_c": {Path: []string{"d", "c"}},
	}
	// Declared order b,a,c (ties keep declaration order).
	first, err := compileTestTree(t, ops, reg, DomainRoot{Name: "d"})
	if err != nil {
		t.Fatalf("CompileCommandTree: %v", err)
	}
	if got := renderTree(first); got != "b | a | c" {
		t.Fatalf("declaration-order render = %q, want %q", got, "b | a | c")
	}
	// Re-running the same input yields the identical tree (deterministic).
	for i := 0; i < 5; i++ {
		again, err := compileTestTree(t, ops, reg, DomainRoot{Name: "d"})
		if err != nil {
			t.Fatalf("CompileCommandTree: %v", err)
		}
		if got := renderTree(again); got != renderTree(first) {
			t.Fatalf("non-deterministic tree: run %d = %q, first = %q", i, got, renderTree(first))
		}
	}

	// Explicit Order reorders.
	reg["d_a"] = OpShape{Path: []string{"d", "a"}, Order: -1}
	reg["d_b"] = OpShape{Path: []string{"d", "b"}, Order: 5}
	ordered, _ := compileTestTree(t, ops, reg, DomainRoot{Name: "d"})
	if got := renderTree(ordered); got != "a | c | b" {
		t.Fatalf("ordered render = %q, want %q", got, "a | c | b")
	}
}

// TestCompileTree_PathMustStartWithRoot asserts a shape whose Path[0] does not
// equal the declared domain root is a compile error.
func TestCompileTree_PathMustStartWithRoot(t *testing.T) {
	reg := ShapeRegistry{
		"x_thing": {Path: []string{"wrongroot", "thing"}},
	}
	if _, err := compileTestTree(t, []opmesh.Operation{shapeOp("x_thing")}, reg, DomainRoot{Name: "x"}); err == nil {
		t.Fatal("path not starting at root should error")
	}
}

// TestIPNSShapes_SelfConsistent pins the registry self-consistency contract:
// every key underscore-separated, Path[0] == the domain root, no dotted key,
// and every Segments index within the path.
func TestIPNSShapes_SelfConsistent(t *testing.T) {
	root := "ipns"
	for name, shape := range IPNSShapes {
		if strings.Contains(name, ".") {
			t.Errorf("registry key %q is dotted (must be underscore)", name)
		}
		if len(shape.Path) == 0 {
			t.Errorf("%s: empty Path", name)
			continue
		}
		if shape.Path[0] != root {
			t.Errorf("%s: Path[0] = %q, want domain root %q", name, shape.Path[0], root)
		}
		for idx := range shape.Segments {
			if idx <= 0 || idx >= len(shape.Path) {
				t.Errorf("%s: Segments index %d out of range for Path len %d", name, idx, len(shape.Path))
			}
		}
	}
	// Every IPNS op is covered by the registry.
	want := []string{
		"ipns_keys_list", "ipns_keys_create", "ipns_keys_get", "ipns_keys_delete",
		"ipns_publish", "ipns_republish", "ipns_resolve",
	}
	for _, name := range want {
		if _, ok := IPNSShapes[name]; !ok {
			t.Errorf("IPNSShapes missing declaration for %q", name)
		}
	}
}

// TestCompileTree_IPNSShapeRendersFlatAndNested compiles the real IPNS shape
// registry with stub ops and asserts the exact tree the CLI must produce.
func TestCompileTree_IPNSShapeRendersFlatAndNested(t *testing.T) {
	ops := []opmesh.Operation{}
	for _, name := range []string{
		"ipns_keys_list", "ipns_keys_create", "ipns_keys_get", "ipns_keys_delete",
		"ipns_publish", "ipns_republish", "ipns_resolve",
	} {
		ops = append(ops, shapeOp(name))
	}
	nodes, err := compileTestTree(t, ops, IPNSShapes, IPNSDomainRoot)
	if err != nil {
		t.Fatalf("CompileCommandTree: %v", err)
	}
	// keys parent first (declaration order), then the three flat leaves.
	if got := renderTree(nodes); got != "keys>list,create,get,delete | publish | republish | resolve" {
		t.Fatalf("renderTree = %q", got)
	}
	keys := nodes[0]
	if keys.name != "keys" || keys.isLeaf {
		t.Fatalf("keys should be a parent node, got %+v", keys)
	}
	if got := keys.children[0].name; got != "list" {
		t.Fatalf("first keys child = %q, want list", got)
	}
}

// TestDNSShapes_SelfConsistent pins the DNS registry self-consistency
// contract: every key underscore-separated, Path[0] == the domain root, no
// dotted key, every Segments index within the path, and full coverage of the
// DNS op set.
func TestDNSShapes_SelfConsistent(t *testing.T) {
	root := "dns"
	for name, shape := range DNSShapes {
		if strings.Contains(name, ".") {
			t.Errorf("registry key %q is dotted (must be underscore)", name)
		}
		if len(shape.Path) == 0 {
			t.Errorf("%s: empty Path", name)
			continue
		}
		if shape.Path[0] != root {
			t.Errorf("%s: Path[0] = %q, want domain root %q", name, shape.Path[0], root)
		}
		for idx := range shape.Segments {
			if idx <= 0 || idx >= len(shape.Path) {
				t.Errorf("%s: Segments index %d out of range for Path len %d", name, idx, len(shape.Path))
			}
		}
	}
	// Every DNS op is covered by the registry.
	want := []string{
		"dns_zones_list", "dns_zones_create", "dns_zones_get", "dns_zones_delete", "dns_zones_validate",
		"dns_records_list", "dns_records_create", "dns_records_get", "dns_records_update", "dns_records_delete",
	}
	for _, name := range want {
		if _, ok := DNSShapes[name]; !ok {
			t.Errorf("DNSShapes missing declaration for %q", name)
		}
	}
}

// TestCompileTree_DNSShapeRendersNested compiles the real DNS shape registry
// with stub ops and asserts the exact two-parent (zones, records) tree the CLI
// must produce, in zones-then-records order with each parent's leaves in
// declaration order.
func TestCompileTree_DNSShapeRendersNested(t *testing.T) {
	ops := []opmesh.Operation{}
	for _, name := range []string{
		"dns_zones_list", "dns_zones_create", "dns_zones_get", "dns_zones_delete", "dns_zones_validate",
		"dns_records_list", "dns_records_create", "dns_records_get", "dns_records_update", "dns_records_delete",
	} {
		ops = append(ops, shapeOp(name))
	}
	nodes, err := compileTestTree(t, ops, DNSShapes, DNSDomainRoot)
	if err != nil {
		t.Fatalf("CompileCommandTree: %v", err)
	}
	want := "zones>list,create,get,delete,validate | records>list,create,get,update,delete"
	if got := renderTree(nodes); got != want {
		t.Fatalf("renderTree = %q, want %q", got, want)
	}
	if nodes[0].name != "zones" || nodes[0].isLeaf {
		t.Fatalf("zones should be a parent node, got %+v", nodes[0])
	}
	// The leaf inherits its parent segment's category (DNS).
	if nodes[0].children[0].isLeaf {
		if got := nodes[0].children[0].leaf.Category; got != "DNS" {
			t.Fatalf("zones list leaf category = %q, want DNS", got)
		}
	}
	if got := nodes[0].children[0].name; got != "list" {
		t.Fatalf("first zones child = %q, want list", got)
	}
}

// TestPinsShapes_SelfConsistent pins the pins registry self-consistency
// contract: every key underscore-separated, Path[0] == the domain root, no
// dotted key, every Segments index within the path, and full coverage of the
// pins op set.
func TestPinsShapes_SelfConsistent(t *testing.T) {
	root := "pins"
	for name, shape := range PinsShapes {
		if strings.Contains(name, ".") {
			t.Errorf("registry key %q is dotted (must be underscore)", name)
		}
		if len(shape.Path) == 0 {
			t.Errorf("%s: empty Path", name)
			continue
		}
		if shape.Path[0] != root {
			t.Errorf("%s: Path[0] = %q, want domain root %q", name, shape.Path[0], root)
		}
		for idx := range shape.Segments {
			if idx <= 0 || idx >= len(shape.Path) {
				t.Errorf("%s: Segments index %d out of range for Path len %d", name, idx, len(shape.Path))
			}
		}
	}
	// Every pins op is covered by the registry.
	want := []string{
		"pins_list", "pins_add", "pins_rm", "pins_status", "pins_update",
	}
	for _, name := range want {
		if _, ok := PinsShapes[name]; !ok {
			t.Errorf("PinsShapes missing declaration for %q", name)
		}
	}
}

// TestCompileTree_PinsShapeRendersFlat compiles the real pins shape registry
// with stub ops and asserts the exact flat tree the CLI must produce, in
// PinsOperations declaration order, with the "list" leaf carrying the "ls"
// alias.
func TestCompileTree_PinsShapeRendersFlat(t *testing.T) {
	ops := []opmesh.Operation{}
	for _, name := range []string{
		"pins_list", "pins_add", "pins_rm", "pins_status", "pins_update",
	} {
		ops = append(ops, shapeOp(name))
	}
	nodes, err := compileTestTree(t, ops, PinsShapes, PinsDomainRoot)
	if err != nil {
		t.Fatalf("CompileCommandTree: %v", err)
	}
	want := "list | add | rm | status | update"
	if got := renderTree(nodes); got != want {
		t.Fatalf("renderTree = %q, want %q", got, want)
	}
	byName := map[string]testNode{}
	for _, n := range nodes {
		byName[n.name] = n
	}
	if !byName["list"].isLeaf {
		t.Fatalf("list should be a leaf node")
	}
	// The ideal naming: canonical "list" carries the "ls" alias.
	if got := byName["list"].leaf.Aliases; !reflect.DeepEqual(got, []string{"ls"}) {
		t.Fatalf("list aliases = %v, want [ls]", got)
	}
	// rm keeps its canonical op leaf name (pins_rm) with no alias.
	if got := byName["rm"].leaf.Aliases; len(got) != 0 {
		t.Fatalf("rm aliases = %v, want none", got)
	}
	// Every flat leaf inherits the root category (Pinning).
	if got := byName["add"].leaf.Category; got != "Pinning" {
		t.Fatalf("add leaf category = %q, want Pinning", got)
	}
}

// TestWebsitesShapes_SelfConsistent pins the websites registry
// self-consistency contract: every key underscore-separated, Path[0] == the
// domain root, no dotted key, every Segments index within the path, and full
// coverage of the websites op set.
func TestWebsitesShapes_SelfConsistent(t *testing.T) {
	root := "websites"
	for name, shape := range WebsitesShapes {
		if strings.Contains(name, ".") {
			t.Errorf("registry key %q is dotted (must be underscore)", name)
		}
		if len(shape.Path) == 0 {
			t.Errorf("%s: empty Path", name)
			continue
		}
		if shape.Path[0] != root {
			t.Errorf("%s: Path[0] = %q, want domain root %q", name, shape.Path[0], root)
		}
		for idx := range shape.Segments {
			if idx <= 0 || idx >= len(shape.Path) {
				t.Errorf("%s: Segments index %d out of range for Path len %d", name, idx, len(shape.Path))
			}
		}
	}
	// Every websites op is covered by the registry.
	want := []string{
		"websites_list", "websites_get", "websites_create", "websites_update",
		"websites_enable_ipns", "websites_delete", "websites_validate",
		"websites_ssl_status", "websites_config",
		"websites_domains_list", "websites_domains_add", "websites_domains_remove",
		"websites_domains_verify", "websites_domains_dns_requirements",
		"websites_domains_dane_republish", "websites_domains_convert_onchain",
		"websites_domains_update",
		"websites_platform_domains_list", "websites_platform_domain_availability",
	}
	for _, name := range want {
		if _, ok := WebsitesShapes[name]; !ok {
			t.Errorf("WebsitesShapes missing declaration for %q", name)
		}
	}
}

// TestCompileTree_WebsitesShapeRenders compiles the real websites shape
// registry with stub ops and asserts the exact tree the CLI must produce, in
// WebsitesOperations declaration order — flat leaves, the ssl/domains/
// platform-domain synthesized parents (with declared aliases), and the
// behavior-preserving naming (enable-ipns canonical with "ipns" alias, domains
// list/remove canonical with "ls"/"rm" aliases, dane nested 3 levels deep).
func TestCompileTree_WebsitesShapeRenders(t *testing.T) {
	ops := []opmesh.Operation{}
	for _, name := range []string{
		"websites_list", "websites_get", "websites_create", "websites_update",
		"websites_enable_ipns", "websites_delete", "websites_validate",
		"websites_ssl_status", "websites_config",
		"websites_domains_list", "websites_domains_add", "websites_domains_remove",
		"websites_domains_verify", "websites_domains_dns_requirements",
		"websites_domains_dane_republish", "websites_domains_convert_onchain",
		"websites_domains_update",
		"websites_platform_domains_list", "websites_platform_domain_availability",
	} {
		ops = append(ops, shapeOp(name))
	}
	nodes, err := compileTestTree(t, ops, WebsitesShapes, WebsitesDomainRoot)
	if err != nil {
		t.Fatalf("CompileCommandTree: %v", err)
	}
	want := "list | get | create | update | enable-ipns | delete | validate | ssl>status | config | " +
		"domains>list,add,remove,verify,dns-requirements,dane>republish,convert-onchain,update | " +
		"platform-domain>domains-list,domain-availability"
	if got := renderTree(nodes); got != want {
		t.Fatalf("renderTree = %q, want %q", got, want)
	}
	byName := map[string]testNode{}
	for _, n := range nodes {
		byName[n.name] = n
	}
	// enable_ipns renders as the canonical "enable-ipns" leaf with the legacy
	// "ipns" alias.
	enable := byName["enable-ipns"]
	if !enable.isLeaf || !reflect.DeepEqual(enable.leaf.Aliases, []string{"ipns"}) {
		t.Fatalf("enable-ipns leaf/aliases = %v, want leaf with [ipns] alias", enable)
	}
	// Flat leaves inherit the root category (Management).
	if got := byName["list"].leaf.Category; got != "Management" {
		t.Fatalf("list leaf category = %q, want Management", got)
	}
	// The top-level websites list is the canonical `list` leaf with the `ls`
	// alias (ideal naming rule: list canonical, ls a muscle-memory alias),
	// matching domains list and the pins domain.
	if got := byName["list"].leaf.Aliases; !reflect.DeepEqual(got, []string{"ls"}) {
		t.Fatalf("websites list aliases = %v, want [ls]", got)
	}
	// The domains parent exposes its conventional singular "domain" alias.
	domains := byName["domains"]
	if len(domains.children) == 0 {
		t.Fatalf("domains should be a parent with children")
	}
	// domains list/remove keep the "ls"/"rm" aliases on their canonical names.
	justNames := func() map[string][]string {
		out := map[string][]string{}
		var walk func(nodes []testNode)
		walk = func(nodes []testNode) {
			for _, n := range nodes {
				out[n.name] = n.leaf.Aliases
				walk(n.children)
			}
		}
		walk(domains.children)
		return out
	}()
	if got := justNames["list"]; !reflect.DeepEqual(got, []string{"ls"}) {
		t.Fatalf("domains list aliases = %v, want [ls]", got)
	}
	if got := justNames["remove"]; !reflect.DeepEqual(got, []string{"rm"}) {
		t.Fatalf("domains remove aliases = %v, want [rm]", got)
	}
	// platform-domain parent holds the two platform leaves.
	platform := byName["platform-domain"]
	if got := platformChildrenNames(platform); !reflect.DeepEqual(got, []string{"domains-list", "domain-availability"}) {
		t.Fatalf("platform-domain children = %v, want [domains-list domain-availability]", got)
	}
}

func platformChildrenNames(n testNode) []string {
	out := []string{}
	for _, c := range n.children {
		out = append(out, c.name)
	}
	return out
}

// TestVaultShapes_SelfConsistent pins the vault registry self-consistency
// contract: every key underscore-separated, Path[0] == the domain root, no
// dotted key, every Segments index within the path, and full coverage of the
// vault op set.
func TestVaultShapes_SelfConsistent(t *testing.T) {
	root := "vault"
	for name, shape := range VaultShapes {
		if strings.Contains(name, ".") {
			t.Errorf("registry key %q is dotted (must be underscore)", name)
		}
		if len(shape.Path) == 0 {
			t.Errorf("%s: empty Path", name)
			continue
		}
		if shape.Path[0] != root {
			t.Errorf("%s: Path[0] = %q, want domain root %q", name, shape.Path[0], root)
		}
		for idx := range shape.Segments {
			if idx <= 0 || idx >= len(shape.Path) {
				t.Errorf("%s: Segments index %d out of range for Path len %d", name, idx, len(shape.Path))
			}
		}
	}
	// Every vault op is covered by the registry.
	want := []string{
		"vault_status", "vault_ls", "vault_stat", "vault_verify",
		"vault_version_ls", "vault_version_get", "vault_version_restore",
		"vault_search",
		"vault_tag_add", "vault_tag_rm", "vault_tag_set", "vault_tag_ls",
		"vault_rm", "vault_sync",
		"vault_flush", "vault_flush_status",
		"vault_profiles", "vault_share", "vault_share_accept", "vault_send",
		"vault_forget", "vault_profile_use",
		"vault_cache_rebuild", "vault_cache_clear",
	}
	for _, name := range want {
		if _, ok := VaultShapes[name]; !ok {
			t.Errorf("VaultShapes missing declaration for %q", name)
		}
	}
}

// TestCompileTree_VaultShapeRenders compiles the real vault shape registry with
// stub ops and asserts the exact tree the CLI must produce — root children in
// VaultOperations declaration order, the nested version/tag/flush/share/
// profile/cache parents, and the two FoldFlat leaves folded into the flush and
// share parents' top-level Action.
func TestCompileTree_VaultShapeRenders(t *testing.T) {
	ops := []opmesh.Operation{}
	for _, name := range []string{
		"vault_status", "vault_ls", "vault_stat", "vault_verify",
		"vault_version_ls", "vault_version_get", "vault_version_restore",
		"vault_search",
		"vault_tag_add", "vault_tag_rm", "vault_tag_set", "vault_tag_ls",
		"vault_rm", "vault_sync",
		"vault_flush", "vault_flush_status",
		"vault_profiles", "vault_share", "vault_share_accept", "vault_send",
		"vault_forget", "vault_profile_use",
		"vault_cache_rebuild", "vault_cache_clear",
	} {
		ops = append(ops, shapeOp(name))
	}
	nodes, err := compileTestTree(t, ops, VaultShapes, VaultDomainRoot)
	if err != nil {
		t.Fatalf("CompileCommandTree: %v", err)
	}
	want := "status | ls | stat | verify | version>ls,get,restore | search | tag>add,rm,set,ls | rm | sync | flush>status | profiles | share>accept | send | forget | profile>use | cache>rebuild,clear"
	if got := renderTree(nodes); got != want {
		t.Fatalf("renderTree = %q, want %q", got, want)
	}
	byName := map[string]testNode{}
	for _, n := range nodes {
		byName[n.name] = n
	}
	// vault_flush is folded into the flush parent as its top-level Action.
	flush := byName["flush"]
	if flush.isLeaf || flush.own == nil || flush.own.name != "flush" {
		t.Fatalf("flush parent own leaf = %+v, want folded 'flush'", flush.own)
	}
	// vault_share is folded into the share parent as its top-level Action.
	share := byName["share"]
	if share.isLeaf || share.own == nil || share.own.name != "share" {
		t.Fatalf("share parent own leaf = %+v, want folded 'share'", share.own)
	}
	// Parents that only group (no fold) must not carry a folded leaf.
	for _, name := range []string{"version", "tag", "profile", "cache"} {
		if n := byName[name]; n.isLeaf || n.own != nil {
			t.Fatalf("%s parent own leaf = %+v, want nil", name, n.own)
		}
	}
	// Every leaf inherits the root category (Vault).
	if got := byName["status"].leaf.Category; got != "Vault" {
		t.Fatalf("status leaf category = %q, want Vault", got)
	}
	// version children are ls, get, restore in declaration order.
	version := byName["version"]
	if got := platformChildrenNames(version); !reflect.DeepEqual(got, []string{"ls", "get", "restore"}) {
		t.Fatalf("version children = %v, want [ls get restore]", got)
	}
}

// TestAccountShapes_SelfConsistent pins the account registry
// self-consistency contract: every key underscore-separated, Path[0] == the
// domain root, no dotted key, every Segments index within the path, and full
// coverage of the account op set.
func TestAccountShapes_SelfConsistent(t *testing.T) {
	root := "account"
	for name, shape := range AccountShapes {
		if strings.Contains(name, ".") {
			t.Errorf("registry key %q is dotted (must be underscore)", name)
		}
		if len(shape.Path) == 0 {
			t.Errorf("%s: empty Path", name)
			continue
		}
		if shape.Path[0] != root {
			t.Errorf("%s: Path[0] = %q, want domain root %q", name, shape.Path[0], root)
		}
		for idx := range shape.Segments {
			if idx <= 0 || idx >= len(shape.Path) {
				t.Errorf("%s: Segments index %d out of range for Path len %d", name, idx, len(shape.Path))
			}
		}
	}
	// Every account op is covered by the registry.
	want := []string{
		"account_info", "account_update_email", "account_update_password",
		"account_otp_disable", "account_subscription", "account_quota",
	}
	for _, name := range want {
		if _, ok := AccountShapes[name]; !ok {
			t.Errorf("AccountShapes missing declaration for %q", name)
		}
	}
}

// TestCompileTree_AccountShapeRendersFlat compiles the real account shape
// registry with stub ops and asserts the exact flat tree the CLI must produce:
// info, update-email, update-password, subscription, quota in AccountOperations
// declaration order — with account_otp_disable EXCLUDED (it is not emitted as a
// flat `otp-disable` leaf; it nests under the hand-written `otp` parent) — and
// the multi-token leaves hyphenated (update_email -> update-email).
func TestCompileTree_AccountShapeRendersFlat(t *testing.T) {
	ops := []opmesh.Operation{}
	for _, name := range []string{
		"account_info", "account_update_email", "account_update_password",
		"account_otp_disable", "account_subscription", "account_quota",
	} {
		ops = append(ops, shapeOp(name))
	}
	nodes, err := compileTestTree(t, ops, AccountShapes, AccountDomainRoot)
	if err != nil {
		t.Fatalf("CompileCommandTree: %v", err)
	}
	// account_otp_disable never reaches the tree: the flat surface holds
	// exactly the five non-excluded leaves in declaration order.
	want := "info | update-email | update-password | subscription | quota"
	if got := renderTree(nodes); got != want {
		t.Fatalf("renderTree = %q, want %q", got, want)
	}
	byName := map[string]testNode{}
	for _, n := range nodes {
		byName[n.name] = n
	}
	// The multi-token leaves hyphenate their canonical underscore leaf token.
	for _, name := range []string{"info", "update-email", "update-password", "subscription", "quota"} {
		n, ok := byName[name]
		if !ok || !n.isLeaf {
			t.Fatalf("expected leaf %q, got %+v", name, n)
		}
	}
	// No flat presence of the excluded disable op in any form.
	if _, ok := byName["otp-disable"]; ok {
		t.Fatalf("account_otp_disable must not render as a flat 'otp-disable' leaf")
	}
	// Every leaf inherits the root category (Setup).
	if got := byName["info"].leaf.Category; got != "Setup" {
		t.Fatalf("info leaf category = %q, want Setup", got)
	}
}

// adminOpsOrder mirrors the emission order of
// catalogops.AdminOperations (and the CLI registration-test admin section
// layout), used to build stub ops for the admin shape-render test.
var adminOpsOrder = []string{
	"admin_platform_domains_list", "admin_platform_domains_register", "admin_platform_domains_update", "admin_platform_domains_delete", "admin_platform_domains_bind",
	"admin_websites_block", "admin_websites_unblock",
	"admin_social_providers_list", "admin_social_providers_get", "admin_social_providers_create", "admin_social_providers_update", "admin_social_providers_delete", "admin_social_providers_enable", "admin_social_providers_disable",
	"admin_quota_plans_list", "admin_quota_plans_get", "admin_quota_plans_create", "admin_quota_plans_update", "admin_quota_plans_delete", "admin_quota_plans_set_default",
	"admin_quota_allowances_list", "admin_quota_allowances_create", "admin_quota_allowances_update", "admin_quota_allowances_delete",
	"admin_quota_user_configs_list", "admin_quota_user_configs_update", "admin_quota_user_configs_reset",
	"admin_quota_stats", "admin_quota_reconcile", "admin_quota_cleanup",
	"admin_billing_credits_list", "admin_billing_credits_get", "admin_billing_credits_create", "admin_billing_credits_delete", "admin_billing_credits_restore", "admin_billing_credits_purge", "admin_billing_credits_user_balance", "admin_billing_credits_user_deleted_credits",
	"admin_billing_price_lines_list", "admin_billing_price_lines_get", "admin_billing_price_lines_create", "admin_billing_price_lines_update", "admin_billing_price_lines_delete", "admin_billing_price_lines_add_plan", "admin_billing_price_lines_delete_plan", "admin_billing_price_lines_update_plan_position",
	"admin_billing_pricing_plans_list", "admin_billing_pricing_plans_get", "admin_billing_pricing_plans_create", "admin_billing_pricing_plans_update", "admin_billing_pricing_plans_delete", "admin_billing_pricing_plans_sync", "admin_billing_pricing_plans_sync_all",
	"admin_billing_pricing_plan_periods_list", "admin_billing_pricing_plan_periods_get", "admin_billing_pricing_plan_periods_create", "admin_billing_pricing_plan_periods_update", "admin_billing_pricing_plan_periods_delete",
	"admin_billing_subscribers_list", "admin_billing_subscribers_get", "admin_billing_subscribers_list_gateway", "admin_billing_subscribers_list_user", "admin_billing_subscribers_cancel", "admin_billing_subscribers_abort_cancel", "admin_billing_subscribers_change_plan", "admin_billing_subscribers_pause", "admin_billing_subscribers_resume",
	"admin_billing_overview",
}

// findTestNode returns the first child (recursively) named `name`, or nil.
func findTestNode(nodes []testNode, name string) *testNode {
	for i := range nodes {
		if nodes[i].name == name {
			return &nodes[i]
		}
		if len(nodes[i].children) > 0 {
			if got := findTestNode(nodes[i].children, name); got != nil {
				return got
			}
		}
	}
	return nil
}

// childNamesOf returns the immediate child names of a node.
func childNamesOf(n *testNode) []string {
	out := make([]string, 0, len(n.children))
	for i := range n.children {
		out = append(out, n.children[i].name)
	}
	return out
}

// TestAdminShapes_SelfConsistent pins the admin registry self-consistency
// contract: every key underscore-separated, Path[0] == the domain root, no
// dotted key, every Segments index within the path, and full coverage of the
// AdminOperations op set (all ops are section-scoped under the admin root).
func TestAdminShapes_SelfConsistent(t *testing.T) {
	root := "admin"
	for name, shape := range AdminShapes {
		if strings.Contains(name, ".") {
			t.Errorf("registry key %q is dotted (must be underscore)", name)
		}
		if len(shape.Path) == 0 {
			t.Errorf("%s: empty Path", name)
			continue
		}
		if shape.Path[0] != root {
			t.Errorf("%s: Path[0] = %q, want domain root %q", name, shape.Path[0], root)
		}
		// The section token (Path[1]) must be a declared section.
		if len(shape.Path) < 2 {
			t.Errorf("%s: Path must include a section token", name)
			continue
		}
		foundSection := false
		for _, s := range AdminDomainRoot.Sections {
			if shape.Path[1] == s.Name {
				foundSection = true
				break
			}
		}
		if !foundSection {
			t.Errorf("%s: Path[1] = %q is not a declared admin section", name, shape.Path[1])
		}
		for idx := range shape.Segments {
			if idx <= 0 || idx >= len(shape.Path) {
				t.Errorf("%s: Segments index %d out of range for Path len %d", name, idx, len(shape.Path))
			}
		}
	}
	// Every admin op is covered by the registry.
	for _, name := range adminOpsOrder {
		if _, ok := AdminShapes[name]; !ok {
			t.Errorf("AdminShapes missing declaration for %q", name)
		}
	}
}

// TestCompileTree_AdminShapeRendersSections compiles the real admin shape
// registry with stub ops and asserts the sectioned tree the CLI must produce:
// five section parents under the admin root, the quota/billing group parents,
// and the flat section leaves — with every leaf inheriting the Admin category
// and every parent/resolved leaf carrying the history-preserving metadata.
func TestCompileTree_AdminShapeRendersSections(t *testing.T) {
	ops := make([]opmesh.Operation, 0, len(adminOpsOrder))
	for _, name := range adminOpsOrder {
		ops = append(ops, shapeOp(name))
	}
	nodes, err := compileTestTree(t, ops, AdminShapes, AdminDomainRoot)
	if err != nil {
		t.Fatalf("CompileCommandTree: %v", err)
	}

	// The five sections are the root's direct children, in declaration order.
	wantSections := []string{"quota", "billing", "websites", "platform-domains", "social-providers"}
	gotSections := childNamesOf(&testNode{children: nodes})
	if !reflect.DeepEqual(gotSections, wantSections) {
		t.Fatalf("admin sections = %v, want %v", gotSections, wantSections)
	}

	// quota: group parents plans/allowances/user-configs + flat stats/reconcile/cleanup.
	quota := findTestNode(nodes, "quota")
	if quota == nil {
		t.Fatalf("admin root missing 'quota' section")
	}
	for _, want := range []string{"plans", "allowances", "user-configs", "stats", "reconcile", "cleanup"} {
		if findTestNode(nodes, want) == nil {
			t.Fatalf("quota should contain %q", want)
		}
	}
	plans := findTestNode(nodes, "plans")
	if plans == nil || !reflect.DeepEqual(childNamesOf(plans), []string{"list", "get", "create", "update", "delete", "set-default"}) {
		t.Fatalf("quota plans children = %v, want the six plan leaves", childNamesOf(plans))
	}
	if !reflect.DeepEqual(childNamesOf(findTestNode(nodes, "allowances")), []string{"list", "create", "update", "delete"}) {
		t.Fatalf("quota allowances children wrong")
	}
	if !reflect.DeepEqual(childNamesOf(findTestNode(nodes, "user-configs")), []string{"list", "update", "reset"}) {
		t.Fatalf("quota user-configs children wrong")
	}

	// billing: overview flat + five groups.
	billing := findTestNode(nodes, "billing")
	if billing == nil {
		t.Fatalf("admin root missing 'billing' section")
	}
	for _, want := range []string{"overview", "credits", "price-lines", "pricing-plans", "pricing-plan-periods", "subscribers"} {
		if findTestNode(nodes, want) == nil {
			t.Fatalf("billing should contain %q", want)
		}
	}
	if !reflect.DeepEqual(childNamesOf(findTestNode(nodes, "credits")), []string{"list", "get", "create", "delete", "restore", "purge", "user-balance", "user-deleted-credits"}) {
		t.Fatalf("billing credits children wrong")
	}
	if !reflect.DeepEqual(childNamesOf(findTestNode(nodes, "subscribers")), []string{"list", "get", "list-gateway", "list-user", "cancel", "abort-cancel", "change-plan", "pause", "resume"}) {
		t.Fatalf("billing subscribers children wrong")
	}
	if !reflect.DeepEqual(childNamesOf(findTestNode(nodes, "pricing-plans")), []string{"list", "get", "create", "update", "delete", "sync", "sync-all"}) {
		t.Fatalf("billing pricing-plans children wrong")
	}

	// platform-domains / websites / social-providers flat leaves.
	if !reflect.DeepEqual(childNamesOf(findTestNode(nodes, "platform-domains")), []string{"list", "register", "update", "delete", "bind"}) {
		t.Fatalf("platform-domains children wrong")
	}
	if !reflect.DeepEqual(childNamesOf(findTestNode(nodes, "websites")), []string{"block", "unblock"}) {
		t.Fatalf("websites children wrong")
	}
	if !reflect.DeepEqual(childNamesOf(findTestNode(nodes, "social-providers")), []string{"list", "get", "create", "update", "delete", "enable", "disable"}) {
		t.Fatalf("social-providers children wrong")
	}

	// Every leaf inherits the Admin category.
	leaf := findTestNode(nodes, "bind")
	if leaf == nil || !leaf.isLeaf || leaf.leaf.Category != "Admin" {
		t.Fatalf("bind leaf should inherit Admin category, got %+v", leaf)
	}
	// Group parents carry the Admin category and the historical usage.
	if plans == nil || plans.category != "Admin" || plans.usage != "Manage plans (admin)" {
		t.Fatalf("plans group should carry Admin/'Manage plans (admin)', got cat=%q usage=%q", plans.category, plans.usage)
	}
	// Sections carry their historical usage.
	if quota.usage != "Quota management operations" || billing.usage != "Billing management operations" {
		t.Fatalf("section usages wrong: quota=%q billing=%q", quota.usage, billing.usage)
	}
}
