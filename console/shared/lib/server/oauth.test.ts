// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Pins the Twitch token-endpoint quirk the login 500 came from: scope arrives
// as a JSON array where RFC 6749 §5.1 (and oauth4webapi) require a string.
// The exchange itself succeeded, then parsing threw OperationProcessingError
// on every login. These tests run the REAL library parser over Twitch-shaped
// bodies, so a future oauth4webapi bump that changes strictness — or a future
// Twitch fix that removes the quirk — surfaces here instead of at login.
import { describe, expect, it, test } from 'bun:test';
import {
  processAuthorizationCodeResponse,
  type AuthorizationServer,
  type Client
} from 'oauth4webapi';
import {
  __normalizeTwitchScopeForTests as normalizeTwitchScope,
  Google,
  OAuth2Tokens,
  ResponseBodyError
} from './oauth';

const GOOGLE = new Google('go-id', 'go-secret', 'https://dash.example/youtube/callback');

const AS: AuthorizationServer = {
  issuer: 'https://id.twitch.tv/oauth2',
  authorization_endpoint: 'https://id.twitch.tv/oauth2/authorize',
  token_endpoint: 'https://id.twitch.tv/oauth2/token'
};
const client: Client = { client_id: 'client-id' };

const b64 = (o: object) => Buffer.from(JSON.stringify(o)).toString('base64url');
const now = Math.floor(Date.now() / 1000);
const idToken = `${b64({ alg: 'RS256', typ: 'JWT' })}.${b64({
  iss: 'https://id.twitch.tv/oauth2',
  aud: 'client-id',
  exp: now + 3600,
  iat: now,
  sub: '12345',
  nonce: 'abc'
})}.sig`;

const twitchBody = () => ({
  access_token: 'a'.repeat(30),
  refresh_token: 'r'.repeat(30),
  expires_in: 14124,
  scope: ['openid', 'user:read:email'],
  token_type: 'bearer',
  id_token: idToken
});

const jsonResponse = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), { status, headers: { 'content-type': 'application/json' } });

describe('normalizeTwitchScope', () => {
  it('makes the real Twitch shape (array scope) parse through oauth4webapi', async () => {
    const normalized = await normalizeTwitchScope(jsonResponse(twitchBody()));
    const result = await processAuthorizationCodeResponse(AS, client, normalized, {
      requireIdToken: true,
      expectedNonce: 'abc'
    });
    expect(result.access_token).toBe('a'.repeat(30));
    expect(result.scope).toBe('openid user:read:email');
  });

  it('the raw Twitch shape still throws without the shim (quirk still exists upstream)', async () => {
    await expect(
      processAuthorizationCodeResponse(AS, client, jsonResponse(twitchBody()), {
        requireIdToken: true,
        expectedNonce: 'abc'
      })
    ).rejects.toThrow(/"scope" property must be a string/);
  });

  it('leaves an already-conformant string scope untouched', async () => {
    const body = { ...twitchBody(), scope: 'openid user:read:email' };
    const normalized = await normalizeTwitchScope(jsonResponse(body));
    const result = await processAuthorizationCodeResponse(AS, client, normalized, {
      requireIdToken: true,
      expectedNonce: 'abc'
    });
    expect(result.scope).toBe('openid user:read:email');
  });

  it('passes error responses through byte-identical for ResponseBodyError classification', async () => {
    const resp = jsonResponse({ status: 400, message: 'invalid grant' }, 400);
    const out = await normalizeTwitchScope(resp);
    expect(out).toBe(resp);
  });

  it('tolerates a non-JSON body without throwing', async () => {
    const resp = new Response('gateway timeout', { status: 200 });
    const out = await normalizeTwitchScope(resp);
    expect(out).toBe(resp);
  });
});

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

describe('OAuth2Tokens', () => {
  test('refuses to hand out missing token fields', () => {
    const tokens = new OAuth2Tokens({});
    expect(() => tokens.accessToken()).toThrow('access_token');
    expect(() => tokens.refreshToken()).toThrow('refresh_token');
    expect(() => tokens.claims()).toThrow('no ID Token');
  });
});

describe('Google', () => {
  test('authorize URL carries offline access + forced consent, byte-for-byte', () => {
    const url = GOOGLE.createAuthorizationURL('st123', ['https://www.googleapis.com/auth/youtube.force-ssl']);
    expect(url.toString()).toBe(
      'https://accounts.google.com/o/oauth2/v2/auth?response_type=code&client_id=go-id' +
        '&state=st123&scope=https%3A%2F%2Fwww.googleapis.com%2Fauth%2Fyoutube.force-ssl' +
        '&redirect_uri=https%3A%2F%2Fdash.example%2Fyoutube%2Fcallback' +
        '&access_type=offline&prompt=consent'
    );
  });

  test('exchanges with client_secret_post credentials and keeps expires_in', async () => {
    const captured: Request[] = [];
    const original = global.fetch;
    global.fetch = fakeFetch(
      200,
      {
        access_token: 'at',
        refresh_token: 'rt',
        token_type: 'bearer',
        expires_in: 3599,
        scope: 'https://www.googleapis.com/auth/youtube.force-ssl'
      },
      captured
    );
    try {
      const tokens = await GOOGLE.validateAuthorizationCode('auth-code');
      expect(tokens.accessToken()).toBe('at');
      expect(tokens.refreshToken()).toBe('rt');
      expect(tokens.expiresIn()).toBe(3599);
    } finally {
      global.fetch = original;
    }

    expect(captured).toHaveLength(1);
    const req = captured[0];
    expect(req.method).toBe('POST');
    expect(req.url).toBe('https://oauth2.googleapis.com/token');
    // Same secret-in-body auth method as Twitch; parameter order is
    // oauth4webapi's own, pinned so any drift is a deliberate change.
    expect(await req.text()).toBe(
      'redirect_uri=https%3A%2F%2Fdash.example%2Fyoutube%2Fcallback' +
        '&code=auth-code&grant_type=authorization_code' +
        '&client_id=go-id&client_secret=go-secret'
    );
  });

  test('a missing refresh token is a value, not an error', async () => {
    const original = global.fetch;
    global.fetch = fakeFetch(200, { access_token: 'at', token_type: 'bearer', expires_in: 3599 });
    try {
      const tokens = await GOOGLE.validateAuthorizationCode('auth-code');
      expect(tokens.refreshTokenOptional()).toBeUndefined();
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
        await GOOGLE.validateAuthorizationCode('bad-code');
      } catch (err) {
        caught = err;
      }
      expect(caught).toBeInstanceOf(ResponseBodyError);
    } finally {
      global.fetch = original;
    }
  });
});
