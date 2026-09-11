// Package mintcontract is the dependency-neutral canonical source of the vault
// mint durability/flush prose contract. It lives OUTSIDE the mcp parent
// package (it imports nothing from it) so every subpackage that describes a
// mint-backed vault write — the toolforge tool descriptions, the vault tool
// result text, the vault upload app/launcher copy, and the parent package's
// capabilities/guide canon — composes THE SAME fragments: the staging status,
// the immediate readability, the non-blocking semantics, the vault_flush job
// shape, the durability polling loop, and the no-upload_status fact each have
// exactly ONE source and can never drift apart.
//
// Fragments are deliberately neutral: no consumer-specific lead args or
// trailing punctuation — each consumer composes its own prose around them.
package mintcontract

import "fmt"

// The canonical vault mint durability/flush fragments. NEVER paraphrase these
// at a call site: compose them.
const (
	// NonBlockingLead names the invariant: a vault write to a presigned mint
	// PUT does not block on durability. The summary canon composes it with a
	// colon and the staged-write clause.
	NonBlockingLead = "the vault write is non-blocking"

	// StagedWrite is the staging contract: the presigned PUT returns after
	// locally staging the bytes, at which point the file is already readable
	// on this instance (lowercase opener; consumers capitalize as grammar
	// requires via FirstUpper).
	StagedWrite = "the PUT returns quickly after staging the bytes locally (status: staged); the file is immediately readable from this instance"

	// FlushAcceptedJob is the vault_flush response payload shape. It is its
	// own fragment so consumers that mention the payload mid-clause (e.g. the
	// share flow's "run vault_flush (...)") still cite the same shape.
	FlushAcceptedJob = "accepted job { job_id, profile, path? }"

	// FlushJobShape is vault_flush's accepted-job contract: non-blocking
	// invocation, accepted-job response shape.
	FlushJobShape = "vault_flush is non-blocking and returns an " + FlushAcceptedJob

	// durabilityMechanisms is the WHERE part of the durability source; kept
	// separate so DurabilitySourceWith can insert an inline qualifier without
	// hand-paraphrasing the mechanisms clause.
	durabilityMechanisms = "happens in the background or via the vault_flush tool"

	// DurabilitySource composes where durability comes from: background
	// processing, or the vault_flush tool — WITHOUT restating the job shape
	// (consumers that state the payload separately compose DurabilitySource
	// plus FlushAcceptedJob; the contract canon composes DurabilityFollows).
	DurabilitySource = "durability on Sia " + durabilityMechanisms

	// DurabilityFollows is DurabilitySource with the flush job shape inline.
	DurabilityFollows = DurabilitySource + " (" + FlushJobShape + ")"

	// DurabilityPollCore is the durability wait loop's CORE: which tools to
	// poll and the terminal status to wait for. Consumers that wrap it in
	// their own context (e.g. the share flow's ", then share/send again")
	// compose this fragment; the contract canon composes DurabilityPoll.
	DurabilityPollCore = "poll vault_flush_status(job_id) or vault_stat until status: durable"

	// DurabilityPoll is DurabilityPollCore with the when-to-poll precondition.
	DurabilityPoll = DurabilityPollCore + " when durability is needed before sharing"

	// NoUploadStatus is the explicit absence fact: vault writes have no
	// upload_status counterpart to poll.
	NoUploadStatus = "there is no upload_status to poll"

	// UploadStatusContrast is the upload_file-contrast parenthetical: WHY
	// vault writes have no upload_status counterpart. Consumers that state
	// the absence fact compose NoUploadStatus, then append this fragment
	// (the tool descriptions cite it mid-sentence after their own canon).
	UploadStatusContrast = "(upload_status tracks upload_file's IPFS uploads, not vault writes)"

	// NoUploadStatusWhy is NoUploadStatus plus the contrast with upload_file's
	// IPFS upload_status, for consumers that explain why the handle is absent.
	NoUploadStatusWhy = NoUploadStatus + " " + UploadStatusContrast

	// VaultFlushTriage is the canonical vault_stat flush-diagnostic triage:
	// the full flush_started_at / flush_attempts / flush_error interpretation
	// across the flushing, failed, and never-started states. EVERY surface
	// that explains how to diagnose a non-durable file (capabilities
	// completion contract, guide share flow, ...) composes this ONE fragment,
	// so adding a diagnostic field is a single edit here. It is a complete
	// sentence (capitalized opener, trailing period) by design: it is
	// composed at sentence boundaries, not mid-clause.
	VaultFlushTriage = "If a file stays non-durable across polls, read vault_stat's flush_started_at, flush_attempts and flush_error: a flushing file shows a flush_started_at and a rising flush_attempts with no error, a failed file shows flush_attempts plus a non-empty flush_error, and a staged file that never started shows zero attempts/no error and an empty flush_started_at — compare now against flush_started_at to tell a long host upload from a hung pin."
)

