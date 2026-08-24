// Copyright (c) 2026 Adam Ousmer. All rights reserved.

// End of the Spotify connect flow: verify the state cookie, exchange the code,
// and hand the refresh token to the modules service's sealed custody
// (bagel.rpc.modules.spotify.set). The token is never echoed or logged.
//
// Spotify reuses consent: a re-connect with unchanged scopes can come back
// WITHOUT a refresh token (refreshTokenOptional). That is only a success when
// one is already on file — otherwise the broadcaster must revoke the app on
// Spotify's side so the next grant issues a fresh one.
import type { RequestHandler } from './$types';
import { redirect } from '@sveltejs/kit';
import { ResponseBodyError } from '@bagel/shared/server/oauth';
import { rpc } from '@bagel/shared/server/nats';
import { logger } from '@bagel/shared/server/logger';
import { spotify } from '$lib/server/oauth';
import { SUB, auditDashboardImpersonation } from '$lib/server/services';
import { gateModulePage } from '$lib/server/module-gate';
import { effectiveId } from '$lib/server/board';
import type { Session } from '$lib/server/session';
import { dev } from '$app/environment';
import { env } from '$env/dynamic/private';

const DEMO = dev && env.DEMO === '1';

const STATE_COOKIE = 'spotify_oauth_state';

function gate(session: Session | null | undefined): void {
  gateModulePage(session, 'songqueue');
}

function requireSession(locals: App.Locals): string | null {
  if (!DEMO && !locals.session) return null;
  return effectiveId(locals.session);
}

function fail(slug: string): never {
  throw redirect(302, `/songqueue?e=${slug}`);
}

export const GET: RequestHandler = async ({ url, cookies, locals }) => {
  gate(locals.session);
  const uid = requireSession(locals);
  if (!uid) throw redirect(302, '/login?next=/songqueue');
  if (DEMO) throw redirect(302, '/songqueue');

  const code = url.searchParams.get('code');
  const state = url.searchParams.get('state');
  const storedState = cookies.get(STATE_COOKIE);
  cookies.delete(STATE_COOKIE, { path: '/' });

  if (!code || !state || state !== storedState) fail('state');

  let refreshToken: string | undefined;
  try {
    const tokens = await spotify().validateAuthorizationCode(code);
    refreshToken = tokens.refreshTokenOptional();
  } catch (e) {
    if (!(e instanceof ResponseBodyError)) throw e;
    logger.warn({ err: e }, '[spotify-callback] code exchange refused');
    fail('oauth');
  }

  if (refreshToken) {
    try {
      await rpc<{ error?: string }>(`${SUB.spotifyKey}.set`, { user_id: uid, refresh_token: refreshToken }, 5000);
    } catch (err) {
      logger.error({ err }, '[spotify-callback] token store failed');
      fail('store');
    }
  } else {
    // Consent reuse: keep the stored token, but only when there is one.
    try {
      const status = await rpc<{ present?: boolean }>(`${SUB.spotifyKey}.status`, { user_id: uid }, 3000);
      if (!status.present) fail('notoken');
    } catch {
      fail('store');
    }
  }

  auditDashboardImpersonation(locals.session, 'spotify:connect', '');
  throw redirect(302, '/songqueue?connected=1');
};
