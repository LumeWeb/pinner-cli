package clicatalog

// shapes_operations.go declares the declarative command shape for the
// operations catalog domain, consumed by CompileCommandTree.
//
// Canonical op names (all-underscore, emitted by catalogops.OperationsOperations
// in a stable declaration order):
//
//	operations_list -> {"operations","list"}
//	operations_get  -> {"operations","get"}
//
// Every op is a single flat leaf under the `operations` root (no intermediate
// synthesized parents), so the default flat derivation would suffice; we still
// declare every emitted op explicitly for full registry coverage. The leaves
// are already single tokens, so no hyphenation, rename, alias, exclusions or
// folds apply here.
//
// Order is set explicitly (get before list) rather than left to declaration
// order. The catalog compiler emits leaves alphabetically (cat.Search sorts by
// Name, so "operations_get" < "operations_list"), and the command-tree
// integration test pins that get,list order. To keep the tree byte-identical
// to that surface, operations_get carries Order 0 and operations_list Order 1
// — a deliberate deviation from the other domains' declaration-order freeze.

// OperationsShapes is the ShapeRegistry for the operations domain, keyed by
// the all-underscore canonical op Name.
var OperationsShapes = ShapeRegistry{
	"operations_get":  {Path: []string{"operations", "get"}, Order: 0},
	"operations_list": {Path: []string{"operations", "list"}, Order: 1},
}

// OperationsDomainRoot is the DomainRoot declaration for the operations domain.
// The root command itself is constructed by the consuming CLI mount
// (newOperationsCommandCatalog); the transform returns its ordered leaf
// children (list, get).
var OperationsDomainRoot = DomainRoot{
	Name:     "operations",
	Category: "Management",
	Usage:    "List and inspect account operations",
	Desc:     "View and monitor account operations such as uploads, pins, and other processing tasks. These subcommands are compiled from the canonical operation catalog (internal/catalogops).",
}
