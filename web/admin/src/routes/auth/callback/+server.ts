// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { RequestHandler } from './$types';
import type { Cookies } from '@sveltejs/kit';
import { redirect } from '@sveltejs/kit';
import { isOAuthProtocolError, readTwitchAccount } from '@bagel/kit/server/oauth';
import { twitch } from '#lib/server/oauth';
import { adminCheck } from '#lib/server/services';
import { COOKIE, seal, SESSION_TTL_SECONDS } from '#lib/server/session';
import { env } from '@bagel/kit/server/env';

type IdTokenClaims = { sub: string; aud?: string | string[]; iss?: string; nonce?: string };

function rejectLogin(reason: 'state' | 'oauth' | 'denied'): never {
  throw redirect(302, `/login?e=${reason}`);
}

function audienceOk(aud: IdTokenClaims['aud'], clientId: string): boolean {
  return Array.isArray(aud) ? aud.includes(clientId) : aud === clientId;
}

function claimsOk(claims: IdTokenClaims, clientId: string, storedNonce: string): boolean {
  return audienceOk(claims.aud, clientId) && claims.iss === 'https://id.twitch.tv/oauth2' && claims.nonce === storedNonce;
}

function stateMatches(state: string | null, stored: string | undefined): boolean {
  return !!stored && state === stored;
}

function callbackCode(url: URL, cookies: Cookies): { code: string; storedNonce: string } {
  const code = url.searchParams.get('code');
  const state = url.searchParams.get('state');
  const stored = cookies.get('oauth_state');
  const storedNonce = cookies.get('oauth_nonce');
  cookies.delete('oauth_state', { path: '/' });
  cookies.delete('oauth_nonce', { path: '/' });

  if (!code || !stateMatches(state, stored)) rejectLogin('state');
  // The nonce cookie is mandatory: a flow that lost it must fail, not log in without replay protection.
  if (!storedNonce) rejectLogin('state');
  return { code, storedNonce };
}

async function signIn(url: URL, cookies: Cookies, code: string, storedNonce: string): Promise<void> {
  const clientId = env.TWITCH_CLIENT_ID ?? '';
  const tokens = await twitch().validateAuthorizationCode(code, storedNonce);
  const claims = tokens.claims() as unknown as IdTokenClaims;
  if (!claimsOk(claims, clientId, storedNonce)) rejectLogin('state');

  const account = await readTwitchAccount(clientId, tokens.accessToken());
  if (!account) rejectLogin('oauth');
  const check = await adminCheck(claims.sub, account.login, account.displayName);
  if (!check.admin) rejectLogin('denied');

  const now = Math.floor(Date.now() / 1000);
  const value = seal({
    user_id: claims.sub,
    login: account.login,
    display_name: account.displayName,
    role: 'streamer',
    iat: now,
    expires_at: now + SESSION_TTL_SECONDS
  });
  cookies.set(COOKIE, value, {
    path: '/',
    httpOnly: true,
    secure: url.protocol === 'https:',
    sameSite: 'lax',
    maxAge: SESSION_TTL_SECONDS
  });
}

export const GET: RequestHandler = async ({ url, cookies }) => {
  const { code, storedNonce } = callbackCode(url, cookies);
  try {
    await signIn(url, cookies, code, storedNonce);
  } catch (e) {
    if (isOAuthProtocolError(e)) rejectLogin('oauth');
    throw e;
  }
  throw redirect(302, '/');
};
