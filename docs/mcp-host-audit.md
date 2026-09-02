# Pinner MCP Host-Specific Audit Prompt

This document is the canonical procedure for auditing the Pinner MCP server's
tool-programming surface **as it is presented to a specific connected host**
(stdio, HTTP, OpenAI tunnel, Grok, etc.). It is the prompt used to audit hosts
for MCP regressions and host-specific behavior/intel. It complements the MCP
architecture overview in [`AGENTS.md`](../AGENTS.md) and the host-aware surface
built by `internal/mcp/hostenv` + `internal/mcp/toolforge`.

Read the full document before running an audit; it defines evidence types,
classification, intentional patterns, probe order, and the exact output
format.

---

You are the **live host connected to this Pinner MCP server**. Your job is to audit the tool-programming surface **as it is presented to YOU** — not as a generic MCP client, and not as some other model’s host.

Do not roleplay a different product. Do not assume ChatGPT, Claude, Cursor, Grok, or another host’s rules unless the **live profile, live tools, and wire-visible behavior** support that identity.

Your job is to determine whether the exposed Pinner surface describes **one coherent legal decision tree for this host**.

---

## 0. MISSION

Produce an audit a coding agent can implement.

Every finding must be supported by one or more of:

- **LIVE** evidence from this connection:
  - `capabilities`
  - `agent_guide`
  - `tools/list`
  - flattened/direct wrappers
  - `search_tools`
  - `describe_tool`
  - safe non-destructive invocation
- **REPO** evidence from the `pinner-cli` repository backing this server.
- **TEST** evidence from existing regression/unit/e2e tests.
- **INFERRED** evidence only when clearly labeled and derived from the above.

Every finding must be classified as one of:

1. **host-specific mismatch**
2. **generic bug affecting multiple/all hosts**
3. **deployment/version skew**
4. **intentional policy — do not “fix”**
5. **not observable from this host**

You are **not** being asked to hide tools merely because you dislike them.

You are being asked whether these surfaces describe the **same legal behavior**:

1. Capabilities JSON
2. Capabilities description
3. Direct / `tools/list` / flattened surface
4. Progressive catalog / `describe_tool` surface
5. `agent_guide` steps and decisions
6. `agent_guide` detail and tool policy prose

---

## 1. AUDIT BASELINE — RECORD THIS BEFORE FINDINGS

Before comparing behavior, record the audit baseline.

Capture, when observable:

- Live Pinner version/build/commit.
- Repository name.
- Repository branch/ref inspected.
- Repository HEAD SHA inspected.
- Timestamp of the live probe.
- Detected host identity.
- Detected transport.
- Whether the live deployment is known to match the inspected repo SHA.

Tag evidence as:

- `LIVE`
- `REPO`
- `TEST`
- `INFERRED`

### Version-skew rule

Never assume a live deployment equals moving repository HEAD.

If the repo already contains a fix but the live connection still shows the old behavior:

- classify it as **deployment/version skew** unless evidence proves the live server includes that fix;
- do **not** file a duplicate code bug merely because the live server is stale.

If the live version cannot be determined, say so.

---

## 2. HOW PINNER IS BUILT — DO NOT UNLEARN THESE SPLITS

Pinner compiles a per-host surface from multiple independent inputs:

- **Transport mechanism**
  - stdio
  - HTTP
  - OpenAI tunnel
- **Host capability flags**
  - file handoff
  - `x-mcp-file`
  - MCP Apps
  - elicitation
  - host-specific sibling relay tools where applicable
- **Registration-time wiring**
  - whether handlers are actually wired
- **Runtime host detection / overlays**
- **Dedicated per-host servers**
  - where used, descriptions may resolve for the detected host

### Critical distinctions

#### Mechanism ≠ capability

HTTP `upload_file` being mint-only does **not** imply the host lacks every other upload path.

Separate tools such as:

- `upload_url`
- `upload_data`

may exist independently.

#### `source_modes` ≠ complete upload surface

`capabilities.source_modes` describes the legal `source.mode` values for:

- `upload_file`
- `vault_put_file`

It is **not** necessarily the complete set of ways bytes can enter Pinner.

Use `capabilities.upload_tools`, tool registration, and tool descriptions to determine sibling upload routes.

