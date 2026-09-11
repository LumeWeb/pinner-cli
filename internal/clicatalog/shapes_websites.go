package clicatalog

// shapes_websites.go declares the declarative command shape for the websites
// catalog domain, consumed by CompileCommandTree.
//
// Canonical op names (all-underscore, emitted by catalogops.WebsitesOperations
// in a stable declaration order):
//
//	websites_list/create/get/update/enable_ipns/delete/validate/config
//	                                  -> {"websites",<leaf>}             (flat leaves)
//	websites_ssl_status               -> {"websites","ssl","status"}
//	websites_domains_*                -> {"websites","domains",<leaf>}  (and dane+)
//	websites_platform_domain*         -> {"websites","platform",<leaf>}
//
// Name mapping:
//
//	- websites_enable_ipns renders as the canonical "enable-ipns" leaf with the
//	  legacy "ipns" alias (the historical `pinner websites enable-ipns`).
//	- The top-level websites_list renders as the canonical "list" leaf with the
//	  "ls" alias (ideal naming rule: list canonical, ls a muscle-memory
//	  alias), matching the domains and pins leaves below.
//	- websites_delete keeps the historical "delete" leaf (not converged to
//	  "remove": the operation ID is websites_delete, and "delete" is a full
//	  verb rather than an abbreviation; pending an explicit remove rename
//	  decision).
//	- The `domains` parent keeps its conventional "domain" alias; the domains
//	  leaves keep the "ls"/"rm" aliases on their canonical "list"/"remove"
//	  names. websites_domains_dane_republish nests 3 levels deep under a `dane`
//	  parent.
//	- The two `platform` ops share one "platform-domain" parent (the historical
//	  `websites platform-domain domains-list` / `domain-availability`).
//	- The `ssl` parent wraps the single `websites_ssl_status` leaf as "status".
//
// Order is left at 0 everywhere so tied siblings keep the
// registry-declaration (== WebsitesOperations emission) order, per the model's
// deterministic (Order, declaration-order) sort. The interactive wizard
// commands are NOT catalog ops and are appended by the CLI mount after the
// compiled tree is returned.

// WebsitesShapes is the ShapeRegistry for the websites domain, keyed by the
// all-underscore canonical op Name.
var WebsitesShapes = ShapeRegistry{
	// Flat leaves under the websites root. websites_list is the canonical
	// `list` leaf with the `ls` alias (the ideal naming rule: list canonical,
	// ls stays a muscle-memory alias), matching websites_domains_list and the
	// pins domain below.
	"websites_list": {
		Path:    []string{"websites", "list"},
		Aliases: []string{"ls"},
	},
	"websites_get":    {Path: []string{"websites", "get"}},
	"websites_create": {Path: []string{"websites", "create"}},
	"websites_update": {Path: []string{"websites", "update"}},
	"websites_enable_ipns": {
		Path:    []string{"websites", "enable_ipns"},
		Name:    "enable-ipns",
		Aliases: []string{"ipns"},
	},
	"websites_delete":   {Path: []string{"websites", "delete"}},
	"websites_validate": {Path: []string{"websites", "validate"}},
	"websites_config":   {Path: []string{"websites", "config"}},

	// SSL status nests under an "ssl" parent.
	"websites_ssl_status": {
		Path:     []string{"websites", "ssl", "status"},
		Segments: map[int]Segment{1: websitesSSLStatusSegment},
	},

	// Domain bindings nest under a "domains" parent (alias "domain").
	"websites_domains_list": {
		Path:     []string{"websites", "domains", "list"},
		Segments: map[int]Segment{1: websitesDomainsSegment},
		Aliases:  []string{"ls"},
	},
	"websites_domains_add": {
		Path:     []string{"websites", "domains", "add"},
		Segments: map[int]Segment{1: websitesDomainsSegment},
	},
	"websites_domains_remove": {
		Path:     []string{"websites", "domains", "remove"},
		Segments: map[int]Segment{1: websitesDomainsSegment},
		Aliases:  []string{"rm"},
	},
	"websites_domains_verify": {
		Path:     []string{"websites", "domains", "verify"},
		Segments: map[int]Segment{1: websitesDomainsSegment},
	},
	"websites_domains_dns_requirements": {
		Path:     []string{"websites", "domains", "dns_requirements"},
		Segments: map[int]Segment{1: websitesDomainsSegment},
	},
	"websites_domains_dane_republish": {
		Path:     []string{"websites", "domains", "dane", "republish"},
		Segments: map[int]Segment{1: websitesDomainsSegment, 2: websitesDANESegment},
	},
	"websites_domains_convert_onchain": {
		Path:     []string{"websites", "domains", "convert_onchain"},
		Segments: map[int]Segment{1: websitesDomainsSegment},
	},
	"websites_domains_update": {
		Path:     []string{"websites", "domains", "update"},
		Segments: map[int]Segment{1: websitesDomainsSegment},
	},

	// Platform (free-subdomain) ops share a "platform-domain" parent.
	"websites_platform_domains_list": {
		Path:     []string{"websites", "platform", "domains_list"},
		Segments: map[int]Segment{1: websitesPlatformSegment},
	},
	"websites_platform_domain_availability": {
		Path:     []string{"websites", "platform", "domain_availability"},
		Segments: map[int]Segment{1: websitesPlatformSegment},
	},
}

// websitesSSLStatusSegment is the `ssl` sibling parent under the websites root.
var websitesSSLStatusSegment = Segment{
	Name:     "ssl",
	Category: "Management",
	Usage:    "Manage website ssl",
}

// websitesDomainsSegment is the shared `domains` sibling parent under the
// websites root, with its conventional singular "domain" alias.
var websitesDomainsSegment = Segment{
	Name:     "domains",
	Category: "Management",
	Usage:    "Manage domain bindings for a website",
	Aliases:  []string{"domain"},
}

// websitesDANESegment is the `dane` child parent under the domains parent.
var websitesDANESegment = Segment{
	Name:     "dane",
	Category: "Management",
	Usage:    "Manage a domain's DANE TLSA records",
}

// websitesPlatformSegment is the `platform-domain` sibling parent under the
// websites root (the "platform" path segment renders hyphenated as
// "platform-domain", matching the historical CLI expectation).
var websitesPlatformSegment = Segment{
	Name:     "platform-domain",
	Category: "Management",
	Usage:    "Manage platform (free-subdomain) domain availability",
}

// WebsitesDomainRoot is the DomainRoot declaration for the websites domain.
// The root command itself is constructed by the consuming CLI mount; the
// transform returns its ordered children (flat leaves + ssl/domains/
// platform-domain parents).
var WebsitesDomainRoot = DomainRoot{
	Name:     "websites",
	Category: "Management",
	Usage:    "Manage websites",
	Desc:     `Manage websites: associate domain names with CIDs so your IPFS/IPNS content is served over your custom domains. These subcommands are compiled from the canonical operation catalog (internal/catalogops).`,
}
