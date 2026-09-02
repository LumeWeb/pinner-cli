import { test, expect } from 'sunpeak/test';
import { invoke, textOf } from './helpers';

// This file MUST run serially in a single worker: the wizard FSM session is
// stateful (a session_id must be threaded through every websites_wizard_step
// call), and the complete happy-path drive CREATES a website in the shared
// fake store. sunpeak's base config sets `fullyParallel: true`, which gives
// every test its own worker + fresh module instance — that would break the
// single FSM session. Serial mode keeps the file's tests in order in one worker.
test.describe.configure({ mode: 'serial' });

/**
 * Wizard FSM session lifecycle (websites_*), driven through the
 * host-discovery contract: every call goes through the typed invoke dispatchers with
 * { name, args }, never by calling the direct tool name.
 *
 * CONTRACT (from internal/mcp/wizard/wizard.go, marshalWizardResponse at
 * :1079): the wizard start/step tools return a BARE StepResponse JSON object
 * (NOT the { status:'ok', value } envelope the REST-backed domain tools use).
 * The shape is { session_id, current_step, next_step?, next_step_schema?,
 * complete?, message?, error? }:
 *   - websites_wizard_start {}  -> { session_id, current_step:'auth_check',
 *                                  next_step, next_step_schema }
 *   - websites_wizard_step { session_id, input:{ <step fields> } } advances the
 *     FSM one state and returns the new current_step plus the NEXT step's input
 *     schema (next_step_schema). The step tool's arguments are { session_id,
 *     input } — see wizardStepSchema() (wizard.go:1062). Each step's `input`
 *     matches the CURRENT step's schema (the one the FSM is sitting on).
 *   - A step against an unknown/expired session_id returns isError=true with a
 *     StepResponse carrying { error, current_step:'' } — NOT the ok envelope.
 *
 * FSM states (websitesFSMEvents, wizard.go:299) give the happy-path order:
 *   init -> auth_check -> content_source -> target_type -> domain -> dns_mode
 *         -> create -> dns_setup -> validate -> complete
 * The documented step-input field names come from the input structs
 * (wizard.go:170-197) and were PROBED live via next_step_schema:
 *   auth_check    input {}                       (NoInput; auto-validates token)
 *   content_source input { choice:'cid', cid }   (schema: choice, cid)
 *   target_type   input { type:'ipfs' }          (schema: type)
 *   domain        input { domain:'<d>' }         (schema: domain)
 *   dns_mode      input { mode:'managed' }       (schema: mode)
 *   create        input { confirm:true }         (schema: confirm) — CREATES the
 *                                                   website via the fake APIs
 *   dns_setup     input {}                       (NoInput; informational)
 *   validate      input {}                       (schema: retry, optional)
 *   -> complete (complete:true fires)
 *
 * This file drives the real `pinner mcp` over stdio -> SDK -> fake API. It
 * was verified locally (`npx sunpeak test -c playwright.mcp-e2e.config.ts
 * wizard` — 6/6 passing across both host projects) and runs in CI via the
 * mcp-e2e config. The FSM contract is also covered on the Go side in
 * internal/mcp/wizard/wizard.go and wizard_test.go.
 *
 * STATE SAFETY: the website store in cmd/mcp-test-server's fake content API is
 * shared by BOTH host projects in a run ([chatgpt] and [claude] each spawn
 * their own `pinner mcp` against the SAME fake). To isolate this file's
 * create, it mints its OWN unique website domain at load time. The create step
 * is the LAST data-mutating action in the drive; no other file/project can
 * collide with it.
 */

// Mint a unique domain per module instance so each host project's happy-path
// creation stays isolated in the shared fake store (mirrors websites.test.ts).
const Domain = `wiz-${Math.random().toString(36).slice(2, 8)}.test`;
const Cid = `Qm${Math.random().toString(36).slice(2, 12)}`;

// The bare StepResponse JSON is returned as the tool's text content. The
// wizard tools do NOT wrap it in the { status, value } envelope, so parse the
// text to inspect the structural contract instead of relying on
// toHaveStructuredContent.
function stepResponse(result: Awaited<ReturnType<typeof invoke>>): Record<string, unknown> {
  return JSON.parse(textOf(result));
}