// The mint LEAD clauses. Both previously lived as 3-4 drifted hand copies
// across the toolforge descriptions, the capabilities completion contracts,
// and the agent guide; every surface now composes these ONE definitions.
const (
	// VaultMintLead is the canonical vault-mint lead clause: mint returns a
	// one-time presigned PUT url bound to vault_path and has stored NO bytes
	// at that point. The toolforge vault_put_file mint description, the
	// capabilities vault_put_file completion contract, and the agent guide's
	// vault byte-route branch/detail all compose this fragment — a copy of
	// "one-time presigned ... bound to vault_path" or the not-stored-bytes
	// fact is always a drift regression.
	VaultMintLead = "a one-time presigned PUT url bound to vault_path (it has NOT stored bytes yet)"
)

// The upload-mint completion contract (upload_file(source.mode=mint)): the
// five neutral facts a completion statement carries — the one-time presigned
// HTTP PUT endpoint, the not-stored-bytes fact, the PUT action, the
// upload_status poll with the returned upload_handle, and the already-pinned
// completed CID (no pins_add). The toolforge upload_file mint description,
// the capabilities upload_file completion contract, and the agent guide's
// IPFS byte-route branch all COMPOSE these — no surface states a completion
// fact by hand.
const (
	// UploadMintEndpoint is the one-time-presigned-PUT-endpoint fact.
	UploadMintEndpoint = "a one-time presigned HTTP PUT endpoint"

	// UploadMintNoBytes is the not-stored-bytes fact (no subject — a surface
	// supplies its own: "Mint ..." or "it returns ... but ...").
	UploadMintNoBytes = "has NOT stored bytes"

	// UploadMintPutAction is the PUT action, with the canonical concrete
	// transport command (one edit point for the curl form).
	// The embedded curl command shape must stay the placeholder instance of
	// CurlUploadCommand: pinned byte-for-byte to the ONE builder in
	// TestUploadMintLeadFragmentsCanonical (contract_test.go), so the prose
	// canon and the runtime builder cannot drift in flags, placeholder, or
	// quoting.
	UploadMintPutAction = "PUT the agent-local file to the returned url (curl -sS -T <your-file> \"<url>\")"

	// UploadMintPoll is the completion wait loop: which tool to poll, with
	// what handle, and the terminal status to wait for.
	UploadMintPoll = "poll upload_status with the returned upload_handle until it reports completed"

	// UploadMintNoPinsAdd is the completed-surface contract: the completed CID
	// is already pinned, so no pins_add is needed.
	UploadMintNoPinsAdd = "the completed CID is already pinned, so pins_add is unnecessary"

	// The no-pins_add UPLOAD completion contract, shared by every tool that
	// pins an upload as a side effect (upload_file, upload_url, upload_data):
	// the returned CID is already pinned, so a follow-up pins_add is not
	// needed. The upload_url/upload_data toolforge preambles, the upload_file
	// toolforge preamble, and the agent guide's upload completion steps all
	// compose these — a hand copy of "the returned CID is already pinned, so
	// pins_add is ..." is always a drift regression.
	UploadReturnedCIDPinned = "the returned CID is already pinned"
	// UploadNoPinsAddNeeded is the follow-up-action fact: no pins_add after
	// the upload (the upload already pinned the content).
	UploadNoPinsAddNeeded = "pins_add is not needed afterward"
	// UploadPinnedCIDCompletion composes the full contract sentence body:
	// "the returned CID is already pinned, so pins_add is not needed
	// afterward" — lowercase opener, no punctuation; callers capitalize as
	// grammar requires via FirstUpper.
	UploadPinnedCIDCompletion = UploadReturnedCIDPinned + ", so " + UploadNoPinsAddNeeded

	// UploadMintPutPoll is the short-form upload-mint completion summary: the
	// PUT action followed by the upload_status poll WITH the returned
	// upload_handle until it reports completed. Surfaces that restate the
	// PUT/poll summary outside the full five-fact contract (the guide
	// summary's upload_mint branch, the capabilities byte-route chooser)
	// compose THIS fragment, never a hand-paraphrase — a paraphrase is
	// historically the drift that silently dropped the
	// poll-with-the-returned-handle-until-completed contract.
	UploadMintPutPoll = UploadMintPutAction + ", then " + UploadMintPoll
)

