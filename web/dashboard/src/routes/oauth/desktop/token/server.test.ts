// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { beforeEach, describe, expect, mock, test } from 'bun:test';
import { stubSvelteKit } from '../../../../../test/sveltekit';

stubSvelteKit();

let rateDecision = { allowed: true, retryAfterSec: 0 };
let exchangeOutcome: unknown = { ok: true, tokens: tokensFixture() };
let refreshOutcome: unknown = { ok: true, tokens: tokensFixture() };

function tokensFixture() {
  return {
    accessToken: 'v1.payload.sig',
    tokenType: 'Bearer' as const,
    expiresIn: 900,
    refreshToken: 'bbr_next',
    grantId: 'grant-1',
    deviceId: 'device-1'
  };
}

const services = await import('../../../../lib/server/services');
const deviceAuthStore = await import('../../../../lib/server/device-auth-store');

mock.module('#lib/server/device-auth-store', () => ({
  ...deviceAuthStore,
  checkTokenRate: async () => rateDecision
}));
mock.module('#lib/server/services', () => ({
  ...services,
  deviceCodeExchange: async () => exchangeOutcome,
  deviceTokenRefresh: async () => refreshOutcome
}));

const { POST } = await import('./+server');

const GOOD_VERIFIER = 'dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXkabc';
const GOOD_BODY =
  `grant_type=authorization_code&client_id=itsbagelbot-desktop&code=abc123` +
  `&code_verifier=${GOOD_VERIFIER}&redirect_uri=http%3A%2F%2F127.0.0.1%3A51123%2Fcallback`;

async function post(body: string, headers: Record<string, string> = {}): Promise<Response> {
  const request = new Request('https://dashboard.itsbagelbot.com/oauth/desktop/token', {
    method: 'POST',
    headers: { 'content-type': 'application/x-www-form-urlencoded', ...headers },
    body
  });
  return POST({ request, getClientAddress: () => '203.0.113.5' } as never);
}

beforeEach(() => {
  rateDecision = { allowed: true, retryAfterSec: 0 };
  exchangeOutcome = { ok: true, tokens: tokensFixture() };
  refreshOutcome = { ok: true, tokens: tokensFixture() };
});

describe('oauth desktop token endpoint', () => {
  test('a successful exchange returns the snake_case RFC 6749 shape with no-store headers', async () => {
    const res = await post(GOOD_BODY);
    expect(res.status).toBe(200);
    expect(res.headers.get('cache-control')).toBe('no-store');
    expect(res.headers.get('pragma')).toBe('no-cache');
    const body = await res.json();
    expect(body).toEqual({
      access_token: 'v1.payload.sig',
      token_type: 'Bearer',
      expires_in: 900,
      refresh_token: 'bbr_next',
      grant_id: 'grant-1',
      device_id: 'device-1'
    });
  });

  test('an invalid_grant outcome answers 400 with the RFC 6749 error code, still no-store', async () => {
    exchangeOutcome = { ok: false, oauthError: 'invalid_grant' };
    const res = await post(GOOD_BODY);
    expect(res.status).toBe(400);
    expect(res.headers.get('cache-control')).toBe('no-store');
    expect(await res.json()).toEqual({ error: 'invalid_grant' });
  });

  test('a transient server_error outcome answers 503, never invalid_grant', async () => {
    exchangeOutcome = { ok: false, oauthError: 'server_error' };
    const res = await post(GOOD_BODY);
    expect(res.status).toBe(503);
    expect(await res.json()).toEqual({ error: 'server_error' });
  });

  test('refresh_token grant forwards to deviceTokenRefresh', async () => {
    refreshOutcome = { ok: true, tokens: { ...tokensFixture(), refreshToken: 'bbr_rotated' } };
    const res = await post('grant_type=refresh_token&client_id=itsbagelbot-desktop&refresh_token=bbr_abc123');
    expect(res.status).toBe(200);
    expect((await res.json()).refresh_token).toBe('bbr_rotated');
  });

  test('a non-form content type is refused before any parsing', async () => {
    const res = await post(JSON.stringify({ grant_type: 'authorization_code' }), { 'content-type': 'application/json' });
    expect(res.status).toBe(400);
    expect(await res.json()).toEqual({ error: 'invalid_request' });
  });

  test('a body over the size cap is refused by content-length alone', async () => {
    const res = await post(GOOD_BODY, { 'content-length': String(1024 * 1024) });
    expect(res.status).toBe(400);
  });

  test('a wrong client_id answers invalid_client', async () => {
    const res = await post(GOOD_BODY.replace('itsbagelbot-desktop', 'someone-else'));
    expect(res.status).toBe(400);
    expect(await res.json()).toEqual({ error: 'invalid_client' });
  });

  test('an unsupported grant_type is refused', async () => {
    const res = await post('grant_type=password&client_id=itsbagelbot-desktop');
    expect(res.status).toBe(400);
    expect(await res.json()).toEqual({ error: 'unsupported_grant_type' });
  });

  test('an oversized body with no Content-Length is still refused', async () => {
    const res = await post(`${GOOD_BODY}&pad=${'x'.repeat(5000)}`);
    expect(res.status).toBe(400);
    expect(await res.json()).toEqual({ error: 'invalid_request' });
  });

  test('a malformed code_verifier never reaches the rate limiter or Users', async () => {
    const res = await post(GOOD_BODY.replace(GOOD_VERIFIER, 'too-short'));
    expect(res.status).toBe(400);
    expect(await res.json()).toEqual({ error: 'invalid_request' });
  });

  test('a rate-limited request answers 429 with Retry-After and no-store, before calling Users', async () => {
    rateDecision = { allowed: false, retryAfterSec: 17 };
    const res = await post(GOOD_BODY);
    expect(res.status).toBe(429);
    expect(res.headers.get('retry-after')).toBe('17');
    expect(res.headers.get('cache-control')).toBe('no-store');
  });

  test('cookies on the request are never read: the handler only destructures request and getClientAddress', async () => {
    const request = new Request('https://dashboard.itsbagelbot.com/oauth/desktop/token', {
      method: 'POST',
      headers: { 'content-type': 'application/x-www-form-urlencoded', cookie: 'bagel_session=someones-session' },
      body: GOOD_BODY
    });
    const res = await POST({ request, getClientAddress: () => '203.0.113.5' } as never);
    expect(res.status).toBe(200);
    expect(res.headers.has('set-cookie')).toBe(false);
  });
});
