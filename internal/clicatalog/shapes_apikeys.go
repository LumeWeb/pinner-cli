package clicatalog

// shapes_apikeys.go declares the declarative command shape for the api-keys
// catalog domain, consumed by CompileCommandTree.
//
// Canonical op names (all-underscore, from catalogops.APIKeysOperations):
//
//	api_keys_list   -> {"api-keys","list"}
//	api_keys_create -> {"api-keys","create"}
//	api_keys_delete -> {"api-keys","delete"}
//
// Every api-keys op is a single flat leaf under the "api-keys" root (no
// intermediate parents), so the default flat derivation would suffice; we still
// declare each op explicitly for full registry coverage. The canonical domain
// prefix underscored ("api_keys_") maps onto the hyphenated root display name
// "api-keys", so each Path begins with the presented root token.
//
// Ideal naming is applied only where this domain's operations support it:
// every canonical leaf (list/create/delete) is already the historical CLI
// surface, so no rename and no leaf alias is needed. The only aliases live on
// the synthesized root parent itself ("apikey", "api-key") and are carried by
// the consuming CLI mount from APIKeysDomainRoot.
//
// Order is left at 0 everywhere so tied siblings keep the
// registry-declaration (== APIKeysOperations emission) order (list, create,
// delete), per the model's deterministic (Order, declaration-order) sort.

// APIKeysShapes is the ShapeRegistry for the api-keys domain, keyed by the
// all-underscore canonical op Name.
var APIKeysShapes = ShapeRegistry{
	"api_keys_list":   {Path: []string{"api-keys", "list"}},
	"api_keys_create": {Path: []string{"api-keys", "create"}},
	"api_keys_delete": {Path: []string{"api-keys", "delete"}},
}

// APIKeysDomainRoot is the DomainRoot declaration for the api-keys domain. The
// root command itself is constructed by the consuming CLI mount (which also
// applies the root aliases); the transform returns its ordered leaf children
// (list, create, delete).
var APIKeysDomainRoot = DomainRoot{
	Name:     "api-keys",
	Aliases:  []string{"apikey", "api-key"},
	Category: "Management",
	Usage:    "Manage API keys",
	Desc:     "Manage API keys for your Pinner.xyz account. These subcommands are compiled from the canonical operation catalog (internal/catalogops).",
}
