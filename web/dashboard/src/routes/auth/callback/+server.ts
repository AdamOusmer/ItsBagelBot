// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { RequestHandler } from './$types';
import type { Cookies } from '@sveltejs/kit';
import { redirect } from '@sveltejs/kit';
import { randomBytes } from 'node:crypto';
import { isOAuthProtocolError } from '@bagel/kit/server/oauth';
import { twitch, safeNextPath, fetchTwitchAccount } from '#lib/server/oauth';
import { rpc, RpcError } from '@bagel/kit/server/nats';
import { logger } from '@bagel/kit/server/logger';
import { saveGrant, isBanned, delegationConsume, userLocale, setLocale, userCursor, accountState, evictRenamed } from '#lib/server/services';
import { COOKIE, CURSOR_COOKIE, seal, SESSION_TTL_SECONDS } from '#lib/server/session';
import { isLocale, LOCALE_COOKIE } from '@bagel/kit/i18n';
import { env } from '@bagel/kit/server/env';
import { cookieOptions, YEAR_SECONDS } from '#lib/server/cookies';
import { isBotAccount } from '#lib/server/bot-account';
import { purgeRenamedChannel, schedulePurgeChannel } from '#lib/server/edge-purge';
import { count } from '@bagel/kit/server/analytics';
import { splitState } from '#lib/server/login-via';

const DASHBOARD = env.NATS_DASHBOARD_SUBJECT_PREFIX ?? 'rpc.dashboard';

type IdTokenClaims = {
  sub: string;
  aud?: string | string[];
  iss?: string;
  nonce?: string;
  scope?: string;
};

type Identity = { userId: string; login: string; displayName: string };
type LoginFailure = 'declined' | 'oauth' | 'state' | 'bot' | 'scope' | 'link' | 'retry' | 'banned';

function rejectLogin(step: LoginFailure): never {
  count('auth.login', step);
  throw redirect(302, `/login?e=${step}`);
}

function audIssuerOk(claims: IdTokenClaims): boolean {
  const clientId = env.TWITCH_CLIENT_ID ?? '';
  const audOk = Array.isArray(claims.aud) ? claims.aud.includes(clientId) : claims.aud === clientId;
  return audOk && claims.iss === 'https://id.twitch.tv/oauth2';
}

function nonceMismatch(claims: IdTokenClaims, storedNonce: string | undefined): boolean {
  return !!storedNonce && claims.nonce !== storedNonce;
}

function missingOpenidScope(claims: IdTokenClaims): boolean {
  return !!claims.scope && !claims.scope.includes('openid');
}

function claimRejection(claims: IdTokenClaims, storedNonce: string | undefined): LoginFailure | null {
  if (!audIssuerOk(claims)) return 'state';
  if (nonceMismatch(claims, storedNonce)) return 'state';
  if (isBotAccount(claims.sub, env)) return 'bot';
  if (missingOpenidScope(claims)) return 'scope';
  return null;
}

function verifyClaims(claims: IdTokenClaims, storedNonce: string | undefined): void {
  const rejected = claimRejection(claims, storedNonce);
  if (rejected) rejectLogin(rejected);
}

function setSessionCookie(cookies: Cookies, url: URL, session: Parameters<typeof seal>[0]): void {
  cookies.set(COOKIE, seal(session), cookieOptions(url, SESSION_TTL_SECONDS));
}

function streamerSession(id: Identity) {
  const now = Math.floor(Date.now() / 1000);
  return {
    user_id: id.userId,
    login: id.login,
    display_name: id.displayName,
    role: 'streamer' as const,
    sid: randomBytes(16).toString('base64url'),
    iat: now,
    expires_at: now + SESSION_TTL_SECONDS
  };
}

function grantedScopes(tokens: { scopes(): string[] }): { scopes?: string[] } {
  const granted = tokens.scopes();
  return granted.length ? { scopes: granted } : {};
}

function delegationErrorLanding(err: unknown): string {
  if (!(err instanceof RpcError)) rejectLogin('retry');
  if (err.message === 'cannot delegate to yourself') return '/settings?e=self';
  if (['link already used', 'ent: delegation not found'].includes(err.message)) {
    rejectLogin('link');
  }
  rejectLogin('retry');
}

async function acceptPendingDelegation(cookies: Cookies, url: URL, id: Identity): Promise<string | null> {
  const pending = cookies.get('pending_delegation');
  if (!pending) return null;

  cookies.delete('pending_delegation', { path: '/' });
  let result: Awaited<ReturnType<typeof delegationConsume>>;
  try {
    result = await delegationConsume({ token: pending, delegate: { id: id.userId, login: id.login } });
  } catch (err) {
    return delegationErrorLanding(err);
  }
  if (!result.ok) rejectLogin('link');

  setSessionCookie(cookies, url, {
    ...streamerSession(id),
    delegate_of: result.owner_user_id,
    delegate_login: result.owner_login,
    sections: result.sections ?? []
  });
  count('auth.login', 'ok');
  throw redirect(302, '/');
}

// Register before sealing: the ghost-session gate wipes a session with no user row (sign-out loop).
async function registerUser(id: Identity, email: string | null): Promise<void> {
  try {
    await rpc(`${DASHBOARD}.upsert_user`, {
      user_id: id.userId,
      username: id.login,
      display_name: id.displayName,
      ...(email ? { email } : {})
    });
  } catch (err: unknown) {
    logger.error({ err }, '[callback] upsert_user failed, refusing session');
    rejectLogin('retry');
  }
}

