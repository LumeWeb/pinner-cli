package clicatalog

import (
	"fmt"
	"sort"
	"strings"

	opmesh "go.lumeweb.com/opmesh"
)

// shape.go implements the declarative command-shape model. It moves naming,
// nesting, alias, ordering, category and usage derivation out of the per-domain
// CLI wiring mounts (internal/cli/*_wiring.go) and into the catalog compiler,
// driven by plain data declarations (a ShapeRegistry + DomainRoot) instead of
// ad-hoc switch/inference blocks.
//
// Constraints honored here:
//   - internal/clicatalog never imports internal/cli. The transform is generic
//     over the frontend node type `T` (CLI: *cli.Command); the concrete
//     CatalogAdapterConfig reachable only through a mount-owned builder closure.
//   - Shape is presentation-layer metadata. It never mutates an opmesh
//     Operation, never names a flag/gate/positional/renderer, and can be
//     consumed by any frontend (CLI, hosted, MCP-relevant subsets).
//   - The canonical op Name is the all-underscore, group-prefixed operation ID
//     (e.g. "ipns_keys_list"). There is no dotted-name convention.

// Segment declaratively describes ONE synthesized parent (or section) in the
// command tree. It is presentation metadata only.
type Segment struct {
	Name     string   // display segment; e.g. "domain", "keys", "platform-domain"
	Category string   // e.g. "Management", "DNS", "Vault" (empty => inherit)
	Usage    string   // the parent's help Usage line (empty => derived "Manage <Name>")
	Desc     string   // optional Description on the parent
	Aliases  []string // parent aliases; e.g. "domain" for the "domains" parent
	Order    int      // ascending sort among siblings (ties keep declaration order)
}

// OpShape declaratively describes how ONE catalog operation renders in a
// command tree. It never mutates the opmesh Operation.
type OpShape struct {
	// Path is the full tree path INCLUDING the domain root as Path[0]. Segment
	// separators are the underscore tokens of the canonical Name:
	//   ipns_keys_list                   -> {"ipns","keys","list"}
	//   websites_domains_dane_republish  -> {"websites","domains","dane","republish"}
	// For each intermediate index, the matching Segment (from Segments)
	// supplies category/usage/desc/order/aliases; a missing intermediate entry
	// falls back to the default parent derivation.
	Path []string

	// Segments supplies explicit metadata for intermediate Path indices.
	Segments map[int]Segment

	// Name is the PRIMARY display name of the leaf. When empty it is derived:
	// the last Path token with '_'->'-' (leaf hyphenation). Set it to an
	// entirely different name for a primary rename that is NOT an alias
	// (e.g. websites "enable_ipns" -> "enable-ipns").
	Name string

	// KeepUnderscores, when true, suppresses default hyphenation of the leaf
	// name (rare; a leaf that must literally keep an underscore).
	KeepUnderscores bool

	// Aliases are leaf-level aliases (remove->rm, list->ls).
	Aliases []string

	// ExcludeFromFlatMount, when true, suppresses flat (single-segment) leaf
	// emission under the domain root. It is used for ops that must NOT appear
	// as a top-level command under the root (account "otp*", dns non-2-segment
	// names, admin domain-prefix leaves).
	ExcludeFromFlatMount bool

	// FoldFlat, when true and a sibling op with the SAME single display token is
	// declared as a parent, moves this flat leaf INTO that parent as its
	// top-level Action (the vault "share" -> "share accept" fold).
	FoldFlat bool

	// Order fixes this leaf's position among its siblings (ascending). Ties
	// (or 0) keep registry-declaration order, the freeze source for today's
	// map-iteration-arbitrary ordering. Negative values slot before defaults.
	Order int
}

// ShapeRegistry is the declarative source of truth: canonical op Name -> shape.
// It is package-level plain data in internal/clicatalog/shapes_<domain>.go,
// read-only after init: no branches, no string inference.
type ShapeRegistry map[string]OpShape

// DomainRoot declares the synthesized top-level parent for one domain. The root
// itself is created by the consuming mount/frontend; the transform returns the
// ordered list of its direct children.
type DomainRoot struct {
	Name, Category, Usage, Desc string
	Aliases                     []string
	Order                       int
	// Sections declares admin-style section parents under the root, in order.
	// Each section's children are the ops whose Path[1] equals the section.
	Sections []Segment
}

