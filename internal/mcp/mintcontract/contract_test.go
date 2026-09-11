package mintcontract

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestDurabilityCanonComposesFragmentsOnly pins that the canonical durability
// paragraph is EXACTLY the fragment composition — no second hand-written copy
// of the staging clause, the flush job shape, the polling loop, or the
// no-upload_status fact hides inside it.
func TestDurabilityCanonComposesFragmentsOnly(t *testing.T) {
	require.Equal(t,
		"the vault write is non-blocking: "+StagedWrite+", and "+DurabilitySource+" ("+FlushJobShape+"); "+DurabilityPoll+"; "+NoUploadStatus,
		DurabilityCanon)
	require.Equal(t, DurabilityCanon, NonBlockingLead+": "+DurabilityFacts)
	require.Equal(t, "accepted job { job_id, profile, path? }", FlushAcceptedJob)
	require.Equal(t, "vault_flush is non-blocking and returns an "+FlushAcceptedJob, FlushJobShape)
	require.Equal(t, "there is no upload_status to poll (upload_status tracks upload_file's IPFS uploads, not vault writes)", NoUploadStatusWhy)
	// DurabilityPollCore is the wait loop's core; DurabilityPoll composes it
	// with the sharing precondition (one edit point for the poll targets).
	require.Equal(t, "poll vault_flush_status(job_id) or vault_stat until status: durable", DurabilityPollCore)
	require.Equal(t, DurabilityPollCore+" when durability is needed before sharing", DurabilityPoll)
	// NoUploadStatusWhy composes NoUploadStatus plus UploadStatusContrast, so
	// the contrast parenthetical (hand-copied into tool descriptions too) has
	// exactly one source.
	require.Equal(t, NoUploadStatus+" "+UploadStatusContrast, NoUploadStatusWhy)
}

// TestDurabilitySourceWithQualifier pins the qualifier helper: the annotated
// durability-source clause stays a single composition (no hand-paraphrase at
// the annotation sites), an empty qualifier collapses to DurabilitySource, and
// both spellings keep the same mechanisms clause.
func TestDurabilitySourceWithQualifier(t *testing.T) {
	require.Equal(t, DurabilitySource, DurabilitySourceWith(""))
	require.Equal(t,
		"durability on Sia (upload + pin) happens in the background or via the vault_flush tool",
		DurabilitySourceWith("upload + pin"))
	require.Equal(t, DurabilitySourceWith("upload + pin"),
		"durability on Sia (upload + pin) "+durabilityMechanisms)
}

// TestVaultFlushTriageIsCanonical pins the flush-triage fragment's shape: one
// complete sentence covering all three vault_stat states (flushing, failed,
// never started), so every triage surface composes the same single edit point.
func TestVaultFlushTriageIsCanonical(t *testing.T) {
	require.Contains(t, VaultFlushTriage, "vault_stat's flush_started_at, flush_attempts and flush_error")
	require.Contains(t, VaultFlushTriage, "a flushing file shows")
	require.Contains(t, VaultFlushTriage, "a failed file shows")
	require.Contains(t, VaultFlushTriage, "a staged file that never started shows")
	require.Contains(t, VaultFlushTriage, "compare now against flush_started_at to tell a long host upload from a hung pin")
	require.True(t, strings.HasSuffix(VaultFlushTriage, "."), "triage is a complete sentence")
}

// TestFragmentsAreNeutral pins the neutralness guarantees callers compose
// against: lowercase openers (so mid-sentence composition reads correctly),
// no lead-in tool args, no trailing punctuation.
func TestFragmentsAreNeutral(t *testing.T) {
	for name, fragment := range map[string]string{
		"StagedWrite":       StagedWrite,
		"DurabilitySource":  DurabilitySource,
		"DurabilityFollows": DurabilityFollows,
		"DurabilityPoll":    DurabilityPoll,
		"NoUploadStatus":    NoUploadStatus,
		"NonBlockingLead":   NonBlockingLead,
	} {
		require.NotEqual(t, 0, len(fragment), "%s must not be empty", name)
		require.False(t, strings.HasSuffix(fragment, "."), "%s must not carry sentence punctuation", name)
		require.False(t, fragment[0] >= 'A' && fragment[0] <= 'Z',
			"%s must keep a lowercase opener so mid-sentence composition reads as a clause", name)
	}
	require.NotContains(t, StagedWrite, "source.mode", "fragments must not embed consumer-specific args")
}

// TestFirstUpperCapitalizesOpener pins the sentence-opener capitalization
// helper consumers use: only the first byte changes, ASCII-safe, empty-safe.
func TestFirstUpperCapitalizesOpener(t *testing.T) {
	require.Equal(t, "The vault write is non-blocking", FirstUpper(NonBlockingLead))
	require.Equal(t, "The PUT returns quickly", FirstUpper("the PUT returns quickly"))
	require.Equal(t, "", FirstUpper(""))
	// Non-letter first bytes are unchanged (a fragment starting with a digit
	// or symbol must not be mangled).
	require.Equal(t, "3-stage write", FirstUpper("3-stage write"))
}