async function priorLogin(userId: string): Promise<string | null | undefined> {
  try {
    return (await accountState(userId)).username.toLowerCase();
  } catch (err) {
    return err instanceof RpcError && err.code === 'not_found' ? null : undefined;
  }
}

function purgeNewChannelPages(id: Identity, prior: string | null | undefined): void {
  if (prior === null) schedulePurgeChannel(id.userId, [id.login]);
  else if (prior && prior !== id.login) {
    evictRenamed(id.userId, [id.login, prior]);
    purgeRenamedChannel(id.userId, [id.login, prior]);
  }
}

function validOAuthState(code: string | null, state: string | null, stored: string | undefined): code is string {
  if (!code || !state) return false;
  return !!stored && splitState(state).state === stored;
}

async function syncDeviceLocale(userId: string, deviceLocale: string): Promise<void> {
  try {
    await setLocale(userId, deviceLocale);
  } catch (err) {
    logger.error({ err }, '[callback] failed to sync pre-login locale to account');
  }
}

async function applyLocale(cookies: Cookies, url: URL, userId: string): Promise<void> {
  const saved = await userLocale(userId);
  const existingCookie = cookies.get(LOCALE_COOKIE);
  const deviceLocale = existingCookie && isLocale(existingCookie) ? existingCookie : null;

  if (deviceLocale && deviceLocale !== saved) return syncDeviceLocale(userId, deviceLocale);
  if (!isLocale(saved)) return;
  cookies.set(LOCALE_COOKIE, saved, cookieOptions(url, YEAR_SECONDS));
}

async function seedLocaleCookie(cookies: Cookies, url: URL, userId: string): Promise<void> {
  try {
    await applyLocale(cookies, url, userId);
  } catch {}
}

async function seedCursorCookie(cookies: Cookies, url: URL, userId: string): Promise<void> {
  try {
    const on = await userCursor(userId);
    cookies.set(CURSOR_COOKIE, on ? '1' : '0', cookieOptions(url, YEAR_SECONDS));
  } catch {}
}

async function persistGrant(userId: string, tokens: { accessToken(): string; refreshToken(): string }): Promise<void> {
  try {
    await saveGrant(userId, tokens.accessToken(), tokens.refreshToken());
  } catch (err: unknown) {
    logger.error({ err }, '[callback] grant_save failed (non-fatal)');
  }
}

function callbackGate(
  params: URLSearchParams,
  cookies: Cookies
): { ok: true; code: string; storedNonce: string } | { ok: false; slug: LoginFailure } {
  const error = params.get('error');
  const code = params.get('code');
  const state = params.get('state');
  const storedState = cookies.get('oauth_state');
  const storedNonce = cookies.get('oauth_nonce');
  if (error) return { ok: false, slug: error === 'access_denied' ? 'declined' : 'oauth' };
  if (!validOAuthState(code, state, storedState)) return { ok: false, slug: 'state' };
  // The nonce cookie is mandatory: a flow that lost it must fail, not log in without replay protection.
  if (!storedNonce) return { ok: false, slug: 'state' };
  return { ok: true, code, storedNonce };
}

async function runLogin(cookies: Cookies, url: URL, code: string, storedNonce: string): Promise<string | null> {
  try {
    return await completeLogin(cookies, url, code, storedNonce);
  } catch (e) {
    if (!isOAuthProtocolError(e)) throw e;
    rejectLogin('oauth');
  }
}

export const GET: RequestHandler = async ({ url, cookies }) => {
  const next = safeNextPath(cookies.get('login_next'));
  const gate = callbackGate(url.searchParams, cookies);
  cookies.delete('oauth_state', { path: '/' });
  cookies.delete('oauth_nonce', { path: '/' });
  cookies.delete('login_next', { path: '/' });

  if (!gate.ok) rejectLogin(gate.slug);

  const landing = await runLogin(cookies, url, gate.code, gate.storedNonce);

  count('auth.login', 'ok');
  throw redirect(302, landing ?? next ?? '/');
};

async function completeLogin(cookies: Cookies, url: URL, code: string, storedNonce: string): Promise<string | null> {
  const tokens = await twitch().validateAuthorizationCode(code, storedNonce);
  const claims = tokens.claims() as unknown as IdTokenClaims;
  verifyClaims(claims, storedNonce);

  const [banned, account, prior] = await Promise.all([
    isBanned(claims.sub),
    fetchTwitchAccount(tokens.accessToken()),
    priorLogin(claims.sub)
  ]);
  if (banned) rejectLogin('banned');
  if (!account) rejectLogin('retry');
  // preferred_username is the display name, which may use any script; the login comes from Helix.
  const identity: Identity = { userId: claims.sub, login: account.login, displayName: account.displayName };

  // Register before the delegation accept, or the ghost-session gate bounces a first-time invitee.
  await registerUser(identity, account.email);
  if (prior === null) count('signup.via', splitState(url.searchParams.get('state') ?? '').via, 'signup');
  purgeNewChannelPages(identity, prior);
  await Promise.all([
    seedLocaleCookie(cookies, url, identity.userId),
    seedCursorCookie(cookies, url, identity.userId),
    persistGrant(identity.userId, tokens)
  ]);

  const landing = await acceptPendingDelegation(cookies, url, identity);

  setSessionCookie(cookies, url, { ...streamerSession(identity), ...grantedScopes(tokens) });
  return landing;
}