// The agent guide's numbered upload-mint steps, standardized on the canonical
// fragments: the numbered step copy is UploadMintPutAction/UploadMintPoll with
// the guide's fixed step numbers — never a hand-copy. A hand-copy already
// drifted once ("your agent-local file" vs the canonical "the agent-local
// file"); these constants make such drift impossible.
const (
	// UploadMintStepPut is numbered step 1: PUT the agent-local file to the
	// returned url (canonical curl command, symbolically).
	UploadMintStepPut = "1) " + UploadMintPutAction

	// UploadMintStepPoll is numbered step 2: poll upload_status with the
	// returned upload_handle until completed.
	UploadMintStepPoll = "2) " + UploadMintPoll
)

// The upload-mint runtime curl command. The PROSE canon above (UploadMintPutAction)
// embeds the command shape symbolically for the agent guide; the response
// builders (core/transfer/upload_file's mint branch and vault/vault_put_file's
// HTTP mint branch) need the CONCRETE command line with the minted URL
// substituted in. CurlUploadCommand is that ONE builder, so the two
// response builders' curl_command fields cannot drift apart in the
// placeholder, the flags, or the quoting.
func CurlUploadCommand(url string) string {
	return fmt.Sprintf("curl -sS -T <your-file> %q", url)
}

// DurabilitySourceWith composes DurabilitySource with an inline parenthesized
// qualifier after "durability on Sia" — e.g. DurabilitySourceWith("upload +
// pin") = "durability on Sia (upload + pin) happens in the background or via
// the vault_flush tool". An empty qualifier composes DurabilitySource
// unchanged. This is the ONE helper for surfaces that annotate the durability
// source inline, so they compose instead of hand-paraphrasing the clause.
func DurabilitySourceWith(qualifier string) string {
	if qualifier == "" {
		return DurabilitySource
	}
	return "durability on Sia (" + qualifier + ") " + durabilityMechanisms
}

// DurabilityFacts composes the canonical durability facts in sentence order:
// staged write → durability source (background or vault_flush, with its
// accepted-job shape) → the durability wait loop → the no-upload_status fact.
// It is the ONE copy every vault-mint statement composes.
const DurabilityFacts = StagedWrite + ", and " + DurabilityFollows + "; " + DurabilityPoll + "; " + NoUploadStatus

// DurabilityCanon composes the canonical durability paragraph used verbatim by
// the guide consumers (flow detail, decision branch) so every statement of the
// contract stays word-for-word identical. The summary composes DurabilityFacts
// under its own tool-scoped lead ("vault_put_file is non-blocking") and the
// tool descriptions compose it after their own mint lead-in. Consumers append
// their own sentence-final punctuation and, where the grammar needs a
// capitalized sentence opener, FirstUpper of this text.
const DurabilityCanon = NonBlockingLead + ": " + DurabilityFacts

// FirstUpper capitalizes the first byte of s so a lowercase-canonical fragment
// can open a sentence. The fragments are plain ASCII, so byte capitalization is
// safe; an empty string is returned unchanged.
func FirstUpper(s string) string {
	if s == "" {
		return s
	}
	return string(rune(upperByte(s[0]))) + s[1:]
}

func upperByte(b byte) byte {
	if b >= 'a' && b <= 'z' {
		return b - ('a' - 'A')
	}
	return b
}