// NodeSpec is the resolved shape metadata for a synthesized parent node. It is
// neutral: no opmesh, no cli.
type NodeSpec struct {
	Name, Category, Usage, Desc string
	Aliases                     []string
	Order                       int
}

// LeafLocator is the resolved shape of one leaf that a builder materializes. It
// carries NO behavior; the builder decides how flags/action/renderer attach.
type LeafLocator struct {
	Op       opmesh.Operation // canonical op (builders read flags/gate from Op)
	Path     []string         // full resolved path incl. domain root
	Name     string           // resolved primary display name
	Aliases  []string         // resolved leaf aliases
	Category string           // resolved leaf category
	Order    int
}

// LeafBuilder is the mount-owned materializer for one leaf node. It returns the
// frontend node type T (CLI: *cli.Command). cfg is passed through untouched;
// clicatalog never names the concrete config type — the CLI package's builder
// closure is the only place it is type-asserted.
type LeafBuilder[T any] func(loc LeafLocator, cfg any) (T, error)

// ParentBuilder is the mount-owned materializer for one synthesized parent
// node. ownLeaf is set (non-nil for pointer node types) only when a FoldFlat
// leaf was folded INTO this parent (the fold's top-level Action); the builder
// decides how to merge it (CLI: parent.Action = ownLeaf.Action plus merged
// flags/usage). A zero/nil ownLeaf means a plain grouping node wrapping
// children. (Frontend node types in practice are pointers, so the zero value
// reliably signals "no folded leaf".)
type ParentBuilder[T any] func(spec NodeSpec, ownLeaf T, children []T) (T, error)

// domainSegments returns the explicit Segment for a Path index, or a derived
// default when none is declared. The default parent derivation (Name from the
// path token, category inherited, usage "Manage <Name>") mirrors today's
// ad-hoc synthesized parents.
func (o OpShape) segmentFor(idx int, token string, inheritCategory string) Segment {
	if s, ok := o.Segments[idx]; ok {
		if s.Name == "" {
			s.Name = token
		}
		if s.Category == "" {
			s.Category = inheritCategory
		}
		if s.Usage == "" {
			s.Usage = defaultUsage(s.Name)
		}
		return s
	}
	return Segment{
		Name:     token,
		Category: inheritCategory,
		Usage:    defaultUsage(token),
	}
}

func defaultUsage(name string) string { return "Manage " + name }

// defaultPath derives the tree Path for an op with no registry entry: the
// first underscore token of the canonical Name is the domain root (dropped from
// the display path, since the root is mount-created); the remaining tokens
// become the path after the root. This reproduces today's *flat* default:
//
//	"ipns_publish"   -> {"ipns","publish"}
//	"dns_zones_list" -> {"dns","zones","list"}   (only if not explicitly shaped)
func defaultPath(op opmesh.Operation, rootName string) []string {
	name := op.Name()
	if name == "" {
		return []string{rootName}
	}
	// The default is FLAT: strip the first underscore token (the canonical
	// domain prefix, which maps to the already-mount-created root) and leave
	// the ENTIRE remainder as a single leaf segment; the leaf then hyphenates
	// the whole remainder ("dom_thing_one" -> flat leaf "thing-one"). Nested
	// shapes are always an explicit declaration, never inferred here.
	if idx := strings.Index(name, "_"); idx > 0 {
		return []string{rootName, name[idx+1:]}
	}
	return []string{rootName, name}
}

// leafName derives the primary display name for a leaf from its last path
// token, honoring KeepUnderscores and an explicit Name override.
func leafName(shape *OpShape, last string) string {
	if shape != nil && shape.Name != "" {
		return shape.Name
	}
	if shape != nil && shape.KeepUnderscores {
		return last
	}
	return strings.ReplaceAll(last, "_", "-")
}

// sortedIndexes returns the order in which leaf children should be laid out for
// a given parent: ascending by Order, ties preserving declaration order
// (stable). This is the determinism guarantee that replaces today's
// map-iteration-arbitrary emission order.
func sortChildren[T any](nodes []*treeNode[T]) {
	sort.SliceStable(nodes, func(i, j int) bool {
		if nodes[i].order != nodes[j].order {
			return nodes[i].order < nodes[j].order
		}
		return nodes[i].declIndex < nodes[j].declIndex
	})
}

