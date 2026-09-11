package mcp

// Re-exports of the dependency-neutral vault-mint durability/flush canon
// (internal/mcp/mintcontract). The fragments and their composition live in
// THAT package — the ONE source shared with the toolforge/vault/upload
// subpackage descriptions (which cannot import this parent package) — so the
// staged-write semantics, the vault_flush accepted-job shape, the durability
// polling loop, and the no-upload_status fact each have exactly one
// definition and can never drift between the capabilities description, the
// agent-guide paths, and the tool/app copy. These constants exist so the
// parent package's consumers (capabilities.go, agent_guide.go, tests) keep
// their historical in-package names.

import "go.lumeweb.com/pinner-cli/internal/mcp/mintcontract"

const (
	// vaultMintNonBlockingLead names the invariant: a vault write to a
	// presigned mint PUT does not block on durability.
	vaultMintNonBlockingLead = mintcontract.NonBlockingLead

	// vaultMintStagedWrite is the staging contract: the presigned PUT returns
	// after locally staging the bytes, at which point the file is readable
	// here.
	vaultMintStagedWrite = mintcontract.StagedWrite

	// vaultFlushAcceptedJob is the vault_flush response payload shape.
	vaultFlushAcceptedJob = mintcontract.FlushAcceptedJob

	// vaultFlushJobShape is vault_flush's accepted-job contract: non-blocking
	// invocation, accepted-job response shape.
	vaultFlushJobShape = mintcontract.FlushJobShape

	// vaultDurabilitySource composes where durability comes from without
	// restating the job shape.
	vaultDurabilitySource = mintcontract.DurabilitySource

	// vaultDurabilityFollows composes where durability comes from with the
	// flush job shape inline.
	vaultDurabilityFollows = mintcontract.DurabilityFollows

	// vaultDurabilityPollCore is the durability wait loop's core: which tools
	// to poll and the terminal status to wait for, WITHOUT the sharing
	// precondition — consumers wrap it in their own context (e.g. the share
	// flow's ", then share/send again").
	vaultDurabilityPollCore = mintcontract.DurabilityPollCore

	// vaultDurabilityPoll is the durability wait loop: which tools to poll and
	// the terminal status to wait for.
	vaultDurabilityPoll = mintcontract.DurabilityPoll

	// vaultNoUploadStatus is the explicit absence fact: vault writes have no
	// upload_status counterpart to poll.
	vaultNoUploadStatus = mintcontract.NoUploadStatus

	// vaultNoUploadStatusWhy is vaultNoUploadStatus plus the
	// upload_file-contrast parenthetical.
	vaultNoUploadStatusWhy = mintcontract.NoUploadStatusWhy

	// vaultFlushTriage is the canonical vault_stat flush-diagnostic triage
	// (flush_started_at / flush_attempts / flush_error across the flushing,
	// failed, and never-started states). Both the capabilities completion
	// contract and the guide share flow compose this ONE fragment.
	vaultFlushTriage = mintcontract.VaultFlushTriage

	// vaultUploadStatusContrast is the upload_file-contrast parenthetical.
	vaultUploadStatusContrast = mintcontract.UploadStatusContrast

	// vaultMintLead is the canonical vault-mint lead clause (one-time
	// presigned PUT url bound to vault_path, never bytes stored). The
	// toolforge vault_put_file mint description, this package's
	// capabilities completion contract, and the guide's vault byte-route
	// detail/sentence all compose this ONE clause.
	vaultMintLead = mintcontract.VaultMintLead
)

// vaultMintDurabilityFacts composes the canonical durability facts in sentence
// order: staged write → durability source (background or vault_flush, with its
// accepted-job shape) → the durability wait loop → the no-upload_status fact.
// It is the ONE copy every vault-mint statement composes.
const vaultMintDurabilityFacts = mintcontract.DurabilityFacts

// vaultMintDurabilityCanon composes the canonical durability paragraph used
// verbatim by the guide consumers (flow detail, decision branch) so every
// statement of the contract stays word-for-word identical. The guide summary
// composes the same facts under its own tool-scoped lead (the traced invariant
// "vault_put_file is non-blocking" must keep naming the tool). Consumers
// append their own sentence-final punctuation (and, where the grammar needs a
// capitalized sentence opener, firstUpper of this text).
const vaultMintDurabilityCanon = mintcontract.DurabilityCanon

// vaultDurabilitySourceQualified is the durability-source clause carrying the
// capabilities surface's inline "(upload + pin)" qualifier, composed through
// mintcontract.DurabilitySourceWith (the ONE qualifier helper). A var, not a
// const, only because DurabilitySourceWith is a function call.
var vaultDurabilitySourceQualified = mintcontract.DurabilitySourceWith("upload + pin")

// firstUpper capitalizes the first byte of s so a lowercase-canonical fragment
// can open a sentence. It delegates to mintcontract.FirstUpper.
func firstUpper(s string) string {
	return mintcontract.FirstUpper(s)
}
