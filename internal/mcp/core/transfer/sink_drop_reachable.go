package transfer

// SinkDropReachable is the ONE filedrop-sink reachability decision CLI side:
// sink=drop is advertised/accepted only when a filedrop coordinator is wired
// AND the transport is not the embedded OpenAI tunnel (which exposes no
// reachable HTTP mux — a minted drop URL would fall back to an unreachable
// loopback).
//
// On the module side transfer's downloadSinksFor / SinkEnumValues already
// derive this same single decision (the published `sink` enum rewrite and
// DownloadSinksAllowed validation both consume it). On the CLI side, the
// download_file and vault_get_file profile builders AND the parent package's
// sinkModesFor capability reporting consume THIS one predicate, so the
// advertised sink set can never drift from what a download tool accepts at
// invocation. The parent package's presigned MINT launcher gates
// (open_upload_manager / open_vault_manager registration and the
// uploadFile/vaultPutFile availability reports' presigned branch) route
// through this same predicate, so the filedrop advertisement, the register
// decision, and the mint reachability share ONE truth table by construction.
func SinkDropReachable(dropWired, tunnelOpenAI bool) bool {
	return dropWired && !tunnelOpenAI
}

// SinkDropReachableCase is one wiring combination of the filedrop-sink
// reachability truth table (dropWired / tunnelOpenAI inputs plus the expected
// reachable outcome).
type SinkDropReachableCase struct {
	DropWired    bool
	TunnelOpenAI bool
	Reachable    bool
}

// SinkDropReachableCases is the canonical reachability-case table: the ONE
// expected-outcome pin for the SinkDropReachable truth table across all four
// wiring combinations. Every package that tests a consumer of the predicate
// (core/transfer's download_profile builder, vault's vault_get_profile
// builder) must iterate THIS table — a package-local re-derivation would let
// the truth tables drift, so the owning-package table is the single source
// the consuming-package tests compose.
func SinkDropReachableCases() []SinkDropReachableCase {
	return []SinkDropReachableCase{
		{false, false, false},
		{true, false, true},
		{false, true, false},
		{true, true, false},
	}
}
