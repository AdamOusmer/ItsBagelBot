// Copyright (c) 2026 Adam Ousmer. All rights reserved.

import type { RequestHandler } from './$types';
import { redirect } from '@sveltejs/kit';
import { rpc } from '@bagel/kit/server/nats';
import { logger } from '@bagel/kit/server/logger';
import { spotifyRedirectURI } from '$lib/server/oauth';
import { SUB, auditDashboardImpersonation } from '$lib/server/services';
import { SPOTIFY_STATE_COOKIE, requireSongqueueActor, songqueueFail } from '$lib/server/spotify-oauth';

interface SpotifyGrantReply {
  refreshToken?: string;
  scopes: string[];
}

async function exchangeCode(uid: string, code: string): Promise<SpotifyGrantReply> {
  let reply: { refresh_token?: string; scopes?: string[]; error?: string };
  try {
    reply = await rpc(
      `${SUB.gossip}.spotify.exchange`,
      { channel_id: uid, code, redirect_uri: spotifyRedirectURI() },
      10000
    );
  } catch (err) {
    logger.error({ err }, '[spotify-callback] code exchange unreachable');
    songqueueFail('oauth');
  }
  if (reply.error) {
    logger.warn({ err: reply.error }, '[spotify-callback] code exchange refused');
    songqueueFail('oauth');
  }
  return {
    refreshToken: reply.refresh_token || undefined,
    scopes: Array.isArray(reply.scopes) ? reply.scopes : []
  };
}

async function storeRefreshToken(uid: string, grant: SpotifyGrantReply): Promise<void> {
  try {
    await rpc<{ error?: string }>(
      `${SUB.spotifyKey}.set`,
      { user_id: uid, refresh_token: grant.refreshToken, scopes: grant.scopes },
      5000
    );
  } catch (err) {
    logger.error({ err }, '[spotify-callback] token store failed');
    songqueueFail('store');
  }
}

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

function verifiedCode(
  code: string | null,
  state: string | null,
  storedState: string | undefined
): string | null {
  if (!code) return null;
  if (!state) return null;
  if (!storedState) return null;
  if (state !== storedState) return null;
  return code;
}

export const GET: RequestHandler = async ({ url, cookies, locals }) => {
  const uid = requireSongqueueActor(locals);

  const code = url.searchParams.get('code');
  const state = url.searchParams.get('state');
  const storedState = cookies.get(SPOTIFY_STATE_COOKIE);
  cookies.delete(SPOTIFY_STATE_COOKIE, { path: '/' });

  const accepted = verifiedCode(code, state, storedState);
  if (!accepted) songqueueFail('state');

  const grant = await exchangeCode(uid, accepted);
  if (grant.refreshToken) await storeRefreshToken(uid, grant);
  else await requireStoredToken(uid);

  auditDashboardImpersonation(locals.session, 'spotify:connect', '');
  throw redirect(302, '/songqueue?connected=1');
};
