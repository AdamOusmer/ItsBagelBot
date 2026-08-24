// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Pins the Twitch token-endpoint quirk the login 500 came from: scope arrives
// as a JSON array where RFC 6749 §5.1 (and oauth4webapi) require a string.
// The exchange itself succeeded, then parsing threw OperationProcessingError
// on every login. These tests run the REAL library parser over Twitch-shaped
// bodies, so a future oauth4webapi bump that changes strictness — or a future
// Twitch fix that removes the quirk — surfaces here instead of at login.
import { describe, expect, it } from 'bun:test';
import {
  processAuthorizationCodeResponse,
  type AuthorizationServer,
  type Client
} from 'oauth4webapi';
import { __normalizeTwitchScopeForTests as normalizeTwitchScope } from './oauth';

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