#### `FeatSourceURL` / `FeatSourceData` may register sibling tools

On some hosts — notably Grok HTTP — these feature flags may mean:

- register `upload_url`
- register `upload_data`

They do **not** automatically widen:

```text
upload_file.source.mode
vault_put_file.source.mode
```

If the enum remains mint-only, that can be intentional.

#### `FeatElicitation` ≠ wizard existence

Website/domain/setup wizards use an in-band JSON state loop such as:

- `next_step`
- `next_step_schema`
- wizard step tools

Do **not** recommend gating wizard registration on MCP elicitation merely because a host lacks elicitation forms.

Elicitation may affect UX/orchestration, but not necessarily whether the wizard FSM is callable.

#### `FeatMCPApps` ≠ “some coordinator exists”

`open_*` iframe launchers require a host that can actually render MCP Apps.

Do not infer MCP Apps support solely because an open/coordinator tool exists.

#### Generic no-base64 policy stays generic

“Do not base64-encode a real file when a better route exists” is generic policy.

Do not delete it merely to make copy “host-specific.”

---

## 3. INTENTIONAL PATTERNS — DO NOT REOPEN WITHOUT EVIDENCE

Treat these as deliberate unless live/repo evidence contradicts them.

- No OpenAI `{file_id, download_url}` object on hosts without host file input capability.
- Mint return is the **start** of upload, not completion.
- A mint flow requires:
  1. mint
  2. out-of-band PUT
  3. `upload_status`
- `sink=local` writes to the MCP server’s disk.
- Remote agents should prefer `sink=drop` when available.
- `pins_add` is for external CIDs, not a mandatory post-upload step.
- Generic “publish this” / “create me a website” must not invent a label.
- Generic publish should not automatically start a wizard unless the user asked for a guided flow.
- Progressive discovery and flattened/direct wrappers may coexist intentionally.
- Visible wizard tools are not inherently defects.
- Visible relay tools are not inherently defects when their registration is intentional and their descriptions explain when to use them.

---

## 4. DETECT YOURSELF FIRST

Before findings, establish the live identity.

### 4.1 Call capabilities

Call `capabilities` or the flattened equivalent.

Record all observable fields, especially:

- `transport`
- `source_modes`
- `upload_tools`
- `download_sink_modes`
- `host_file_input`
- `host_file_input_preferred`
- `file_input_policy`
- `draft_x_mcp_file`
- `upload_file`
- `vault_put_file`
- `download_file`
- `vault_get_file`
- relay limits if present

### 4.2 Call agent_guide

Record:

- summary
- every flow
- every flow’s steps
- every decision
- every decision branch
- detail
- global rules

### 4.3 Inspect important tools

Describe or otherwise inspect, when registered:

- `upload_file`
- `vault_put_file`
- `upload_url`
- `upload_data`
- `upload_status`
- `download_file`
- `vault_get_file`
- `websites_create`
- `websites_validate`
- `websites_wizard_start`
- `websites_wizard_step`
- `capabilities`

Do not assume optional tools exist.

### 4.4 Inspect discovery surfaces

Use the actual schema exposed by this host.

Probe relevant catalog/search surfaces such as:

- help/onboarding
- upload
- vault
- wizard
- website

List flattened/direct wrappers that are callable without progressive discovery.

### 4.5 Inspect repository evidence when available

Read:

- the host profile for this host
- host detection
- feature declarations
- custom tool registration
- capabilities builder
- upload/vault schema transforms
- `agent_guide`
- wizard implementation
- latest relevant `fix(mcp)` / `feat(mcp)` commits
- existing regression tests for the suspected behavior

Believe live schema over stale comments.

Believe registration code over comments describing intended registration.

### 4.6 Identity output

Write one concise identity block containing:

- Host name you believe you are:
  - Grok HTTP
  - ChatGPT/OpenAI tunnel
  - OpenAI HTTP
  - generic HTTP
  - stdio
  - unknown
- Evidence supporting that identity.
- What Pinner/server accepts.
- What this host can actually provide or execute.
- What you MUST NOT invent.

Include confidence:

```text
identity_confidence: high | medium | low
```

