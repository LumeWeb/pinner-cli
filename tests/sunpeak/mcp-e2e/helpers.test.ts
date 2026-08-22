import { test, expect } from 'sunpeak/test';
import { isCleanSuccess } from './helpers';

/**
 * Regression test for isCleanSuccess (the "clean MCP result" classifier used
 * across the whole fake-API e2e suite).
 *
 * WHY THIS EXISTS: isCleanSuccess used a naive substring regex
 * (/authenticat|401|unauthor|connection refused/) to detect auth/network
 * failures. That regex false-positived on INNOCUOUS text that legitimately
 * appears inside successful payloads:
 *   - a nanosecond timestamp like "...21:29:40.024015512Z" contains a "401"
 *     digit run (in "0240 4 0 15512" → "4015"), so the bare `/401/` matched and
 *     `isCleanSuccess` returned false for an otherwise-successful result — this
 *     is exactly what flakily broke websites/ipns/resources lists in CI.
 *   - the word "authenticated" (e.g. auth_status -> { authenticated: true })
 *     tripped the `/authenticat/` substring.
 *
 * The current implementation is context-aware: it only treats word-bounded
 * `401`, `unauthor(ized)`, `connection refused`, `authentication failed/...`,
 * and `not authenticated` as failures. These cases lock that contract so a
 * future naive-regex regression is caught immediately rather than surfacing as
 * a mysterious CI flake.
 */
test.describe('isCleanSuccess', () => {
  const okText = (text: string) => ({ content: [{ type: 'text', text }] });
  const errText = (text: string) => ({ content: [{ type: 'text', text }], isError: true });

  test('accepts success payloads whose data innocuously contains "401" in a timestamp', () => {
    // Prior bug: the bare /401/ matched the "401" inside this nanosecond
    // timestamp and wrongly classified the result as failed.
    const result = okText(
      JSON.stringify({
        status: 'ok',
        value: { created_at: '2026-08-22T21:29:40.024015512Z', cid: 'bafy...' },
      }),
    );
    expect(isCleanSuccess(result)).toBe(true);
  });

  test('accepts success payloads containing the word "authenticated"', () => {
    // Prior bug: /authenticat/ matched "authenticated" and wrongly flagged it.
    const result = okText(JSON.stringify({ status: 'ok', value: { authenticated: true } }));
    expect(isCleanSuccess(result)).toBe(true);
  });

  test('accepts a result carrying no failure markers', () => {
    expect(isCleanSuccess(okText('{"status":"ok","value":{}}'))).toBe(true);
  });

  test('rejects an isError-flagged result', () => {
    expect(isCleanSuccess(errText('something broke'))).toBe(false);
  });

  test('rejects a word-bounded 401 status code', () => {
    expect(isCleanSuccess(okText('{"status":401,"error":"Unauthorized"}'))).toBe(false);
    expect(isCleanSuccess(okText('401 Unauthorized'))).toBe(false);
  });

  test('rejects unauthorized / unauthenticated wording', () => {
    expect(isCleanSuccess(errText('unauthorized'))).toBe(false);
    expect(isCleanSuccess(okText('unauthorized'))).toBe(false);
    expect(isCleanSuccess(okText('not authenticated'))).toBe(false);
  });

  test('rejects authentication failure phrasing', () => {
    expect(isCleanSuccess(okText('authentication failed: not authenticated'))).toBe(false);
    expect(isCleanSuccess(okText('authentication required'))).toBe(false);
  });

  test('rejects network connection refusal', () => {
    expect(isCleanSuccess(errText('connection refused'))).toBe(false);
  });
});
