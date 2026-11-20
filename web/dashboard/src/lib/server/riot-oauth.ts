// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { redirect, type Cookies } from '@sveltejs/kit';
import { env } from '@bagel/kit/server/env';
import { generateState } from '@bagel/kit/server/oauth';
import { openOAuthState, sealOAuthState } from '@bagel/kit/server/oauth-state';
import { decodeKey } from '@bagel/kit/server/session';
import { cookieOptions, STATE_TTL_SECONDS } from './cookies';
import { requireModuleOAuthActor } from './module-oauth';

export const RIOT_MODULE_PATH = '/modules/valorant';
export const RIOT_STATE_COOKIE = 'riot_oauth_state';

type OAuthState = string;
type RiotActor = { uid: string; sid: string };
type RiotError = 'unconfigured' | 'declined' | 'state' | 'oauth' | 'store' | 'unlink';

export function requireValorantActor(locals: App.Locals): RiotActor {
  const { uid, session } = requireModuleOAuthActor(locals, { id: 'valorant', path: RIOT_MODULE_PATH, section: 'modules' });
  return { uid, sid: session.sid };
}

export function riotConfigured(): boolean {
  return !!env.RIOT_RSO_CLIENT_ID && !!env.RIOT_RSO_REDIRECT_URI;
}

export function riotRedirectURI(): string {
  const uri = env.RIOT_RSO_REDIRECT_URI;
  if (!uri) throw new Error('RIOT_RSO_REDIRECT_URI not set');
  return uri;
}

export function riotAuthorizeURL(state: OAuthState): string {
  const clientId = env.RIOT_RSO_CLIENT_ID;
  if (!clientId) throw new Error('RIOT_RSO_CLIENT_ID not set');
  const params = new URLSearchParams({
    client_id: clientId,
    redirect_uri: riotRedirectURI(),
    response_type: 'code',
    scope: 'openid',
    state
  });
  return `https://auth.riotgames.com/authorize?${params}`;
}

export function startRiotOAuth(actor: RiotActor, cookies: Cookies, url: URL): string {
  const state = generateState();
  const cookie = sealOAuthState(decodeKey(process.env.SESSION_KEY), RIOT_STATE_COOKIE, JSON.stringify(actor), state);
  cookies.set(RIOT_STATE_COOKIE, cookie, cookieOptions(url, STATE_TTL_SECONDS));
  return riotAuthorizeURL(state);
}

export function clearRiotOAuthState(cookies: Cookies): void {
  cookies.delete(RIOT_STATE_COOKIE, { path: '/' });
}

export function riotCallbackCode(actor: RiotActor, cookies: Cookies, params: URLSearchParams): string {
  const stored = cookies.get(RIOT_STATE_COOKIE);
  clearRiotOAuthState(cookies);
  const state = params.get('state');
  if (!stored || !state) riotFail('state');
  if (openOAuthState(decodeKey(process.env.SESSION_KEY), RIOT_STATE_COOKIE, JSON.stringify(actor), stored) !== state) {
    riotFail('state');
  }
  const error = params.get('error');
  if (error === 'access_denied') riotFail('declined');
  if (error) riotFail('oauth');
  const code = params.get('code');
  if (!code) riotFail('state');
  return code;
}

export function riotFail(slug: RiotError, status: 302 | 303 = 302): never {
  throw redirect(status, `${RIOT_MODULE_PATH}?e=${slug}`);
}