If identity remains uncertain, say so and audit the **live surface anyway**.

---

## 5. SERVER CAPABILITY ≠ HOST EXECUTION CAPABILITY

Audit these separately.

Example:

```text
capabilities.source_modes = ["mint"]
```

proves that Pinner supports the mint protocol on that transport.

It does **not** by itself prove the current host can:

- read an agent-local file,
- issue an arbitrary HTTP PUT,
- access the returned upload URL,
- attach the correct bytes,
- or poll the upload afterward.

Record both sides where relevant:

```text
server_accepts_mint: yes | no
host_can_execute_put: yes | no | unknown
host_can_access_agent_local_file: yes | no | unknown
```

If the server supports mint but the host cannot perform the PUT, report a:

**host integration gap**

Do not call it a schema bug unless the schema falsely claims the host can perform the missing action.

---

## 6. THE SIX SURFACES THAT MUST AGREE

For every important flow — especially upload, vault upload, download, and website publish — compare all six.

---

### A. Capabilities JSON

Check:

- `source_modes`
- `upload_tools`
- sink modes
- host file flags
- upload/vault/download booleans
- file input policy

Rules:

- `source_modes` must match the enum accepted by `upload_file` / `vault_put_file` for that transport.
- `upload_tools` must reflect tools actually registered on this server.
- Registration-time wiring matters.
- A tool must not be advertised if its handler was not registered.
- A mode must not be advertised if the schema rejects it.

A mismatch here is a structured-reader defect.

---

### B. Capabilities DESCRIPTION

The description must explain the jobs of the machine-readable fields.

It must distinguish:

- `source_modes`
- `upload_tools`
- `download_sink_modes`

It must not imply:

> `source_modes` are the complete byte path

when sibling relay tools exist.

### First-clause rule

If the first strong sentence says something false, a later correction does not erase the defect.

Models often act on the first operational instruction.

---

### C. DIRECT / TOOLS-LIST / FLATTENED SURFACE

Audit:

- direct tools
- flattened wrappers
- baked `tools/list` descriptions
- baked schemas
- direct-visible registrations

This is a separate conformance surface.

A direct tool may be callable even when progressive discovery also exists.

Do not flag dual dispatch by itself.

Flag contradictions such as:

- direct tool is registered but baked copy says “do not call”
- direct schema differs from resolved `describe_tool` schema in legal values
- flattened wrapper exposes a call that catalog guidance claims does not exist

---

### D. PROGRESSIVE CATALOG / DESCRIBE_TOOL SURFACE

Audit:

- `search_tools`
- `describe_tool`
- resolved host-specific schema
- resolved host-specific descriptions

C and D may differ in presentation.

They must **not** disagree about:

- tool existence
- enum legality
- required arguments
- host file support
- whether the tool should or should not be called
- whether a sibling tool is the correct route

A direct/baked copy contradiction with resolved `describe_tool` is a real defect.

---

### E. AGENT_GUIDE STEPS AND DECISIONS

Models often follow steps and decisions before reading detail.

Every named MCP tool step must resolve to a real tool on this surface.

### Approved out-of-band pseudo-step

The explicit step:

```text
<host PUT>
```

is allowed.

It is **not** an MCP tool.

Do not pass `<host PUT>` to:

- `describe_tool`
- the typed invoke dispatchers (`invoke_read_tool` / `invoke_write_tool` / `invoke_destructive_tool`)

It represents the host action of PUTing bytes to the minted URL.

For mint upload flows, the correct chain is:

```text
upload_file
→ <host PUT>
→ upload_status
```

For mint vault flows:

```text
vault_put_file
→ <host PUT>
→ upload_status
```

No other fabricated pseudo-tool names are allowed unless the guide explicitly defines them as out-of-band actions.

### Decision rule

Every branch must be legal for THIS host.

Examples:

- public URL branch must not name `upload_url` if that tool is not registered;
- vault URL/data branches must not exist on a transport where `vault_put_file` rejects those source modes.

---

### F. AGENT_GUIDE DETAIL AND TOOL POLICY PROSE

Policy may be richer than steps.

It must not contradict:

- schema
- capabilities
- registration
- steps
- decision branches

Important policy includes:

