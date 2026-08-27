package hostenv

import "github.com/samber/lo"

// Feature is a named capability a host platform may or may not support.
// It functions like a caniuse entry: the forge checks whether the
// connected platform supports a feature to resolve which ToolTarget
// variant to materialize.
type Feature string

const (
	// FeatFileHostInput: host can build {download_url, file_id} file
	// references (OpenAI/ChatGPT runtime). Enables the top-level `file`
	// parameter on upload/vault tools.
	FeatFileHostInput Feature = "file-host-input"

	// FeatSourcePath: co-located filesystem read. The server shares the
	// host filesystem, so source.mode=path works.
	FeatSourcePath Feature = "source-path"

	// FeatSourceMint: presigned HTTP PUT endpoint. The server has a
	// reachable HTTP mux, so source.mode=mint works.
	FeatSourceMint Feature = "source-mint"

	// FeatSourceURL: server-fetchable HTTPS URL relay. The server can
	// fetch a URL the host provides (OpenAI tunnel).
	FeatSourceURL Feature = "source-url"

	// FeatSourceData: RFC 2397 data: URI relay. The server can decode
	// inlined file bytes (OpenAI tunnel).
	FeatSourceData Feature = "source-data"

	// FeatXMcpFile: draft x-mcp-file metadata should be exposed on tools.
	FeatXMcpFile Feature = "x-mcp-file"

	// FeatSinkLocal: host-side disk write. The server writes bytes to
	// a local path for download tools.
	FeatSinkLocal Feature = "sink-local"

	// FeatSinkDrop: one-time HTTP GET filedrop. The server has a
	// reachable HTTP mux to serve a transient download endpoint.
	FeatSinkDrop Feature = "sink-drop"

	// FeatMCPApps: client can render MCP Apps (ui:// resources with
	// MIME type text/html;profile=mcp-app).
	FeatMCPApps Feature = "mcp-apps-ui"

	// FeatElicitation: client supports form/URL elicitation
	// (input_required multi-round-trip).
	FeatElicitation Feature = "elicitation"

	// FeatRemoteAccess: server is reachable over HTTP from the client.
	// Implies the client is not co-located.
	FeatRemoteAccess Feature = "remote-access"

	// FeatCoLocated: server shares the host filesystem. The client
	// runs on the same machine as the MCP server.
	FeatCoLocated Feature = "co-located"
)

// FeatureSet is the set of features a platform profile supports.
type FeatureSet map[Feature]bool

// Has reports whether the feature set contains f.
func (fs FeatureSet) Has(f Feature) bool {
	return fs[f]
}

// HasAll reports whether the feature set contains every feature in req.
func (fs FeatureSet) HasAll(req FeatureSet) bool {
	for f := range req {
		if !fs[f] {
			return false
		}
	}
	return true
}

// Clone returns a shallow copy of the feature set. Callers that need to
// mutate a profile's features (e.g. to overlay runtime flags) MUST clone
// first — the FeatureSet in a static PlatformProfile is a shared map.
func (fs FeatureSet) Clone() FeatureSet {
	return lo.Assign(fs)
}

// transportMechanismFeatures returns the features that are a pure function of
// the running transport: which source/sink modes upload_file/download_file can
// actually serve, plus the reachability facts (co-located vs remote). These
// are derived, never hand-declared per host — an HTTP host always gets mint
// (never url/data), a stdio host path, and the OpenAI tunnel url/data.
//
// They form the transport-mechanism half of every profile's FeatureSet; the
// per-host capability half (FeatFileHostInput, FeatXMcpFile, FeatMCPApps,
// FeatElicitation) is what a profile actually declares. Deriving mechanism
// features from the transport (rather than storing them on the profile) is
// what keeps source-mode prose consistent with the transport-bound schema
// enum, so a host that supports the separate upload_data/upload_url tools can
// never accidentally flip upload_file's source mode to url/data.
func transportMechanismFeatures(t TransportKind) FeatureSet {
	switch t {
	case TransportStdio:
		return FeatureSet{FeatSourcePath: true, FeatSinkLocal: true, FeatCoLocated: true}
	case TransportHTTP:
		return FeatureSet{FeatSourceMint: true, FeatSinkLocal: true, FeatSinkDrop: true, FeatRemoteAccess: true}
	case TransportOpenAI:
		return FeatureSet{FeatSourceURL: true, FeatSourceData: true, FeatSinkLocal: true}
	default:
		return FeatureSet{}
	}
}
