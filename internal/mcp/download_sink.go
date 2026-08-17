package mcp

// DownloadSink enumerates where a downloaded file's bytes can land. It is the
// mirror of UploadSource but for the OUTPUT side: upload sources describe where
// bytes come FROM; download sinks describe where the retrieved bytes go TO.
//
// Unlike UploadSource, download sinks are NOT transport-gated. An upload's
// `path` source means "read a file the *agent* has on its box", so it is only
// meaningful when the caller shares the server's host (stdio). A download's
// `local` sink means "write to the *server's own disk*" — and that disk is
// always present regardless of transport, because the bytes being downloaded
// already live on the server's host (fetched from IPFS or the vault). Host-local
// write is therefore valid on EVERY transport (stdio, HTTP, tunnel). The `drop`
// sink is an ADDITIONAL shipping option that only requires a reachable HTTP
// mux (browser/remote consumers that share no disk with the server).
type DownloadSink string

const (
	// SinkLocal writes the downloaded bytes to a host-side output path on the
	// MCP server's own filesystem. Valid on every transport: the server's disk
	// is always local to the server process.
	SinkLocal DownloadSink = "local"
	// SinkDrop mints a one-time HTTP GET filedrop endpoint the consumer pulls
	// to receive the bytes out of band. Only valid when a reachable HTTP mux
	// exists (plain HTTP or a real tunnel) — see downloadSinksFor.
	SinkDrop DownloadSink = "drop"
)

// Valid reports whether s is a recognized download sink.
func (s DownloadSink) Valid() bool {
	switch s {
	case SinkLocal, SinkDrop:
		return true
	}
	return false
}

// downloadSinksFor returns the download destinations the running server honors,
// in a stable order. host-local write is always offered: the bytes being
// downloaded already live on the server's host (fetched from IPFS/vault), so a
// local sink persists them to a host path on any transport (stdio, HTTP,
// tunnel). The filedrop GET sink is an ADDITIONAL shipping option for consumers
// that share no disk with the server (browser UI, remote agent) — but only when
// an HTTP mux is reachable. The embedded OpenAI tunnel exposes no reachable
// mux, so its minted drop URL would fall back to an unreachable loopback (the
// same pitfall as vaultPutFileAvailable): never advertise drop there.
//
// It is derived from the same reachability decision used at registration, so
// the capability report cannot drift from what the download tools accept.
func downloadSinksFor(dropWired, tunnelOpenAI bool) []DownloadSink {
	sinks := []DownloadSink{SinkLocal}
	if dropWired && !tunnelOpenAI {
		sinks = append(sinks, SinkDrop)
	}
	return sinks
}