- no invented file object
- no unnecessary base64
- mint is incomplete until PUT + status
- no `pins_add` after normal upload
- vault ≠ IPFS relay
- no invented website label
- wizard vs autonomous publish
- download local vs drop semantics

---

## 7. BYTE-ROUTE RULES

Use this generic order **only when the relevant routes actually exist**.

### Route 1 — host-native file object

Only when:

- `host_file_input=true`
- schema contains a top-level `file` argument
- the host can actually produce the required file object

Then prefer the host file argument according to `file_input_policy`.

Never invent:

- `file_id`
- `download_url`
- OpenAI-style file objects

when the host cannot provide them.

### Route 2 — agent-local / sandbox file

When `source.mode=mint` is legal **and this host can perform the PUT**:

```text
upload_file(source.mode=mint)
→ receive url + upload_handle
→ <host PUT> bytes to url
→ poll upload_status
```

For vault:

```text
vault_put_file(source.mode=mint, vault_path=...)
→ <host PUT>
→ upload_status
```

Treat mint return as upload initiation, not success.

### Route 3 — public HTTPS URL

When `upload_url` is actually registered and allowed:

```text
upload_url
```

Use it for bytes already available at a public HTTPS URL.

Do not download them merely to re-upload by mint unless required by the destination flow.

### Route 4 — raw inline bytes

When `upload_data` is actually registered and allowed:

```text
upload_data
```

Use only when there is:

- no usable file
- no usable public URL

This is a last resort.

Do not base64-encode a real file that could use a better route.

### Specialization rules

- Missing tool → drop the branch.
- Present tool + forbid copy → investigate registration/copy mismatch.
- Intentional registration → demand a clear when-clause; do not reflexively recommend hiding.
- Mint-only `upload_file` plus visible `upload_url` does **not** mean `upload_file.source.mode` should gain `url`.

---

## 8. VAULT ≠ IPFS

`upload_url` and `upload_data` pin to IPFS.

They do not write the vault unless an actual documented vault-from-CID capability exists.

Do not invent one.

### Mint-only vault

Legal local-file path:

```text
vault_put_file(source.mode=mint, vault_path=...)
→ <host PUT>
→ upload_status
```

### URL / inline data destined for vault

If `vault_put_file` does not accept URL/data directly:

1. Determine whether this host has a legal way to materialize those bytes to an agent-local file.
2. If yes:
   - materialize
   - use mint
   - PUT
   - poll status
3. If no:
   - report **no executable vault route on this host**
   - do not pretend the IPFS relay wrote the vault

### Tunnel case

If `vault_put_file` truly accepts URL/data in its own enum on the OpenAI tunnel, those branches are valid **there only**.

---

## 9. WEBSITE PUBLISH RULES

Audit website publishing separately from wizard onboarding.

### Generic publish

For requests like:

- “publish this”
- “host this”
- “create me a website”

do not invent:

- label
- custom domain
- platform root preference

The generic path should use platform auto-generation when supported.

### Explicit label

Only enter label-specific availability/claim logic when the user explicitly supplied or requested a label.

### Custom domain

Only enter custom-domain flow when the user actually supplied or owns a custom domain.

### Wizard

Wizard tools may remain visible.

Do not treat visibility as proof they should be used for every publish.

Audit whether descriptions clearly distinguish:

- guided human-in-the-loop onboarding
- autonomous publish

---

## 10. WIZARD ≠ ELICITATION

Do not assume wizard tools require MCP elicitation.

Before claiming elicitation is required, inspect:

- wizard registration
- wizard input schema
- `next_step`
- `next_step_schema`
- step tool behavior
- relevant tests

If the wizard progresses entirely through in-band JSON calls, absence of elicitation does not by itself make it unusable.

If elicitation merely improves UX, classify that separately from technical callability.

---

## 11. LANGUAGE DEFECTS THAT MATTER TO MODELS

Report these when observed.

### Contradictory lead sentence

Bad:

> “This host cannot use URL relay…”

followed later by:

> “…but use upload_url here.”

The first operational clause wins too often.

### Host-wide claim for a tool-scoped restriction

Bad:

> “This host’s only byte path is mint.”

when only:

```text
upload_file.source.mode
```

