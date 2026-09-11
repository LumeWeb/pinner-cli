package clicatalog

// shapes_pins.go declares the declarative command shape for the pins catalog
// domain, consumed by CompileCommandTree.
//
// Canonical op names (all-underscore, from catalogops.PinsOperations):
//
//	pins_list    -> {"pins","list"}    (canonical "list", alias "ls")
//	pins_add     -> {"pins","add"}
//	pins_rm      -> {"pins","rm"}      (op canonical leaf is "rm", not "remove")
//	pins_status  -> {"pins","status"}
//	pins_update  -> {"pins","update"}
//
// Ideal naming is applied only where this domain's operations support it:
// pins_list's canonical leaf is "list", so the CLI leaf is named canonically
// "list" with the "ls" alias (the historically documented `pinner pins ls`
// survives as the alias). pins_rm's canonical leaf is "rm" (the operation is
// literally named pins_rm, not pins_remove), so there is no "remove" to make
// canonical — the CLI keeps "rm" as the primary leaf name with no alias.
//
// Every pins op is a single flat leaf under the root (no intermediate
// parents), so the default flat derivation would suffice; we still declare
// each op explicitly for full registry coverage and to attach the "ls" alias.
// Order is left at 0 so tied siblings keep the registry-declaration order
// (which matches PinsOperations' emission order), per the model's
// deterministic (Order, declaration-order) sort.

// PinsShapes is the ShapeRegistry for the pins domain, keyed by the
// all-underscore canonical op Name.
var PinsShapes = ShapeRegistry{
	"pins_list": {
		Path:    []string{"pins", "list"},
		Aliases: []string{"ls"},
	},
	"pins_add":    {Path: []string{"pins", "add"}},
	"pins_rm":     {Path: []string{"pins", "rm"}},
	"pins_status": {Path: []string{"pins", "status"}},
	"pins_update": {Path: []string{"pins", "update"}},
}

// PinsDomainRoot is the DomainRoot declaration for the pins domain. The root
// command itself is constructed by the consuming CLI mount; the transform
// returns its ordered leaf children (list, add, rm, status, update).
var PinsDomainRoot = DomainRoot{
	Name:     "pins",
	Category: "Pinning",
	Usage:    "Manage pinned content",
	Desc:     "Manage your pinned IPFS content with subcommands for adding, removing, listing, checking status, and updating pin metadata. These subcommands are compiled from the canonical operation catalog (internal/catalogops).",
}
