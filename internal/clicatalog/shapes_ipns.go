package clicatalog

// shapes_ipns.go declares the declarative command shape for the IPNS catalog
// domain, consumed by CompileCommandTree.
//
// Canonical op names (all-underscore, from catalogops.IPNSOperations):
//
//	ipns_keys_list / create / get / delete  -> {"ipns","keys",<leaf>}
//	ipns_publish, ipns_republish, ipns_resolve -> {"ipns",<leaf>}
//
// The `keys` sibling parent is declared once via Segments[1] and shared by the
// four keys ops; the flat leaves use the default flat derivation. Order is
// left at 0 so tied siblings keep the registry-declaration order (which today
// matches the catalog's stable emission order and the registration test), per
// the model's deterministic (Order, declaration-order) sort.

// IPNSShapes is the ShapeRegistry for the ipns domain. Every op is declared
// explicitly (the keys nesting differs from flat), keyed by the all-underscore
// canonical op Name.
var IPNSShapes = ShapeRegistry{
	"ipns_keys_list": {
		Path:     []string{"ipns", "keys", "list"},
		Segments: map[int]Segment{1: ipnsKeysSegment},
	},
	"ipns_keys_create": {
		Path:     []string{"ipns", "keys", "create"},
		Segments: map[int]Segment{1: ipnsKeysSegment},
	},
	"ipns_keys_get": {
		Path:     []string{"ipns", "keys", "get"},
		Segments: map[int]Segment{1: ipnsKeysSegment},
	},
	"ipns_keys_delete": {
		Path:     []string{"ipns", "keys", "delete"},
		Segments: map[int]Segment{1: ipnsKeysSegment},
	},
	"ipns_publish":   {Path: []string{"ipns", "publish"}},
	"ipns_republish": {Path: []string{"ipns", "republish"}},
	"ipns_resolve":   {Path: []string{"ipns", "resolve"}},
}

// ipnsKeysSegment is the shared `keys` sibling parent under the ipns root.
var ipnsKeysSegment = Segment{
	Name:     "keys",
	Category: "Management",
	Usage:    "Manage IPNS keys",
}

// IPNSDomainRoot is the DomainRoot declaration for the ipns domain. The root
// command itself is constructed by the consuming CLI mount; the transform
// returns its ordered children (keys parent + publish/republish/resolve).
var IPNSDomainRoot = DomainRoot{
	Name:     "ipns",
	Category: "Management",
	Usage:    "Manage IPNS (InterPlanetary Name System) keys and records",
	Desc:     "Manage IPNS keys (create/list/get/delete), publish CIDs to IPNS names, republish records, and resolve IPNS names. These subcommands are compiled from the canonical operation catalog (internal/catalogops).",
}