is mint-only.

Prefer:

> “The only `source.mode` this tool accepts on this transport is `mint`.”

### Shared-copy leaks

Investigate copy like:

- “prefer the file parameter”
- “no curl needed”
- OpenAI file-object language
- `x-mcp-file`
- “tunnel transport”
- `source.mode=path`

when the current host/schema cannot use those features.

### Missing mint completion

Flag steps that stop at:

```text
upload_file(source.mode=mint)
```

without:

```text
<host PUT>
upload_status
```

### Vault sibling leak

Flag `vault_put_file` copy that steers URL/data to:

- `upload_url`
- `upload_data`

when those tools only pin to IPFS.

### Generic website label invention

Flag descriptions that encourage label availability checks for a generic publish request.

### Wizard ambiguity

Flag wizard start copy that makes generic publishing look wizard-only when the normal publish flow is legal.

---

## 12. DO NOT REPORT THESE AS DEFECTS BY THEMSELVES

- Long but internally consistent website policy.
- `upload_data` last-resort wording.
- Visible wizard tools.
- Flattened + progressive dual dispatch.
- Host-specific descriptions that differ only in presentation.
- A mint-only `upload_file` on Grok alongside separate `upload_url` / `upload_data`.
- Generic no-base64 policy.
- Absence of OpenAI file-object support on Grok.
- Lack of MCP Apps on a host that never negotiated Apps.

---

## 13. PROBE PROCEDURE

Run the audit from the **current live connection**, not memory.

### Required

1. Call `capabilities`.
2. Call `agent_guide`.
3. Inspect all registered tools relevant to:
   - upload
   - vault
   - download
   - website publish
   - wizard
4. Inspect both:
   - direct / flattened surface
   - progressive discovery surface
5. Compare schemas and descriptions.
6. Inspect the backing repo if available.
7. Inspect relevant tests before filing a bug.
8. Inspect latest relevant MCP commits when version skew is possible.

### Conditional

Only invoke a wizard if necessary to establish a claim about runtime behavior.

Prefer:

1. schema
2. implementation
3. tests
4. safe invocation

before creating wizard session state.

If a safe abort/cleanup path exists and you start a wizard solely for probing, use it.

### Never

Do not:

- call destructive tools merely to test them;
- remove pins;
- remove vault content;
- dump secrets;
- quote tokens;
- expose `dev_*` secret values;
- fabricate arguments to force an error when schema inspection answers the question.

---

## 14. CHECK EXISTING REGRESSION TESTS BEFORE FILING

Before declaring a P0/P1 defect:

1. Search for an existing test covering the behavior.
2. Read the test at the audited repo SHA.
3. Determine whether:
   - the test already asserts the desired behavior;
   - the live deployment is stale;
   - the test is incomplete;
   - the test passes but another surface is untested.

If the test already covers the exact desired behavior and repo HEAD contains the fix, prefer:

**deployment/version skew**

over filing a duplicate production patch.

---

## 15. CLASSIFY EVERY FINDING

Every finding must include:

### ID + title

Example:

```text
P1-03 — Grok direct upload_url copy contradicts registered capability
```

### Status

One of:

- `FAIL`
- `PASS`
- `NOT OBSERVABLE`
- `VERSION SKEW`
- `CHANGED BY DESIGN`

### Surfaces involved

Use:

- A — capabilities JSON
- B — capabilities description
- C — direct / tools-list / flattened
- D — progressive / describe_tool
- E — agent_guide steps / decisions
- F — agent_guide detail / policy

### Evidence

Include short live/repo/test quotes.

Tag each quote:

```text
[LIVE]
[REPO]
[TEST]
```

### Impact

Explain exactly how a model like YOU could make the wrong call.

### Concrete patch

Name:

- file
- function / descriptor / schema transform
- sentence or branch to change

### Regression test

For every P0/P1:

- name the existing test to extend, or
- propose a new test name;
- state the important assertions;
- state at least one positive host and one negative host where applicable.

### Scope

One of:

- host-specific
- generic
- deployment/version skew
- intentional-do-not-touch

### Priority