// TestUploadMintLeadFragmentsCanonical pins the upload-mint completion
// contract's five neutral fragments: the one-time presigned HTTP PUT
// endpoint, the not-stored-bytes fact, the PUT action (with its canonical
// concrete transport command), the upload_status poll with the returned
// upload_handle, and the already-pinned completed CID (no pins_add). The
// five fragments are the contract; no surface states a completion fact by
// hand (the parity test in the parent audit_findings_test.go pins the three
// live compositions).
func TestUploadMintLeadFragmentsCanonical(t *testing.T) {
	require.Equal(t, "a one-time presigned HTTP PUT endpoint", UploadMintEndpoint)
	require.Equal(t, "has NOT stored bytes", UploadMintNoBytes)
	require.Contains(t, UploadMintPutAction, "PUT the agent-local file to the returned url")
	// The prose canon's curl command is the placeholder instance of the ONE
	// CurlUploadCommand builder (normalized from the drifted "<file>"
	// placeholder): flags, placeholder, and quoting are pinned to the builder,
	// so a builder change without a prose update is a build failure.
	require.Equal(t,
		"PUT the agent-local file to the returned url ("+CurlUploadCommand("<url>")+")",
		UploadMintPutAction)
	require.Equal(t,
		"poll upload_status with the returned upload_handle until it reports completed",
		UploadMintPoll)
	require.Equal(t,
		"the completed CID is already pinned, so pins_add is unnecessary",
		UploadMintNoPinsAdd)

	// UploadMintPutPoll is the canonical short-form PUT-plus-poll summary and
	// MUST be the fragment composition (PUT action + the poll WITH the
	// returned upload_handle until completed). The guide summary's
	// upload_mint branch and the capabilities byte-route chooser compose this
	// instead of paraphrasing — a dropped "with the returned upload_handle"
	// here would silently weaken every consuming surface at once.
	require.Equal(t, UploadMintPutAction+", then "+UploadMintPoll, UploadMintPutPoll)
	require.Contains(t, UploadMintPutPoll, "the returned upload_handle",
		"the short-form PUT-plus-poll summary must keep the handle contract")
}

// TestUploadCompletionNoPinsAddCanonical pins the no-pins_add UPLOAD
// completion contract: the returned CID is already pinned (follow-up
// resolution) and no pins_add is needed afterward. Every surface that states
// the upload completion contract — the upload_file/upload_url/upload_data
// toolforge preambles and the agent guide's completion steps — composes these
// ONE fragments; a hand copy is always a drift regression.
func TestUploadCompletionNoPinsAddCanonical(t *testing.T) {
	require.Equal(t, "the returned CID is already pinned", UploadReturnedCIDPinned)
	require.Equal(t, "pins_add is not needed afterward", UploadNoPinsAddNeeded)
	require.Equal(t,
		"the returned CID is already pinned, so pins_add is not needed afterward",
		UploadPinnedCIDCompletion)
	// And the composed contract is exactly the two facts — no surface may
	// paraphrase one side away.
	require.Equal(t, UploadReturnedCIDPinned+", so "+UploadNoPinsAddNeeded, UploadPinnedCIDCompletion)
}

// TestCurlUploadCommandPinned pins the ONE runtime curl_command builder the
// two mint response builders (core/transfer/upload_file's mint branch and
// vault/vault_put_file's HTTP mint branch) compose: the shared command shape
// with the concrete minted URL substituted — never a per-surface
// fmt.Sprintf copy (the placeholder, flags, and quoting must stay identical).
func TestCurlUploadCommandPinned(t *testing.T) {
	require.Equal(t, `curl -sS -T <your-file> "https://mcp.example.test/put?sig=abc"`,
		CurlUploadCommand("https://mcp.example.test/put?sig=abc"))
	// Quoting is the shell-safe %q form, so URLs with quotes survive.
	require.Equal(t, `curl -sS -T <your-file> "https://x/y?a=1&b=2"`,
		CurlUploadCommand("https://x/y?a=1&b=2"))
}

// TestUploadMintNumberedStepsComposeFragments pins the agent guide's numbered
// upload-mint steps: they are the standardized numbers over the canonical
// fragments (UploadMintPutAction/UploadMintPoll), never hand copies — the
// previous hand copy had drifted ("your agent-local file").
func TestUploadMintNumberedStepsComposeFragments(t *testing.T) {
	require.Equal(t, "1) "+UploadMintPutAction, UploadMintStepPut)
	require.Equal(t, "2) "+UploadMintPoll, UploadMintStepPoll)
	require.Equal(t,
		"1) PUT the agent-local file to the returned url ("+CurlUploadCommand("<url>")+")",
		UploadMintStepPut)
	require.Equal(t,
		"2) poll upload_status with the returned upload_handle until it reports completed",
		UploadMintStepPoll)
}

// TestVaultMintLeadCanonical pins the canonical vault-mint lead clause: the
// one-time presigned PUT url bound to vault_path and the not-stored-bytes-yet
// fact in ONE fragment. It stays neutral (lowercase opener, no consumer
// subject, no args, no trailing punctuation) so the toolforge description,
// the capabilities completion contract, and the guide's vault byte-route
// detail all compose it mid-sentence.
func TestVaultMintLeadCanonical(t *testing.T) {
	require.Equal(t,
		"a one-time presigned PUT url bound to vault_path (it has NOT stored bytes yet)",
		VaultMintLead)
	require.False(t, strings.HasSuffix(VaultMintLead, "."), "lead must not carry sentence punctuation")
	require.NotContains(t, VaultMintLead, "mint returns", "lead must not embed a consumer subject")
}