// treeNode is the transform's internal neutral tree node. It is plain data and
// never leaks to callers (they only see materialized `T` values).
type treeNode[T any] struct {
	name, category, usage, desc string
	aliases                     []string
	order, declIndex            int
	isLeaf                      bool
	leafLoc                     *LeafLocator
	foldLeaf                    *LeafLocator // non-nil => FoldFlat leaf folded in
	children                    []*treeNode[T]
}

// CompileCommandTree builds an ordered, nested command tree covering ops per
// their OpShape, then materializes every node through the mount-supplied
// builders. It returns the domain root's ordered direct children as []T
// (CLI: []*cli.Command); the consuming mount creates the DomainRoot parent
// itself from the DomainRoot declaration.
//
// Algorithm:
//  1. Resolve per-op Order; walk Path to build parents on first encounter.
//  2. Exclusion: drop ExcludeFromFlatMount ops before any nesting.
//  3. The domain root is supplied by the caller (never constructed here).
//  4. Create parents walking Path[1:], synthesizing from Segments or defaults.
//  5. Resolve each leaf (name/aliases/category/order, hyphenation, renames).
//  6. Fold flat-into-parent when FoldFlat and a sibling parent matches.
//  7. Materialize leaves bottom-up then parents via the builders.
//  8. Sort siblings deterministically and return the root's direct children.
//
// Errors are returned for malformed declarations (fold with no matching parent,
// a Path that never reaches a root token) so a bad shape is caught at compile
// time rather than as a runtime surprise.
func CompileCommandTree[T any](
	ops []opmesh.Operation,
	reg ShapeRegistry,
	root DomainRoot,
	cfg any,
	buildLeaf LeafBuilder[T],
	buildParent ParentBuilder[T],
) ([]T, error) {
	// rootNode is an internal pseudo-node standing for the mount-created root.
	rootNode := &treeNode[T]{
		name: root.Name, category: root.Category,
		usage: root.Usage, desc: root.Desc, aliases: root.Aliases, order: root.Order,
	}
	parents := map[string]*treeNode[T]{root.Name: rootNode}

	// Pre-create declared sections (admin) under the root.
	for i := range root.Sections {
		s := root.Sections[i]
		if s.Name == "" {
			continue
		}
		seg := Segment{Name: s.Name, Category: s.Category, Usage: s.Usage, Desc: s.Desc, Aliases: s.Aliases, Order: s.Order}
		if seg.Usage == "" {
			seg.Usage = defaultUsage(seg.Name)
		}
		node := &treeNode[T]{
			name: seg.Name, category: seg.Category, usage: seg.Usage, desc: seg.Desc,
			aliases: seg.Aliases, order: seg.Order, declIndex: i,
		}
		rootNode.children = append(rootNode.children, node)
		parents[root.Name+"/"+seg.Name] = node
	}

	// pendingFolds records FoldFlat ops; they resolve AFTER every parent has
	// been created so overlapping sibling shapes ("share" + "share accept")
	// fold correctly regardless of the ops slice order.
	type pendingFold struct {
		op    opmesh.Operation
		shape OpShape
		path  []string
		decl  int
	}
	var folds []pendingFold

	decl := 0
	for _, op := range ops {
		if op == nil {
			continue
		}
		shape, shaped := reg[op.Name()]
		if shaped && shape.ExcludeFromFlatMount {
			continue
		}

		path := shape.Path
		if !shaped || len(path) == 0 {
			path = defaultPath(op, root.Name)
		}
		if len(path) == 0 || path[0] != root.Name {
			return nil, opNameError(op.Name(), "path must begin with domain root %q, got %v", root.Name, path)
		}

		// Defer FoldFlat resolution until all parents are built.
		if shaped && shape.FoldFlat {
			if len(path) < 2 {
				return nil, opNameError(op.Name(), "FoldFlat requires at least one parent token in the path")
			}
			folds = append(folds, pendingFold{op: op, shape: shape, path: path, decl: decl})
			decl++
			continue
		}

		// Walk intermediate tokens, creating parents on first encounter.
		cur := rootNode
		for i := 1; i < len(path)-1; i++ {
			token := path[i]
			parentKey := ""
			// find the parent key: trace from root
			parentKey = strings.Join(append([]string{root.Name}, path[1:i+1]...), "/")
			next, ok := parents[parentKey]
			if !ok {
				var seg Segment
				if shaped {
					seg = shape.segmentFor(i, token, cur.category)
				} else {
					seg = Segment{Name: token, Category: cur.category, Usage: defaultUsage(token)}
				}
				next = &treeNode[T]{
					name: seg.Name, category: seg.Category, usage: seg.Usage, desc: seg.Desc,
					aliases: seg.Aliases, order: seg.Order, declIndex: decl,
				}
				parents[parentKey] = next
				cur.children = append(cur.children, next)
			}
			cur = next
		}

		// Resolve and attach the leaf.
		category := root.Category
		if cur != rootNode && cur.category != "" {
			category = cur.category
		}
		loc := resolvedLeaf(op, path, shapeOrNil(shaped, shape), category)
		leaf := &treeNode[T]{
			name: loc.Name, category: loc.Category, aliases: loc.Aliases,
			order: loc.Order, declIndex: decl, isLeaf: true, leafLoc: &loc,
		}
		cur.children = append(cur.children, leaf)
		decl++
	}

	// Resolve FoldFlat ops now that every parent exists.
	for _, f := range folds {
		parentKey := root.Name + "/" + f.path[1]
		parent, ok := parents[parentKey]
		if !ok || parent.isLeaf {
			return nil, opNameError(f.op.Name(), "FoldFlat set but no matching parent %q", f.path[1])
		}
		if parent.foldLeaf != nil {
			return nil, opNameError(f.op.Name(), "FoldFlat collision: parent %q already has a folded leaf", f.path[1])
		}
		loc := resolvedLeaf(f.op, f.path, &f.shape, parent.category)
		parent.foldLeaf = &loc
	}

	// Sort each parent's children deterministically.
	sortChildrenRec(rootNode)

	// Materialize bottom-up.
	out := make([]T, 0, len(rootNode.children))
	for _, c := range rootNode.children {
		v, err := materializeNode(c, cfg, buildLeaf, buildParent)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

func shapeOrNil(shaped bool, shape OpShape) *OpShape {
	if !shaped {
		return nil
	}
	return &shape
}

func resolvedLeaf(op opmesh.Operation, path []string, shape *OpShape, inheritCategory string) LeafLocator {
	last := path[len(path)-1]
	name := leafName(shape, last)
	category := inheritCategory
	order := 0
	var aliases []string
	if shape != nil {
		aliases = shape.Aliases
		order = shape.Order
	}
	return LeafLocator{
		Op: op, Path: path, Name: name, Aliases: aliases,
		Category: category, Order: order,
	}
}

// sortChildrenRec applies the deterministic (Order, declaration-order) sort to a
// node and, recursively, to every child.
func sortChildrenRec[T any](n *treeNode[T]) {
	sortChildren(n.children)
	for _, c := range n.children {
		sortChildrenRec(c)
	}
}

// materializeNode builds a frontend node `T` for a tree node bottom-up.
func materializeNode[T any](n *treeNode[T], cfg any, buildLeaf LeafBuilder[T], buildParent ParentBuilder[T]) (T, error) {
	var zero T
	if n.isLeaf {
		return buildLeaf(*n.leafLoc, cfg)
	}

	children := make([]T, 0, len(n.children))
	for _, c := range n.children {
		v, err := materializeNode(c, cfg, buildLeaf, buildParent)
		if err != nil {
			return zero, err
		}
		children = append(children, v)
	}

	var ownLeaf T
	if n.foldLeaf != nil {
		v, err := buildLeaf(*n.foldLeaf, cfg)
		if err != nil {
			return zero, err
		}
		ownLeaf = v
	}

	return buildParent(NodeSpec{
		Name: n.name, Category: n.category, Usage: n.usage, Desc: n.desc,
		Aliases: n.aliases, Order: n.order,
	}, ownLeaf, children)
}

// opNameError formats a compile-time shape error with the offending op name.
func opNameError(op, format string, args ...any) error {
	return &shapeError{op: op, msg: fmt.Sprintf(format, args...)}
}

type shapeError struct{ op, msg string }

func (e *shapeError) Error() string { return "clicatalog shape: operation " + e.op + ": " + e.msg }
