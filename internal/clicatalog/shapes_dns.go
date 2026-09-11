package clicatalog

// shapes_dns.go declares the declarative command shape for the DNS catalog
// domain, consumed by CompileCommandTree.
//
// Canonical op names (all-underscore, from catalogops.DNSOperations):
//
//	dns_zones_list / create / get / delete / validate -> {"dns","zones",<leaf>}
//	dns_records_list / create / get / update / delete -> {"dns","records",<leaf>}
//
// Every DNS op is exactly two segments after the `dns_` prefix, so each nests
// under a single `zones`/`records` sibling parent declared once via
// Segments[1]; there is no flat leaf in the DNS tree. Order is left at 0 so
// tied siblings keep the registry-declaration order (which reproduces the
// current zones-then-records emission and each parent's list/create/get/
// delete/… ordering), per the model's deterministic (Order, declaration-order)
// sort.

// DNSShapes is the ShapeRegistry for the dns domain, keyed by the
// all-underscore canonical op Name.
var DNSShapes = ShapeRegistry{
	"dns_zones_list": {
		Path:     []string{"dns", "zones", "list"},
		Segments: map[int]Segment{1: dnsZonesSegment},
	},
	"dns_zones_create": {
		Path:     []string{"dns", "zones", "create"},
		Segments: map[int]Segment{1: dnsZonesSegment},
	},
	"dns_zones_get": {
		Path:     []string{"dns", "zones", "get"},
		Segments: map[int]Segment{1: dnsZonesSegment},
	},
	"dns_zones_delete": {
		Path:     []string{"dns", "zones", "delete"},
		Segments: map[int]Segment{1: dnsZonesSegment},
	},
	"dns_zones_validate": {
		Path:     []string{"dns", "zones", "validate"},
		Segments: map[int]Segment{1: dnsZonesSegment},
	},
	"dns_records_list": {
		Path:     []string{"dns", "records", "list"},
		Segments: map[int]Segment{1: dnsRecordsSegment},
	},
	"dns_records_create": {
		Path:     []string{"dns", "records", "create"},
		Segments: map[int]Segment{1: dnsRecordsSegment},
	},
	"dns_records_get": {
		Path:     []string{"dns", "records", "get"},
		Segments: map[int]Segment{1: dnsRecordsSegment},
	},
	"dns_records_update": {
		Path:     []string{"dns", "records", "update"},
		Segments: map[int]Segment{1: dnsRecordsSegment},
	},
	"dns_records_delete": {
		Path:     []string{"dns", "records", "delete"},
		Segments: map[int]Segment{1: dnsRecordsSegment},
	},
}

// dnsZonesSegment is the shared `zones` sibling parent under the dns root.
var dnsZonesSegment = Segment{
	Name:     "zones",
	Category: "DNS",
	Usage:    "Manage DNS zones",
	Desc:     "Manage DNS zones, the containers that hold DNS records for a domain.",
}

// dnsRecordsSegment is the shared `records` sibling parent under the dns root.
var dnsRecordsSegment = Segment{
	Name:     "records",
	Category: "DNS",
	Usage:    "Manage DNS records",
	Desc:     "Manage DNS records (A, AAAA, CNAME, TXT, MX, NS) inside an existing DNS zone.",
}

// DNSDomainRoot is the DomainRoot declaration for the dns domain. The root
// command itself is constructed by the consuming CLI mount; the transform
// returns its ordered children (zones + records parents).
var DNSDomainRoot = DomainRoot{
	Name:     "dns",
	Category: "Management",
	Usage:    "Manage DNS zones and records",
	Desc:     "Manage raw DNS zones and records for your domains (A/AAAA/CNAME/TXT/MX/NS, _dnslink, apex vs subdomain). Zones hold records; create the zone first ('dns zones create'), then manage records in it ('dns records *'). These subcommands are compiled from the canonical operation catalog (internal/catalogops).",
}
