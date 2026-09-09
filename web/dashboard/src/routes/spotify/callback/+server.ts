// Copyright (c) 2026 Adam Ousmer. All rights reserved.

// End of the Spotify connect flow: verify the state cookie, have the code
// redeemed, and hand the refresh token to the modules service's sealed custody
// (bagel.rpc.modules.spotify.set). The token is never echoed or logged.
//
// The redemption itself happens in gossip (spotify.exchange), not here. Every
// broadcaster authorizes against their OWN Spotify application, and that
// application's client secret is sealed in modules custody and imported by
// gossip alone: the service that already talks to accounts.spotify.com to
// refresh tokens. Forwarding the code keeps the console out of that secret's
// blast radius; it still handles the refresh token, exactly as before.
//
// Spotify reuses consent: a re-connect with unchanged scopes can come back
// WITHOUT a refresh token. That is only a success when one is already on file
//: otherwise the broadcaster must revoke the app on Spotify's side so the
// next grant issues a fresh one.
import type { RequestHandler } from './$types';
import { redirect } from '@sveltejs/kit';
import { rpc } from '@bagel/shared/server/nats';
import { logger } from '@bagel/shared/server/logger';
import { spotifyRedirectURI } from '$lib/server/oauth';
import { SUB, auditDashboardImpersonation } from '$lib/server/services';
import { SPOTIFY_STATE_COOKIE, requireSongqueueActor, songqueueFail } from '$lib/server/spotify-oauth';

// exchangeCode has gossip spend the authorization code against the
// broadcaster's own Spotify app. A refused exchange is the broadcaster's
// problem to retry (wrong credentials pasted, a spent code, a redirect URI
// they never registered), not a server fault, so it becomes an explainer.
//
// The reply-level check sits OUTSIDE the try for the reason spelled out on
// songqueueFail: a redirect thrown inside would be caught by this function's
// own catch and rewritten, losing the reason it was thrown for.
// SpotifyGrantReply is what gossip hands back: the refresh token, plus the
// scopes Spotify actually granted so custody can record them next to it.
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
    // Carries no secret: gossip maps upstream failures to chat-safe text.
    logger.warn({ err: reply.error }, '[spotify-callback] code exchange refused');
    songqueueFail('oauth');
  }
  return {
    refreshToken: reply.refresh_token || undefined,
    scopes: Array.isArray(reply.scopes) ? reply.scopes : []
  };
}

// storeRefreshToken hands the token to sealed custody. Failure is fatal to the
// flow: silently continuing would report a connection that cannot resolve.
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

// verifiedCode is the CSRF check, returning the code it vouched for rather than
// a boolean so the caller gets a non-null value out of the same test.
//
// One reason per line: a missing code, a missing state, a missing cookie and a
// state that does not match are four different ways to be replayed at, even
// though the broadcaster sees one explainer.
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
