package clicatalog

// shapes_account.go declares the declarative command shape for the account
// catalog domain, consumed by CompileCommandTree.
//
// Canonical op names (all-underscore, emitted by catalogops.AccountOperations
// in a stable declaration order):
//
//	account_info              -> {"account","info"}
//	account_update_email      -> {"account","update_email"}    (leaf "update-email")
//	account_update_password   -> {"account","update_password"}  (leaf "update-password")
//	account_subscription      -> {"account","subscription"}
//	account_quota             -> {"account","quota"}
//
// Every op is a single flat leaf under the `account` root (no intermediate
// synthesized parents), so the default flat derivation would suffice; we still
// declare every emitted op explicitly for full registry coverage and to keep
// the CLI surface (kebab-cased multi-token leaves) explicit.
//
// The one non-flat op is account_otp_disable. The CLI historically nests it
// under the hand-written `otp` parent (`pinner account otp disable`), which is
// NOT part of the flat `account` surface — the disable flow is catalog-wired
// (via the hand-written parent's child, accountOTPDisableWired in the CLI
// mount) but the op must never ALSO emit as a flat top-level `otp-disable`
// under the root. That suppression is declared here as ExcludeFromFlatMount
// rather than hardcoded in the mount, matching the DNS/admin exclusion pattern.
//
// Order is left at 0 everywhere so tied siblings keep the
// registry-declaration (== AccountOperations emission, minus the excluded
// disable op) order: info, update-email, update-password, subscription, quota
// — the historical hierarchical order — per the model's deterministic (Order,
// declaration-order) sort.

// AccountShapes is the ShapeRegistry for the account domain, keyed by the
// all-underscore canonical op Name.
var AccountShapes = ShapeRegistry{
	"account_info":            {Path: []string{"account", "info"}},
	"account_update_email":    {Path: []string{"account", "update_email"}},
	"account_update_password": {Path: []string{"account", "update_password"}},
	"account_subscription":    {Path: []string{"account", "subscription"}},
	"account_quota":           {Path: []string{"account", "quota"}},

	// account_otp_disable is NOT emitted as a flat `account otp-disable` leaf:
	// it nests under the hand-written `otp` parent as `account otp disable`
	// (see accountOTPDisableWired in the CLI mount), so the compiled tree must
	// exclude it from the flat root surface.
	"account_otp_disable": {Path: []string{"account", "otp_disable"}, ExcludeFromFlatMount: true},
}

// AccountDomainRoot is the DomainRoot declaration for the account domain. The
// root command itself is constructed by the consuming CLI mount (which merges
// the compiled catalog leaves with the hand-written `otp`/`api-keys` parents);
// the transform returns its ordered leaf children (info, update-email,
// update-password, subscription, quota).
var AccountDomainRoot = DomainRoot{
	Name:     "account",
	Category: "Setup",
	Usage:    "Manage account settings",
	Desc: `Manage your Pinner.xyz account profile, email, password, subscription, quota, 2FA configuration, and API keys.

Examples:
pinner account info
pinner account update-email you@example.com --password currentpass
pinner account update-password
pinner account quota
pinner account quota --open
pinner account subscription
pinner account subscription --open
pinner account otp enable
pinner account otp disable --password mypassword`,
}
