// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// End of the YouTube connect flow: verify the state cookie, exchange the code,
// resolve the granted channel, and persist the lease through the users
// service's grant_save (platform 'youtube'). The token is never echoed or
// logged.
//
// The channel bind happens BEFORE the persist on purpose: the bot's token
// lease RPC (bagel.rpc.youtube.token.get) is addressed by UC channel id, so a
// grant without one is unusable — better to fail here than store an orphan.
// Google only mints a refresh token on consent, and the connect route forces
// prompt=consent for exactly this reason; a response without one still fails
// rather than silently keeping a possibly-stale old grant.
import type { RequestHandler } from './$types';
import { redirect } from '@sveltejs/kit';
import { isOAuthProtocolError } from '@bagel/shared/server/oauth';
import { logger } from '@bagel/shared/server/logger';
import { google } from '$lib/server/oauth';
import { saveYoutubeGrant, auditDashboardImpersonation } from '$lib/server/services';
import { dev } from '$app/environment';
import { env } from '$env/dynamic/private';

const DEMO = dev && env.DEMO === '1';

const STATE_COOKIE = 'youtube_oauth_state';

function fail(slug: string): never {
  throw redirect(302, `/settings?youtube=${slug}`);
}

export const GET: RequestHandler = async ({ url, cookies, locals }) => {
  if (!locals.session || locals.session.delegate_of) throw redirect(302, '/settings');
  if (DEMO) throw redirect(302, '/settings');

  const code = url.searchParams.get('code');
  const state = url.searchParams.get('state');
  const storedState = cookies.get(STATE_COOKIE);
  cookies.delete(STATE_COOKIE, { path: '/' });

  if (!code || !state || state !== storedState) fail('state');

  let accessToken: string;
  let refreshToken: string | undefined;
  let expiresIn: number | undefined;
  try {
    const tokens = await google().validateAuthorizationCode(code);
    accessToken = tokens.accessToken();
    refreshToken = tokens.refreshTokenOptional();
    expiresIn = tokens.expiresIn();
  } catch (e) {
    if (!isOAuthProtocolError(e)) throw e;
    logger.warn({ err: e }, '[youtube-callback] code exchange refused');
    fail('oauth');
  }

  if (!refreshToken) fail('notoken');

  // Resolve which channel was granted. Every failure path just leaves the id
  // empty and falls into the single nochannel gate below — the redirect must
  // stay OUTSIDE this try/catch, because redirect() throws and the catch
  // would swallow it as if it were a fetch error.
  let youtubeChannelId = '';
  try {
    const res = await fetch('https://www.googleapis.com/youtube/v3/channels?part=id&mine=true', {
      headers: { Authorization: `Bearer ${accessToken}` },
      signal: AbortSignal.timeout(5000)
    });
    if (res.ok) {
      const body = (await res.json()) as { items?: Array<{ id?: string }> };
      youtubeChannelId = body.items?.[0]?.id ?? '';
    } else {
      logger.warn({ status: res.status }, '[youtube-callback] channels.list refused');
    }
  } catch (err) {
    logger.warn({ err }, '[youtube-callback] channel lookup failed');
  }
  if (!youtubeChannelId) fail('nochannel');

  // Absolute expiry beats a bare TTL: the users store files it next to the
  // access token so readers can tell "usable" from "re-mint first".
  const accessTokenExpiresAt =
    typeof expiresIn === 'number' ? new Date(Date.now() + expiresIn * 1000).toISOString() : undefined;

  try {
    await saveYoutubeGrant(locals.session.user_id, accessToken, refreshToken, youtubeChannelId, accessTokenExpiresAt);
  } catch (err) {
    logger.error({ err }, '[youtube-callback] grant store failed');
    fail('store');
  }

  auditDashboardImpersonation(locals.session, 'youtube:connect', '');
  throw redirect(302, '/settings?youtube=connected');
};
