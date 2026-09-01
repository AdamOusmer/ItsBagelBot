// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Nightbot OAuth callback for the config importer: verifies the state cookie,
// exchanges the code, and parks the access token in a short-lived HttpOnly
// cookie the preview action reads. Every failure lands back on the wizard
// with ?e=nb_oauth — the import page renders the retry prose, this route
// never renders anything itself.
import type { RequestHandler } from './$types';
import { redirect } from '@sveltejs/kit';
import { logger } from '@bagel/shared/server/logger';
import {
  NB_COOKIE_PATH,
  NB_STATE_COOKIE,
  NB_TOKEN_COOKIE,
  NB_TOKEN_TTL_SECONDS,
  exchangeNightbotCode
} from '$lib/server/nightbot-oauth';

const WIZARD = '/settings/import?source=nightbot';

// callbackGate validates the provider callback against the HttpOnly state
// cookie: provider-reported errors, a missing code, and a state mismatch all
// collapse to the same retry path (no detail leaks into the URL).
function callbackGate(
  code: string | null,
  state: string | null,
  stored: string | undefined
): code is string {
  if (!code || !state) return false;
  return !!stored && state === stored;
}

export const GET: RequestHandler = async ({ locals, cookies, url }) => {
  const s = locals.session;
  if (!s || s.delegate_of) throw redirect(302, '/');

  const stored = cookies.get(NB_STATE_COOKIE);
  cookies.delete(NB_STATE_COOKIE, { path: NB_COOKIE_PATH });

  const code = url.searchParams.get('code');
  const state = url.searchParams.get('state');
  if (!callbackGate(code, state, stored)) throw redirect(302, `${WIZARD}&e=nb_oauth`);

  let token: string;
  try {
    token = await exchangeNightbotCode(code);
  } catch (err) {
    logger.error({ err }, '[nightbot-callback] code exchange failed');
    throw redirect(302, `${WIZARD}&e=nb_oauth`);
  }

  cookies.set(NB_TOKEN_COOKIE, token, {
    path: NB_COOKIE_PATH,
    httpOnly: true,
    secure: url.protocol === 'https:',
    sameSite: 'lax',
    maxAge: NB_TOKEN_TTL_SECONDS
  });
  throw redirect(302, WIZARD);
};
