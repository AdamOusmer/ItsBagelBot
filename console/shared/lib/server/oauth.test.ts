// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Pins the oauth4webapi-backed Twitch client's wire behavior. The authorization
// URL and the token-exchange request are asserted as exact strings: these are
// what Twitch actually sees, so a single changed byte (auth method drifting
// from client_secret_post, a dropped scope separator) must fail loudly — see
// the golden-output-tests note.

import { describe, expect, test } from 'bun:test';
import * as oauth4webapi from 'oauth4webapi';
import { expectNoNonce, generateState, OAuth2Tokens, ResponseBodyError, Twitch } from './oauth';

const CLIENT = new Twitch('cl-id', 'secret-value', 'https://dash.example/auth/callback');

function fakeFetch(status: number, body: unknown, capture?: Request[]): typeof fetch {
  return (async (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
    const req = input instanceof Request ? input : new Request(input, init);
    capture?.push(req);
    return new Response(JSON.stringify(body), {
      status,
      headers: { 'Content-Type': 'application/json' }
    });
  }) as typeof fetch;
}

// A minimal id_token whose claims pass oauth4webapi's validation against the
// pinned issuer/client: signed payload is irrelevant here (TLS trust model).
function idToken(claims: Record<string, unknown>): string {
  const b64 = (v: object) => Buffer.from(JSON.stringify(v)).toString('base64url');
  const now = Math.floor(Date.now() / 1000);
  return [
    b64({ alg: 'RS256', typ: 'JWT' }),
    b64({ iss: 'https://id.twitch.tv/oauth2', aud: 'cl-id', sub: '12345', exp: now + 600, iat: now, ...claims }),
    'sig'
  ].join('.');
}

function tokenBody(id_token: string) {
  return {
    access_token: 'at',
    refresh_token: 'rt',
    id_token,
    token_type: 'bearer',
    expires_in: 12345,
    scope: 'openid'
  };
}

describe('generateState', () => {
  test('is 32 random bytes as unpadded base64url', () => {
    const seen = new Set<string>();
    for (let i = 0; i < 32; i++) {
      const state = generateState();
      expect(state).toMatch(/^[A-Za-z0-9_-]{43}$/);
      seen.add(state);
    }
    expect(seen.size).toBe(32);
  });
});

describe('createAuthorizationURL', () => {
  test('matches the arctic-era consent URL byte-for-byte', () => {
    const url = CLIENT.createAuthorizationURL('st123', ['openid', 'user:read:email']);
    expect(url.toString()).toBe(
      'https://id.twitch.tv/oauth2/authorize?response_type=code&client_id=cl-id' +
        '&state=st123&scope=openid+user%3Aread%3Aemail' +
        '&redirect_uri=https%3A%2F%2Fdash.example%2Fauth%2Fcallback'
    );
  });

  test('omits scope entirely when no scopes are requested', () => {
    expect(CLIENT.createAuthorizationURL('st123', []).searchParams.get('scope')).toBeNull();
  });
});

describe('validateAuthorizationCode', () => {
  test('exchanges with client_secret_post credentials and returns validated claims', async () => {
    const captured: Request[] = [];
    const original = global.fetch;
    global.fetch = fakeFetch(200, tokenBody(idToken({ nonce: 'n-1', preferred_username: 'Mavey' })), captured);
    try {
      const tokens = await CLIENT.validateAuthorizationCode('auth-code', 'n-1');
      expect(tokens.accessToken()).toBe('at');
      expect(tokens.refreshToken()).toBe('rt');
      expect(tokens.claims().sub).toBe('12345');
      expect(tokens.claims().preferred_username).toBe('Mavey');
    } finally {
      global.fetch = original;
    }

    expect(captured).toHaveLength(1);
    const req = captured[0];
    expect(req.method).toBe('POST');
    expect(req.url).toBe('https://id.twitch.tv/oauth2/token');
    // client_secret_post: credentials ride in the form body, exactly like the
    // arctic-era requests Twitch has always accepted from this app. Parameter
    // order is oauth4webapi's own; pinned so any drift is a deliberate change.
    expect(await req.text()).toBe(
      'redirect_uri=https%3A%2F%2Fdash.example%2Fauth%2Fcallback' +
        '&code=auth-code&grant_type=authorization_code' +
        '&client_id=cl-id&client_secret=secret-value'
    );
  });

  test('rejects an id_token whose nonce does not match the stored one', async () => {
    const original = global.fetch;
    global.fetch = fakeFetch(200, tokenBody(idToken({ nonce: 'other-nonce' })));
    try {
      let caught: unknown;
      try {
        await CLIENT.validateAuthorizationCode('auth-code', 'expected-nonce');
      } catch (err) {
        caught = err;
      }
      expect(caught).toBeDefined();
      expect(caught).not.toBeInstanceOf(ResponseBodyError);
    } finally {
      global.fetch = original;
    }
  });

  test('accepts a nonce-bearing id_token only via expectNoNonce when no nonce is given', async () => {
    const original = global.fetch;
    global.fetch = fakeFetch(200, tokenBody(idToken({})));
    try {
      await CLIENT.validateAuthorizationCode('auth-code', expectNoNonce);
    } finally {
      global.fetch = original;
    }
  });

  test('maps a 400 OAuth error body to ResponseBodyError', async () => {
    const original = global.fetch;
    global.fetch = fakeFetch(400, { error: 'invalid_grant', error_description: 'code expired' });
    try {
      let caught: unknown;
      try {
        await CLIENT.validateAuthorizationCode('bad-code');
      } catch (err) {
        caught = err;
      }
      expect(caught).toBeInstanceOf(ResponseBodyError);
      expect((caught as ResponseBodyError).error).toBe('invalid_grant');
    } finally {
      global.fetch = original;
    }
  });

  test('a non-OAuth failure status stays outside ResponseBodyError so routes rethrow it', async () => {
    const original = global.fetch;
    global.fetch = fakeFetch(500, { error: 'nope' });
    try {
      let caught: unknown;
      try {
        await CLIENT.validateAuthorizationCode('any');
      } catch (err) {
        caught = err;
      }
      expect(caught).not.toBeInstanceOf(ResponseBodyError);
    } finally {
      global.fetch = original;
    }
  });
});

describe('OAuth2Tokens', () => {
  test('refuses to hand out missing token fields', () => {
    const tokens = new OAuth2Tokens({});
    expect(() => tokens.accessToken()).toThrow('access_token');
    expect(() => tokens.refreshToken()).toThrow('refresh_token');
    expect(() => tokens.claims()).toThrow('no ID Token');
  });
});
