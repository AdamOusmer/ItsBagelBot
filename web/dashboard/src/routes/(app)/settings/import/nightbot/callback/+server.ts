// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Nightbot OAuth callback for the config importer: verifies the state cookie,
// exchanges the code, and parks the access token in a short-lived HttpOnly
// cookie the preview action reads. Every failure lands back on the wizard
// with ?e=nb_oauth: the import page renders the retry prose, this route
// never renders anything itself.
import type { RequestHandler } from './$types';
import type { Cookies } from '@sveltejs/kit';
import { redirect } from '@sveltejs/kit';
import { logger } from '@bagel/kit/server/logger';
import {
  NB_COOKIE_PATH,
  NB_RETURN_COOKIE,
  NB_STATE_COOKIE_PATH,
  NB_STATE_COOKIE,
  NB_TOKEN_COOKIE,
  NB_TOKEN_TTL_SECONDS,
  exchangeNightbotCode,
  importOwner
} from '$lib/server/nightbot-oauth';

// consumeCallback validates the provider callback against the HttpOnly state
// cookie (always deleting it, single use) and returns the code, or null:
// provider-reported errors, a missing code, and a state mismatch all collapse
// to the same retry path so no detail leaks into the URL.
function consumeCallback(cookies: Cookies, url: URL): string | null {
  const stored = cookies.get(NB_STATE_COOKIE);
  cookies.delete(NB_STATE_COOKIE, { path: NB_STATE_COOKIE_PATH });

  const code = url.searchParams.get('code');
  const state = url.searchParams.get('state');
  if (!code || !state) return null;
  return stored && state === stored ? code : null;
}

// parkToken exchanges the code and parks the access token for the preview
// action; false means the exchange failed (already logged) and the wizard
// shows the retry prose.
async function parkToken(cookies: Cookies, url: URL, code: string): Promise<boolean> {
  let token: string;
  try {
    token = await exchangeNightbotCode(code);
  } catch (err) {
    logger.error({ err }, '[nightbot-callback] code exchange failed');
    return false;
  }
  cookies.set(NB_TOKEN_COOKIE, token, {
    path: NB_COOKIE_PATH,
    httpOnly: true,
    secure: url.protocol === 'https:',
    sameSite: 'lax',
    maxAge: NB_TOKEN_TTL_SECONDS
  });
  return true;
}

// wizardReturn consumes the single-use return cookie the connect route set,
// so the round trip lands back in whichever wizard started it.
function wizardReturn(cookies: Cookies): string {
  const welcome = cookies.get(NB_RETURN_COOKIE) === 'welcome';
  cookies.delete(NB_RETURN_COOKIE, { path: NB_STATE_COOKIE_PATH });
  return welcome ? '/welcome?import=1&source=nightbot' : '/settings/import?source=nightbot';
}

export const GET: RequestHandler = async ({ locals, cookies, url }) => {
  if (!importOwner(locals)) throw redirect(302, '/');
  const wizard = wizardReturn(cookies);

  const code = consumeCallback(cookies, url);
  if (!code) throw redirect(302, `${wizard}&e=nb_oauth`);

  if (!(await parkToken(cookies, url, code))) throw redirect(302, `${wizard}&e=nb_oauth`);
  throw redirect(302, wizard);
};