test('websites_wizard_start returns a session contract', async ({ mcp }) => {
  const result = await invoke(mcp, 'websites_wizard_start', {});

  expect(result).not.toBeError();

  // The start tool returns a bare StepResponse JSON object: session_id +
  // current_step + next_step + next_step_schema (NOT a {status,value} envelope).
  const resp = stepResponse(result);

  // Session contract: a non-empty opaque session handle plus a non-empty
  // current step the FSM is sitting on.
  expect(typeof resp.session_id).toBe('string');
  expect((resp.session_id as string).length).toBeGreaterThan(0);
  expect(typeof resp.current_step).toBe('string');
  expect((resp.current_step as string).length).toBeGreaterThan(0);

  // The first (and every non-terminal) step response advertises the next step
  // and its input schema, so the driving agent knows exactly what to pass.
  expect(typeof resp.next_step).toBe('string');
  expect(resp.next_step_schema).toBeTruthy();
});

test('websites_wizard_step with an unknown session returns an error', async ({ mcp }) => {
  // A step against a session the store has never minted (or that has expired)
  // fails closed: isError=true, no fake mutation, session not found.
  const result = await invoke(mcp, 'websites_wizard_step', {
    session_id: 'sess-does-not-exist',
    input: {},
  });

  expect(result).toBeError();

  // The failure still returns the StepResponse shape (session_id echoed back
  // next to a blank current_step), carrying the cause in `error`.
  const resp = stepResponse(result);
  expect(resp.session_id).toBe('sess-does-not-exist');
  expect(resp.error).toBeTruthy();
});

test('websites wizard happy path drives the full FSM to completion and creates a website', async ({ mcp }) => {
  // 1. Start a fresh session (auth_check).
  const started = stepResponse(await invoke(mcp, 'websites_wizard_start', {}));
  const sessionId = started.session_id as string;
  expect(sessionId.length).toBeGreaterThan(0);
  // The FSM begins on auth_check; next_step/schema are advertised for it.
  expect(started.current_step).toBe('auth_check');
  expect(started.next_step_schema).toBeTruthy();

  // Drive the wizard one step per FSM transition. Each intermediate response
  // advances current_step to the next state AND advertises next_step_schema.
  // The `input` on every call matches the CURRENT step's schema. The terminal
  // 'complete' state is reached on the final call.
  const steps: Array<{ input: Record<string, unknown>; expectStep: string }> = [
    // auth_check: auto-validates the seeded fixture token; no input needed.
    { input: {}, expectStep: 'content_source' },
    // content_source: CID is ready.
    { input: { choice: 'cid', cid: Cid }, expectStep: 'target_type' },
    // target_type: IPFS immutable addressing.
    { input: { type: 'ipfs' }, expectStep: 'domain' },
    // domain: the per-module unique domain.
    { input: { domain: Domain }, expectStep: 'dns_mode' },
    // dns_mode: Pinner manages DNS -> next step is `create` (the confirm gate).
    { input: { mode: 'managed' }, expectStep: 'create' },
    // create: irreversible; confirm:true creates the website via the fake.
    { input: { confirm: true }, expectStep: 'dns_setup' },
    // dns_setup: informational, no input.
    { input: {}, expectStep: 'validate' },
  ];

  for (const step of steps) {
    const resp = stepResponse(
      await invoke(mcp, 'websites_wizard_step', { session_id: sessionId, input: step.input }),
    );
    expect(resp).not.toHaveProperty('error');
    expect(resp.current_step).toBe(step.expectStep);
    // Every non-terminal transition advertises the next step's input schema.
    expect(resp.next_step).toBe(step.expectStep);
    expect(resp.next_step_schema).toBeTruthy();
  }

  // Final: validate (accept current status) -> complete.
  const done = stepResponse(
    await invoke(mcp, 'websites_wizard_step', { session_id: sessionId, input: {} }),
  );
  expect(done).not.toHaveProperty('error');
  expect(done.current_step).toBe('complete');
  // Terminal state: complete:true fires, no next step is advertised.
  expect(done.complete).toBe(true);
  expect(done.next_step).toBeUndefined();

  // Prove the wizard really created the website: the shared fake store must
  // now contain our unique domain (the create step went through the fake).
  const list = textOf(await invoke(mcp, 'websites_list', {}));
  expect(list).toContain(Domain);
});
