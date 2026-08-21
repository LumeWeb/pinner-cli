import { test, expect, request } from '@playwright/test';
import { createHash, randomBytes } from 'node:crypto';

// Raw HTTP tests for the full RFC 9728 OAuth flow of `pinner mcp --http
// --oauth`. The server requires every /mcp call to carry an OAuth access token;
// we drive the entire handshake over raw HTTP to prove the authorization-server
// surface works end-to-end:
//   1. discovery metadata       (oauth-authorization-server, protected-resource)
//   2. dynamic client registration
//   3. authorization-code + PKCE (S256) exchange, secret-as-password login
//   4. token exchange -> access_token
//   5. protected /mcp call with the token; 401 + OAuth challenge without it

const BASE = 'http://127.0.0.1:8124';
const RESOURCE = `${BASE}/mcp`;
const SECRET = process.env.PINNER_TEST_SECRET ?? 'sunpeak-test-secret';
const REDIRECT = 'http://localhost/cb';
const CLIENT = 'pinner-sunpeak-e2e';

function pkce() {
  const verifier = randomBytes(32).toString('base64url');
  const challenge = createHash('sha256').update(verifier).digest('base64url');
  return { verifier, challenge };
}

test('serves OAuth discovery metadata with the required fields', async () => {
  const ctx = await request.newContext({ baseURL: BASE });

  const as = await (await ctx.get('/.well-known/oauth-authorization-server')).json();
  expect(as.authorization_endpoint).toBe(`${BASE}/oauth/authorize`);
  expect(as.token_endpoint).toBe(`${BASE}/oauth/token`);
  expect(as.registration_endpoint).toBe(`${BASE}/oauth/register`);
  expect(as.grant_types_supported).toContain('authorization_code');

  const pr = await ctx.get('/.well-known/oauth-protected-resource');
  expect(pr.status()).toBe(200);
  const prj = await pr.json();
  expect(prj.resource).toContain('/mcp');
  await ctx.dispose();
});

test('completes the full OAuth authorization-code flow and calls /mcp', async () => {
  const ctx = await request.newContext({ baseURL: BASE });
  const { verifier, challenge } = pkce();

  // 1. Dynamic client registration -> client_id.
  const reg = await ctx.post('/oauth/register', {
    headers: { 'Content-Type': 'application/json' },
    data: {
      client_name: CLIENT,
      redirect_uris: [REDIRECT],
      application_type: 'native',
      token_endpoint_auth_method: 'none',
    },
  });
  expect(reg.status()).toBe(201);
  const { client_id } = await reg.json();
  expect(client_id).toBeTruthy();

  // 2. Authorize: the resource owner submits the shared secret as the password.
  const authUrl = `/oauth/authorize?response_type=code&client_id=${client_id}&redirect_uri=${encodeURIComponent(
    REDIRECT
  )}&code_challenge=${challenge}&code_challenge_method=S256&state=st&resource=${encodeURIComponent(
    RESOURCE
  )}`;
  const auth = await ctx.post(authUrl, {
    // Do NOT auto-follow the 302 -> redirect_uri (http://localhost/cb): the
    // authorization code lives in the Location header, and localhost:80 isn't
    // a real server.
    maxRedirects: 0,
    headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
    form: {
      response_type: 'code',
      client_id,
      redirect_uri: REDIRECT,
      state: 'st',
      password: SECRET,
      code_challenge: challenge,
      code_challenge_method: 'S256',
      resource: RESOURCE,
    },
  });
  expect(auth.status()).toBe(302);
  const loc = new URL(auth.headers()['location']);
  const code = loc.searchParams.get('code');
  expect(typeof code).toBe('string');
  expect(code).toBeTruthy();
  expect(loc.searchParams.get('state')).toBe('st');

  // 3. Exchange the code for an access token (PKCE-bound).
  const tok = await ctx.post('/oauth/token', {
    headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
    form: {
      grant_type: 'authorization_code',
      code,
      client_id,
      redirect_uri: REDIRECT,
      code_verifier: verifier,
      resource: RESOURCE,
    },
  });
  expect(tok.status()).toBe(200);
  const { access_token, token_type } = await tok.json();
  expect(typeof access_token).toBe('string');
  expect(access_token).toBeTruthy();
  expect(token_type).toBe('Bearer');

  // 4. The access token authorizes the MCP endpoint.
  const authed = await ctx.post('/mcp', {
    headers: {
      'Content-Type': 'application/json',
      // Streamable HTTP requires BOTH mime types in Accept.
      Accept: 'application/json, text/event-stream',
      Authorization: `Bearer ${access_token}`,
    },
    data: {
      jsonrpc: '2.0',
      id: 1,
      method: 'initialize',
      params: { protocolVersion: '2024-11-05', capabilities: {}, clientInfo: { name: CLIENT, version: '1' } },
    },
  });
  // 200 proves the issued access token actually authorizes the endpoint — a
  // 401 here would mean OAuth -> resource authorization is broken.
  expect(authed.status()).toBe(200);
  await ctx.dispose();
});

test('unauthenticated /mcp returns 401 with an OAuth challenge', async () => {
  const ctx = await request.newContext({ baseURL: BASE });
  const res = await ctx.post('/mcp', {
    headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
    data: { jsonrpc: '2.0', id: 2, method: 'initialize', params: {} },
  });
  expect(res.status()).toBe(401);
  const www = res.headers()['www-authenticate'] ?? '';
  expect(www).toContain('resource_metadata=');
  expect(www).toContain('oauth-protected-resource');
  await ctx.dispose();
});
