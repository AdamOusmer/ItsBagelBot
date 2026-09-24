// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { RequestHandler } from './$types';
import type { Cookies } from '@sveltejs/kit';
import { redirect } from '@sveltejs/kit';
import { randomBytes } from 'node:crypto';
import { isOAuthProtocolError } from '@bagel/kit/server/oauth';
import { twitch, safeNextPath, fetchAccountEmail } from '$lib/server/oauth';
import { rpc } from '@bagel/kit/server/nats';
import { logger } from '@bagel/kit/server/logger';
import { saveGrant, isBanned, delegationConsume, userLocale, setLocale, userCursor } from '$lib/server/services';
import { COOKIE, CURSOR_COOKIE, seal, SESSION_TTL_SECONDS } from '$lib/server/session';
import { isLocale, LOCALE_COOKIE } from '@bagel/kit/i18n';
import { env } from '$env/dynamic/private';

const DASHBOARD = env.NATS_DASHBOARD_SUBJECT_PREFIX ?? 'bagel.rpc.dashboard';

type IdTokenClaims = {
  sub: string;
  preferred_username: string;
  aud?: string | string[];
  iss?: string;
  nonce?: string;
  scope?: string;
};

type Identity = { userId: string; login: string; displayName: string };

function audIssuerOk(claims: IdTokenClaims): boolean {
  const clientId = env.TWITCH_CLIENT_ID ?? '';
  const audOk = Array.isArray(claims.aud) ? claims.aud.includes(clientId) : claims.aud === clientId;
  return audOk && claims.iss === 'https://id.twitch.tv/oauth2';
}

function isBotAccount(sub: string): boolean {
  const botId = env.ADMIN_BOT_USER_ID ?? '';
  return botId !== '' && sub === botId;
}

function nonceMismatch(claims: IdTokenClaims, storedNonce: string | undefined): boolean {
  return !!storedNonce && claims.nonce !== storedNonce;
}

function missingOpenidScope(claims: IdTokenClaims): boolean {
  return !!claims.scope && !claims.scope.includes('openid');
}

function claimRejection(claims: IdTokenClaims, storedNonce: string | undefined): string | null {
  if (!audIssuerOk(claims)) return 'state';
  if (nonceMismatch(claims, storedNonce)) return 'state';
  if (isBotAccount(claims.sub)) return 'bot';
  if (missingOpenidScope(claims)) return 'scope';
  return null;
}

function verifyClaims(claims: IdTokenClaims, storedNonce: string | undefined): void {
  const rejected = claimRejection(claims, storedNonce);
  if (rejected) throw redirect(302, `/login?e=${rejected}`);
}

function setSessionCookie(cookies: Cookies, url: URL, session: Parameters<typeof seal>[0]): void {
  cookies.set(COOKIE, seal(session), {
    path: '/',
    httpOnly: true,
    secure: url.protocol === 'https:',
    sameSite: 'lax',
    maxAge: SESSION_TTL_SECONDS
  });
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

async function acceptPendingDelegation(cookies: Cookies, url: URL, id: Identity): Promise<void> {
  const pending = cookies.get('pending_delegation');
  if (!pending) return;

  cookies.delete('pending_delegation', { path: '/' });
  const result = await delegationConsume(pending, id.userId, id.login);
  if (!result.ok) throw redirect(302, '/login?e=link');

  setSessionCookie(cookies, url, {
    ...streamerSession(id),
    delegate_of: result.owner_user_id,
    delegate_login: result.owner_login,
    sections: result.sections ?? []
  });
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
    throw redirect(302, '/login?e=retry');
  }
}

function validOAuthState(code: string | null, state: string | null, stored: string | undefined): code is string {
  if (!code || !state) return false;
  return !!stored && state === stored;
}

async function seedLocaleCookie(cookies: Cookies, url: URL, userId: string): Promise<void> {
  try {
    const saved = await userLocale(userId);
    const existingCookie = cookies.get(LOCALE_COOKIE);
    const deviceLocale = existingCookie && isLocale(existingCookie) ? existingCookie : null;

    if (deviceLocale && deviceLocale !== saved) {
      try {
        await setLocale(userId, deviceLocale);
      } catch (err) {
        logger.error({ err }, '[callback] failed to sync pre-login locale to account');
      }
      return;
    }
    if (isLocale(saved)) {
      cookies.set(LOCALE_COOKIE, saved, {
        path: '/',
        httpOnly: true,
        secure: url.protocol === 'https:',
        sameSite: 'lax',
        maxAge: 60 * 60 * 24 * 365
      });
    }
  } catch {}
}

async function seedCursorCookie(cookies: Cookies, url: URL, userId: string): Promise<void> {
  try {
    const on = await userCursor(userId);
    cookies.set(CURSOR_COOKIE, on ? '1' : '0', {
      path: '/',
      httpOnly: true,
      secure: url.protocol === 'https:',
      sameSite: 'lax',
      maxAge: 60 * 60 * 24 * 365
    });
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
  code: string | null,
  state: string | null,
  storedState: string | undefined,
  storedNonce: string | undefined
): { ok: true; code: string; storedNonce: string } | { ok: false; slug: string } {
  if (!validOAuthState(code, state, storedState)) return { ok: false, slug: 'state' };
  // The nonce cookie is mandatory: a flow that lost it must fail, not log in without replay protection.
  if (!storedNonce) return { ok: false, slug: 'state' };
  return { ok: true, code, storedNonce };
}

async function runLogin(cookies: Cookies, url: URL, code: string, storedNonce: string): Promise<void> {
  try {
    await completeLogin(cookies, url, code, storedNonce);
  } catch (e) {
    if (!isOAuthProtocolError(e)) throw e;
    throw redirect(302, '/login?e=oauth');
  }
}

export const GET: RequestHandler = async ({ url, cookies }) => {
  const next = safeNextPath(cookies.get('login_next'));
  const gate = callbackGate(
    url.searchParams.get('code'),
    url.searchParams.get('state'),
    cookies.get('oauth_state'),
    cookies.get('oauth_nonce')
  );
  cookies.delete('oauth_state', { path: '/' });
  cookies.delete('oauth_nonce', { path: '/' });
  cookies.delete('login_next', { path: '/' });

  if (!gate.ok) throw redirect(302, `/login?e=${gate.slug}`);

  await runLogin(cookies, url, gate.code, gate.storedNonce);

  throw redirect(302, next ?? '/');
};

async function completeLogin(cookies: Cookies, url: URL, code: string, storedNonce: string): Promise<void> {
  const tokens = await twitch().validateAuthorizationCode(code, storedNonce);
  const claims = tokens.claims() as unknown as IdTokenClaims;
  verifyClaims(claims, storedNonce);

  const identity: Identity = {
    userId: claims.sub,
    login: claims.preferred_username.toLowerCase(),
    displayName: claims.preferred_username
  };

  if (await isBanned(identity.userId)) throw redirect(302, '/login?e=banned');

  // Register before the delegation accept, or the ghost-session gate bounces a first-time invitee.
  const email = await fetchAccountEmail(tokens.accessToken());
  await registerUser(identity, email);

  await acceptPendingDelegation(cookies, url, identity);

  setSessionCookie(cookies, url, streamerSession(identity));
  await seedLocaleCookie(cookies, url, identity.userId);
  await seedCursorCookie(cookies, url, identity.userId);
  await persistGrant(identity.userId, tokens);
}
