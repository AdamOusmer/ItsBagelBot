// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { afterEach, describe, expect, it, mock, spyOn } from 'bun:test';
import {
  processAuthorizationCodeResponse,
  type AuthorizationServer,
  type Client
} from 'oauth4webapi';
import { OAuth2Tokens, ResponseBodyError, Twitch, isOAuthProtocolError } from './oauth';

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

describe('Twitch token exchange', () => {
  const twitch = new Twitch('client-id', 'client-secret', 'https://console.test/callback');
  const exchange = (response: Response) => {
    spyOn(globalThis, 'fetch').mockResolvedValue(response);
    return twitch.validateAuthorizationCode('code', 'abc');
  };

  afterEach(() => mock.restore());

  it('makes the real Twitch shape (array scope) parse through oauth4webapi', async () => {
    const tokens = await exchange(jsonResponse(twitchBody()));
    expect(tokens.accessToken()).toBe('a'.repeat(30));
    expect(tokens.scopes()).toEqual(['openid', 'user:read:email']);
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
    const tokens = await exchange(jsonResponse({ ...twitchBody(), scope: 'openid user:read:email' }));
    expect(tokens.scopes()).toEqual(['openid', 'user:read:email']);
  });

  it('passes error responses through for protocol error classification', async () => {
    const twitchError = await exchange(jsonResponse({ status: 400, message: 'invalid grant' }, 400)).catch((e) => e);
    const oauthError = await exchange(jsonResponse({ error: 'invalid_grant' }, 400)).catch((e) => e);
    expect(isOAuthProtocolError(twitchError)).toBe(true);
    expect(oauthError).toBeInstanceOf(ResponseBodyError);
  });

  it('tolerates a non-JSON body without throwing its own error', async () => {
    const failure = await exchange(new Response('gateway timeout', { status: 200 })).catch((e) => e);
    expect(isOAuthProtocolError(failure)).toBe(true);
  });
});

describe('OAuth2Tokens granted scopes', () => {
  it('reads normalized provider scopes without consulting identity claims', () => {
    expect(new OAuth2Tokens({ scope: 'openid moderator:read:chatters' }).scopes())
      .toEqual(['openid', 'moderator:read:chatters']);
  });
  it('does not assume a response granted permissions when scope is absent', () => {
    expect(new OAuth2Tokens({}).scopes()).toEqual([]);
  });
});