- **P0** — schema/guide contradiction causes illegal or destructive/wrong-destination call
- **P1** — legal route missing or host sent to wrong tool
- **P2** — stale copy, misleading comment, duplicate sentence, lower-risk discoverability defect

---

## 16. NEVER RECOMMEND THESE WITHOUT DIRECT EVIDENCE

Do not recommend:

- adding host file input to a host that cannot construct the file object;
- adding URL/data to `upload_file.source.mode` merely because sibling relay tools exist;
- gating wizard registration on `FeatElicitation`;
- removing last-resort / do-not-base64 policy as “ChatGPT copy”;
- hiding an intentionally registered relay instead of fixing its when-clause;
- enabling MCP Apps merely because an `open_*` tool exists;
- routing vault writes through IPFS-only `upload_url` / `upload_data`;
- treating mint return as completed upload;
- inventing website labels for generic publish.

---

## 17. REGRESSION GUARDS

Score these explicitly.

| Guard | Expected invariant |
|---|---|
| Host file object | Only when host + schema can actually provide it |
| Mint enum | HTTP upload/vault source enum stays transport-derived |
| `draft_x_mcp_file` | Must match host capability |
| Upload chooser in steps | Steps/decisions expose every legal byte route |
| Mint completion | `<host PUT>` + `upload_status` present |
| Vault ≠ IPFS | Relay tools never masquerade as vault writes |
| Wizard ≠ elicitation | Wizard callability not falsely tied to elicitation |
| Direct vs describe | No baked/direct contradiction with resolved catalog |
| Registration vs capabilities | `upload_tools` matches tools actually registered |
| Generic publish | No invented label |
| Apps | Launchers only treated as usable when host can render Apps |

For each, report:

```text
PASS
FAIL
NOT OBSERVABLE
VERSION SKEW
CHANGED BY DESIGN
```

Do not force a defect where none exists.

---

## 18. OUTPUT FORMAT

Return exactly these sections.

### 1. Audit baseline

Include:

- live build/version if known
- repo ref/SHA
- probe timestamp
- whether live/repo parity is known

### 2. Identity

One concise block:

- host
- transport
- identity confidence
- server byte capabilities
- host execution capabilities
- what must not be invented

### 3. Regression guard scorecard

Use the guards from §17.

### 4. Findings

Only material mismatches or actionable gaps.

No essay.

For every finding include all fields from §15.

### 5. Leave alone

List intentional behavior the coding agent must preserve.

### 6. Done if

Give concrete acceptance checks the coding agent can re-probe.

Acceptance checks should be specific enough to run after a patch.

---

## 19. DONE-IF ACCEPTANCE PRINCIPLES

A patch is done only when the affected host can be re-probed and all relevant surfaces agree.

Typical checks:

- capabilities enum matches schema enum;
- `upload_tools` matches registration;
- direct and progressive descriptions agree on legality;
- mint flows show PUT + status;
- relay tools appear only when registered;
- Grok URL/data relays do not widen `upload_file.source.mode`;
- Grok vault does not route through IPFS-only relays;
- tunnel vault URL/data branches exist only where the schema accepts them;
- generic website publish does not invent a label;
- wizard callability is not incorrectly tied to elicitation;
- every named MCP guide step resolves to a registered tool;
- `<host PUT>` is treated only as an approved out-of-band action;
- existing regression tests pass;
- new regression tests cover the fix.

---

## 20. TONE

Be skeptical of shared copy.

Prefer:

- live schema over comments;
- registration over assumptions;
- tests over memory;
- omission of a false claim over a later sentence that cancels it;
- steps/decisions over detail when predicting model behavior;
- tool-scoped wording over host-wide wording;
- explicit version-skew classification over duplicate bug reports.

Known historical failure patterns worth hunting first:

- generic description tails overwriting host-specific tails;
- feature flags being overloaded;
- sibling relay features accidentally widening source enums;
- choosers existing only in detail and not steps;
- direct/baked descriptions diverging from resolved `describe_tool`;
- capabilities reporting tools from request profile rather than registration state;
- vault inheriting upload sibling pointers;
- mint flows omitting PUT or completion polling;
- generic publish inventing a label;
- wizard UX being confused with elicitation capability.

If nothing material remains, say the surface is coherent.

Do not invent work merely because this is an audit.
