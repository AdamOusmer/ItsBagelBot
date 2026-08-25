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
import { SPOTIFY_STATE_COOKIE, requireSongqueueActor, songqueueFail } from '$lib/server/spotify-oauth';

// exchangeCode spends the authorization code. A refused exchange is the
// broadcaster's problem to retry, not a server fault, so it becomes an
// explainer; anything that is not an OAuth error body is a real fault and
// propagates.
async function exchangeCode(code: string): Promise<string | undefined> {
  try {
    const tokens = await spotify().validateAuthorizationCode(code);
    return tokens.refreshTokenOptional();
  } catch (e) {
    if (!(e instanceof ResponseBodyError)) throw e;
    logger.warn({ err: e }, '[spotify-callback] code exchange refused');
    songqueueFail('oauth');
  }
}

// storeRefreshToken hands the token to sealed custody. Failure is fatal to the
// flow: silently continuing would report a connection that cannot resolve.
async function storeRefreshToken(uid: string, refreshToken: string): Promise<void> {
  try {
    await rpc<{ error?: string }>(`${SUB.spotifyKey}.set`, { user_id: uid, refresh_token: refreshToken }, 5000);
  } catch (err) {
    logger.error({ err }, '[spotify-callback] token store failed');
    songqueueFail('store');
  }
}

// requireStoredToken covers consent reuse: Spotify returned no refresh token,
// so this only succeeds if one is already on file.
//
// The presence check is deliberately settled BEFORE anything is thrown. Failing
// inside the try would have its redirect caught by the try's own catch and
// rewritten as 'store', which made the 'notoken' explainer unreachable and told
// a broadcaster who simply needs to revoke and re-grant that our storage broke.
async function requireStoredToken(uid: string): Promise<void> {
  let present = false;
  try {
    const status = await rpc<{ present?: boolean }>(`${SUB.spotifyKey}.status`, { user_id: uid }, 3000);
    present = status.present === true;
  } catch {
    songqueueFail('store');
  }
  if (!present) songqueueFail('notoken');
}

export const GET: RequestHandler = async ({ url, cookies, locals }) => {
  const uid = requireSongqueueActor(locals);

  const code = url.searchParams.get('code');
  const state = url.searchParams.get('state');
  const storedState = cookies.get(SPOTIFY_STATE_COOKIE);
  cookies.delete(SPOTIFY_STATE_COOKIE, { path: '/' });

  if (!code || !state || state !== storedState) songqueueFail('state');

  const refreshToken = await exchangeCode(code);
  if (refreshToken) await storeRefreshToken(uid, refreshToken);
  else await requireStoredToken(uid);

  auditDashboardImpersonation(locals.session, 'spotify:connect', '');
  throw redirect(302, '/songqueue?connected=1');
};
